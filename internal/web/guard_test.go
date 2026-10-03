package web

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
	"github.com/markdlabrecque/composure/internal/store"
)

type guardFixture struct {
	repository *store.Store
	accountID  string
	sessionID  string
	credential string
	path       string
	now        time.Time
}

func newGuardFixture(t *testing.T, example bool) guardFixture {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	path := filepath.Join(t.TempDir(), "composure.db")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	var snapshot *content.Snapshot
	if example {
		snapshot = &content.Snapshot{Title: "Public example", Path: "/example", Fields: map[string]string{"body": "Public body"}}
	}
	if err := store.Initialize(context.Background(), path, "guard-test-site", now, snapshot); err != nil {
		t.Fatal(err)
	}
	repository, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	accountID, err := repository.CreateAccount(context.Background(), store.AccountDraft{
		Email: "guard@example.test", PasswordHash: "private-test-hash", IsEditor: true, State: "active",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	credential := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x81}, 32))
	digest := sha256.Sum256(bytes.Repeat([]byte{0x81}, 32))
	sessionID, err := repository.CreateSession(context.Background(), store.SessionDraft{AccountID: accountID, TokenDigest: digest[:]}, now)
	if err != nil {
		t.Fatal(err)
	}
	return guardFixture{repository: repository, accountID: accountID, sessionID: sessionID, credential: credential, path: path, now: now}
}

func guardRequest(method, target, body string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(method, "https://127.0.0.1:8443"+target, strings.NewReader(body))
	for name, value := range headers {
		if strings.EqualFold(name, "Host") {
			r.Host = value
			continue
		}
		r.Header.Set(name, value)
	}
	return r
}

func guardServe(f guardFixture, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	Handler(f.repository, "8443").ServeHTTP(w, r)
	return w
}

func guardCookie(credential string) string {
	return sessionCookieName + "=" + credential
}

func guardCSRFToken(t *testing.T, credential string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.Strict().DecodeString(credential)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, raw)
	_, _ = mac.Write([]byte("composure:csrf:authenticated:v1"))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func guardAssertSignInRedirect(t *testing.T, w *httptest.ResponseRecorder, secrets ...string) {
	t.Helper()
	if w.Code != http.StatusSeeOther {
		t.Fatalf("guard returned %d, want safe 303 redirect", w.Code)
	}
	location := w.Header().Get("Location")
	if location != "/admin/sign-in" {
		t.Fatalf("guard redirect=%q, want /admin/sign-in", location)
	}
	for _, secret := range secrets {
		if secret != "" && (strings.Contains(w.Body.String(), secret) || strings.Contains(location, secret) || strings.Contains(location, url.QueryEscape(secret))) {
			t.Fatal("guard response reflected a credential or submitted value")
		}
	}
}

func guardItem(t *testing.T, f guardFixture, id string) content.Item {
	t.Helper()
	item, err := f.repository.GetItem(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func guardNewDraft(t *testing.T, f guardFixture, path string) string {
	t.Helper()
	id, err := f.repository.CreateItem(context.Background(), content.ItemDraft{
		Title: "Stored draft", Path: path, Fields: map[string]string{"body": "Stored body"},
	}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAdminGuardRefusesUnusableSessionsAtHandlerBoundary(t *testing.T) {
	malformed := "not-a-canonical-session"
	unknown := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x82}, 32))
	cases := []struct {
		name    string
		cookies []string
		prepare func(t *testing.T, f guardFixture)
	}{
		{name: "absent"},
		{name: "malformed", cookies: []string{guardCookie(malformed)}},
		{name: "duplicate", cookies: []string{guardCookie(unknown), guardCookie(unknown)}},
		{name: "unknown", cookies: []string{guardCookie(unknown)}},
		{name: "expired", cookies: []string{guardCookie(base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x83}, 32)))}, prepare: func(t *testing.T, f guardFixture) {
			raw := bytes.Repeat([]byte{0x83}, 32)
			digest := sha256.Sum256(raw)
			if _, err := f.repository.CreateSession(context.Background(), store.SessionDraft{AccountID: f.accountID, TokenDigest: digest[:]}, f.now.Add(-9*time.Hour)); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "revoked", prepare: func(t *testing.T, f guardFixture) {
			if err := f.repository.RevokeSession(context.Background(), f.sessionID, f.now); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "deactivated", prepare: func(t *testing.T, f guardFixture) {
			if err := f.repository.DeactivateAccount(context.Background(), f.accountID, f.now); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newGuardFixture(t, false)
			if tc.prepare != nil {
				tc.prepare(t, f)
			}
			r := guardRequest(http.MethodGet, "/admin/pages?credential=query-secret", "", nil)
			for _, cookie := range tc.cookies {
				r.Header.Add("Cookie", cookie)
			}
			if tc.name == "revoked" || tc.name == "deactivated" {
				r.Header.Set("Cookie", guardCookie(f.credential))
			}
			w := guardServe(f, r)
			guardAssertSignInRedirect(t, w, "query-secret", malformed, unknown, f.credential)
		})
	}
}

