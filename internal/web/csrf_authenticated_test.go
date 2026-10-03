package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
	"github.com/markdlabrecque/composure/internal/store"
)

var csrfFormForTest = regexp.MustCompile(`(?is)<form\b[^>]*>.*?</form>`)

func csrfPostForTest(t *testing.T, f guardFixture, target string, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := guardRequest(http.MethodPost, target, values.Encode(), map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
		"Cookie":       guardCookie(f.credential),
	})
	return guardServe(f, r)
}

func csrfAssertForbiddenForTest(t *testing.T, response *httptest.ResponseRecorder, submitted ...string) {
	t.Helper()
	if response.Code != http.StatusForbidden {
		t.Errorf("CSRF rejection returned %d, want 403", response.Code)
	}
	for _, secret := range submitted {
		if secret != "" && strings.Contains(response.Body.String(), secret) {
			t.Fatal("CSRF rejection reflected a submitted token")
		}
	}
}

func TestAuthenticatedMutationRoutesRequireOneSessionBoundCSRFTokenBeforeWriting(t *testing.T) {
	otherCredential := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x92}, 32))
	cases := []struct {
		name   string
		values func(string) []string
	}{
		{name: "missing", values: func(string) []string { return nil }},
		{name: "duplicate", values: func(token string) []string { return []string{token, token} }},
		{name: "malformed_short", values: func(token string) []string { return []string{token[:42]} }},
		{name: "malformed_padded", values: func(token string) []string { return []string{token + "="} }},
		{name: "cross_session", values: func(string) []string { return []string{guardCSRFToken(t, otherCredential)} }},
	}
	for _, route := range []string{"create", "save", "publish", "signout"} {
		for _, tc := range cases {
			t.Run(route+"_"+tc.name, func(t *testing.T) {
				f := newGuardFixture(t, false)
				id := guardNewDraft(t, f, "/before")
				before := guardItem(t, f, id)
				beforeList, err := f.repository.ListItems(context.Background(), "page")
				if err != nil {
					t.Fatal(err)
				}
				token := guardCSRFToken(t, f.credential)
				values := url.Values{}
				for _, submitted := range tc.values(token) {
					values.Add("csrf_token", submitted)
				}
				target := "/admin/pages"
				switch route {
				case "create":
					values.Set("title", "must not be created")
					values.Set("path", "/rejected")
					values.Set("body", "rejected")
				case "save":
					target = "/admin/pages/" + id
					values.Set("title", "must not be saved")
					values.Set("path", "/changed")
					values.Set("body", "changed")
					values.Set("draft_revision", "1")
				case "publish":
					target = "/admin/pages/" + id + "/publish"
					values.Set("draft_revision", "1")
				case "signout":
					target = "/admin/sign-out"
				}
				response := csrfPostForTest(t, f, target, values)
				csrfAssertForbiddenForTest(t, response, values["csrf_token"]...)
				if got := guardItem(t, f, id); !reflect.DeepEqual(got, before) {
					t.Error("rejected CSRF request changed the existing draft")
				}
				afterList, err := f.repository.ListItems(context.Background(), "page")
				if err != nil {
					t.Fatal(err)
				}
				if len(afterList) != len(beforeList) {
					t.Error("rejected CSRF request created a draft")
				}
				if _, err := f.repository.PublishedByPath(context.Background(), "/before"); err != content.ErrNotFound {
					t.Errorf("rejected CSRF request published content: %v", err)
				}
				if _, err := f.repository.GetSessionByTokenDigest(context.Background(), beforeSessionDigestForTest(t, f.credential), f.now); err != nil {
					t.Errorf("rejected CSRF request revoked the session: %v", err)
				}
			})
		}
	}
}

func TestAuthenticatedCSRFDoesNotRescueExpiredOrRevokedSessions(t *testing.T) {
	for _, state := range []string{"expired", "revoked"} {
		t.Run(state, func(t *testing.T) {
			f := newGuardFixture(t, false)
			credential := f.credential
			if state == "expired" {
				raw := bytes.Repeat([]byte{0x93}, 32)
				credential = base64.RawURLEncoding.EncodeToString(raw)
				digest := sha256.Sum256(raw)
				if _, err := f.repository.CreateSession(context.Background(), store.SessionDraft{AccountID: f.accountID, TokenDigest: digest[:]}, f.now.Add(-9*time.Hour)); err != nil {
					t.Fatal(err)
				}
			} else if err := f.repository.RevokeSession(context.Background(), f.sessionID, f.now); err != nil {
				t.Fatal(err)
			}
			before, err := f.repository.ListItems(context.Background(), "page")
			if err != nil {
				t.Fatal(err)
			}
			values := url.Values{"title": {"Rejected"}, "path": {"/rejected"}, "body": {"rejected"}, "csrf_token": {guardCSRFToken(t, credential)}}
			r := guardRequest(http.MethodPost, "/admin/pages", values.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Cookie": guardCookie(credential)})
			response := guardServe(f, r)
			guardAssertSignInRedirect(t, response, credential, values.Get("csrf_token"))
			after, err := f.repository.ListItems(context.Background(), "page")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatal("invalid session with a matching derived token created content")
			}
		})
	}
}

func beforeSessionDigestForTest(t *testing.T, credential string) []byte {
	t.Helper()
	raw, err := base64.RawURLEncoding.Strict().DecodeString(credential)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return digest[:]
}

func TestAuthenticatedAdminFormsCarryExactlyOneDerivedCSRFField(t *testing.T) {
	f := newGuardFixture(t, false)
	id := guardNewDraft(t, f, "/form")
	want := guardCSRFToken(t, f.credential)
	for _, target := range []string{"/admin/pages/new", "/admin/pages/" + id + "/edit"} {
		t.Run(strings.ReplaceAll(target, "/", "_"), func(t *testing.T) {
			response := guardServe(f, guardRequest(http.MethodGet, target, "", map[string]string{"Cookie": guardCookie(f.credential)}))
			if response.Code != http.StatusOK {
				t.Fatalf("GET %s returned %d", target, response.Code)
			}
			forms := csrfFormForTest.FindAllString(response.Body.String(), -1)
			if len(forms) == 0 {
				t.Fatalf("GET %s rendered no POST form", target)
			}
			for _, form := range forms {
				if count := strings.Count(form, `name="csrf_token"`); count != 1 {
					t.Errorf("form on %s has %d csrf_token fields, want exactly one", target, count)
				}
				if !strings.Contains(form, `type="hidden"`) || !strings.Contains(form, `value="`+want+`"`) {
					t.Errorf("form on %s lacks the session-derived hidden token", target)
				}
			}
		})
	}
}

func TestAuthenticatedCSRFTokenRemainsValidAfterRepositoryRestart(t *testing.T) {
	f := newGuardFixture(t, false)
	token := guardCSRFToken(t, f.credential)
	path := f.path
	if err := f.repository.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	f.repository = reopened
	values := url.Values{"title": {"After restart"}, "path": {"/after-restart"}, "body": {"stable"}, "csrf_token": {token}}
	response := csrfPostForTest(t, f, "/admin/pages", values)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("restart-stable token returned %d, want 303", response.Code)
	}
}