func TestAdminGuardProtectsEveryAdminEntryAndLeavesPublicRoutesOpen(t *testing.T) {
	f := newGuardFixture(t, true)
	id := guardNewDraft(t, f, "/private")
	for _, target := range []string{"/admin", "/admin/pages", "/admin/pages/new", "/admin/pages/" + id + "/edit", "/admin/pages/" + id + "/preview"} {
		t.Run(strings.ReplaceAll(target, "/", "_"), func(t *testing.T) {
			guardAssertSignInRedirect(t, guardServe(f, guardRequest(http.MethodGet, target, "", nil)))
		})
	}
	for _, tc := range []struct {
		method, target, body string
		headers              map[string]string
		want                 int
	}{
		{http.MethodGet, "/healthz", "", nil, http.StatusOK},
		{http.MethodGet, "/example", "", nil, http.StatusOK},
		{http.MethodGet, "/admin/sign-in", "", nil, http.StatusOK},
		{http.MethodPost, "/admin/sign-in", "", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, http.StatusForbidden},
		{http.MethodGet, "/admin/static/admin.css", "", nil, http.StatusOK},
	} {
		w := guardServe(f, guardRequest(tc.method, tc.target, tc.body, tc.headers))
		if w.Code != tc.want {
			t.Errorf("%s %s returned %d, want %d", tc.method, tc.target, w.Code, tc.want)
		}
		if w.Code == http.StatusSeeOther && w.Header().Get("Location") == "/admin/sign-in" {
			t.Errorf("public route %s entered a sign-in redirect loop", tc.target)
		}
	}
}

func TestAdminGuardRefusesMutationBeforeParsingAndPreservesStoredState(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		f := newGuardFixture(t, false)
		before, err := f.repository.ListItems(context.Background(), "page")
		if err != nil {
			t.Fatal(err)
		}
		secret := "private-create-value"
		w := guardServe(f, guardRequest(http.MethodPost, "/admin/pages?credential=query-secret", "title="+secret, map[string]string{"Content-Type": "application/json"}))
		guardAssertSignInRedirect(t, w, secret, "query-secret")
		after, err := f.repository.ListItems(context.Background(), "page")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(after, before) {
			t.Fatal("refused create changed stored drafts")
		}
	})

	for _, tc := range []struct {
		name, cookie string
	}{
		{name: "save_absent"},
		{name: "save_malformed", cookie: guardCookie("malformed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newGuardFixture(t, false)
			id := guardNewDraft(t, f, "/private")
			before := guardItem(t, f, id)
			secret := "private-save-value"
			values := url.Values{"title": {secret}, "path": {"/changed"}, "body": {"changed"}, "draft_revision": {"1"}}
			headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
			if tc.cookie != "" {
				headers["Cookie"] = tc.cookie
			}
			w := guardServe(f, guardRequest(http.MethodPost, "/admin/pages/"+id, values.Encode(), headers))
			guardAssertSignInRedirect(t, w, secret, "malformed")
			if after := guardItem(t, f, id); !reflect.DeepEqual(after, before) {
				t.Fatal("refused save changed the real stored draft")
			}
		})
	}

	unknown := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x84}, 32))
	for _, tc := range []struct {
		name, cookie string
	}{
		{name: "publish_absent"},
		{name: "publish_unknown", cookie: guardCookie(unknown)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newGuardFixture(t, false)
			id := guardNewDraft(t, f, "/private")
			before := guardItem(t, f, id)
			headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
			if tc.cookie != "" {
				headers["Cookie"] = tc.cookie
			}
			w := guardServe(f, guardRequest(http.MethodPost, "/admin/pages/"+id+"/publish", "draft_revision=1", headers))
			guardAssertSignInRedirect(t, w, unknown)
			if after := guardItem(t, f, id); !reflect.DeepEqual(after, before) {
				t.Fatal("refused publish changed the real stored draft")
			}
			if _, err := f.repository.PublishedByPath(context.Background(), "/private"); err != content.ErrNotFound {
				t.Fatalf("refused publish created a public route or snapshot: %v", err)
			}
		})
	}
}

func TestAdminGuardAllowsValidatedSessionToReachMutatingHandlers(t *testing.T) {
	f := newGuardFixture(t, false)
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Cookie": guardCookie(f.credential)}
	token := guardCSRFToken(t, f.credential)
	create := url.Values{"title": {"Authenticated draft"}, "path": {"/authenticated"}, "body": {"first"}, "csrf_token": {token}}
	w := guardServe(f, guardRequest(http.MethodPost, "/admin/pages", create.Encode(), headers))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("authenticated create returned %d, want 303", w.Code)
	}
	location := w.Header().Get("Location")
	if !strings.HasPrefix(location, "/admin/pages/") || !strings.HasSuffix(location, "/edit") {
		t.Fatalf("authenticated create location=%q", location)
	}
	id := strings.TrimSuffix(strings.TrimPrefix(location, "/admin/pages/"), "/edit")
	save := url.Values{"title": {"Authenticated saved"}, "path": {"/authenticated"}, "body": {"second"}, "draft_revision": {"1"}, "csrf_token": {token}}
	if got := guardServe(f, guardRequest(http.MethodPost, "/admin/pages/"+id, save.Encode(), headers)).Code; got != http.StatusSeeOther {
		t.Fatalf("authenticated save returned %d, want 303", got)
	}
	if got := guardServe(f, guardRequest(http.MethodPost, "/admin/pages/"+id+"/publish", url.Values{"draft_revision": {"2"}, "csrf_token": {token}}.Encode(), headers)).Code; got != http.StatusSeeOther {
		t.Fatalf("authenticated publish returned %d, want 303", got)
	}
	snapshot, err := f.repository.PublishedByPath(context.Background(), "/authenticated")
	if err != nil || snapshot.Title != "Authenticated saved" || snapshot.Fields["body"] != "second" {
		t.Fatalf("authenticated mutations did not reach their handlers: snapshot=%+v err=%v", snapshot, err)
	}
}

func TestAdminGuardRetainsHostOriginAndPathSafety(t *testing.T) {
	f := newGuardFixture(t, false)
	cases := []struct {
		name, target string
		headers      map[string]string
		want         int
	}{
		{"host", "/admin/pages", map[string]string{"Host": "attacker.example"}, http.StatusBadRequest},
		{"origin", "/admin/pages", map[string]string{"Origin": "https://attacker.example", "Cookie": guardCookie(f.credential)}, http.StatusForbidden},
		{"path", "/admin//pages", nil, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := guardServe(f, guardRequest(http.MethodPost, tc.target, fmt.Sprintf("title=%s", tc.name), tc.headers))
			if w.Code != tc.want {
				t.Fatalf("boundary returned %d, want %d", w.Code, tc.want)
			}
		})
	}
}
