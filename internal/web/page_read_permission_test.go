package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/store"
)

type pageReadSession struct {
	accountID  string
	sessionID  string
	credential string
}

func pageReadSessionForTest(t *testing.T, f guardFixture, marker byte, administrator, editor bool) pageReadSession {
	t.Helper()
	accountID, err := f.repository.CreateAccount(context.Background(), store.AccountDraft{
		Email:           strings.Repeat(string(rune('a'+marker%26)), 2) + "@page-read.test",
		PasswordHash:    "page-read-test-hash",
		IsAdministrator: administrator,
		IsEditor:        editor,
		State:           "active",
	}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	raw := bytes.Repeat([]byte{marker}, 32)
	digest := sha256.Sum256(raw)
	sessionID, err := f.repository.CreateSession(context.Background(), store.SessionDraft{AccountID: accountID, TokenDigest: digest[:]}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	return pageReadSession{
		accountID:  accountID,
		sessionID:  sessionID,
		credential: base64.RawURLEncoding.EncodeToString(raw),
	}
}

func pageReadServe(t *testing.T, f guardFixture, method, target, credential string) *httptest.ResponseRecorder {
	t.Helper()
	r := guardRequest(method, target, "", nil)
	if credential != "" {
		r.Header.Set("Cookie", guardCookie(credential))
	}
	w := httptest.NewRecorder()
	HandlerWithClock(f.repository, "8443", func() time.Time { return f.now }).ServeHTTP(w, r)
	return w
}

func TestPageReadRoutesRequireAnActiveEditor(t *testing.T) {
	f := newGuardFixture(t, true)
	draftID := guardNewDraft(t, f, "/private-page-read")
	administrator := pageReadSessionForTest(t, f, 0xa1, true, false)
	editor := pageReadSessionForTest(t, f, 0xa2, false, true)
	both := pageReadSessionForTest(t, f, 0xa3, true, true)
	deactivated := pageReadSessionForTest(t, f, 0xa4, false, true)
	if err := f.repository.DeactivateAccount(context.Background(), deactivated.accountID, f.now); err != nil {
		t.Fatal(err)
	}

	routes := []struct {
		name, target string
		wantStatus   int
		wantText     string
	}{
		{name: "root", target: "/admin", wantStatus: http.StatusSeeOther},
		{name: "list", target: "/admin/pages", wantStatus: http.StatusOK, wantText: "Stored draft"},
		{name: "new", target: "/admin/pages/new", wantStatus: http.StatusOK, wantText: "New Page"},
		{name: "edit", target: "/admin/pages/" + draftID + "/edit", wantStatus: http.StatusOK, wantText: "Stored body"},
		{name: "preview", target: "/admin/pages/" + draftID + "/preview", wantStatus: http.StatusOK, wantText: "Stored body"},
	}
	states := []struct {
		name, credential string
		wantDenied       int
		allowed          bool
	}{
		{name: "unauthenticated", wantDenied: http.StatusSeeOther},
		{name: "administrator_only", credential: administrator.credential, wantDenied: http.StatusForbidden},
		{name: "editor_only", credential: editor.credential, allowed: true},
		{name: "administrator_and_editor", credential: both.credential, allowed: true},
		{name: "deactivated", credential: deactivated.credential, wantDenied: http.StatusSeeOther},
	}

	beforeItem := guardItem(t, f, draftID)
	beforeList, err := f.repository.ListItems(context.Background(), "page")
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range routes {
		for _, state := range states {
			t.Run(route.name+"_"+state.name, func(t *testing.T) {
				response := pageReadServe(t, f, http.MethodGet, route.target, state.credential)
				if state.allowed {
					if response.Code != route.wantStatus {
						t.Fatalf("GET %s returned %d, want %d", route.target, response.Code, route.wantStatus)
					}
					if route.name == "root" && response.Header().Get("Location") != "/admin/pages" {
						t.Fatalf("admin root location = %q, want /admin/pages", response.Header().Get("Location"))
					}
					if route.wantText != "" && !strings.Contains(response.Body.String(), route.wantText) {
						t.Fatalf("GET %s omitted expected Page content %q", route.target, route.wantText)
					}
					return
				}
				if response.Code != state.wantDenied {
					t.Fatalf("GET %s returned %d, want denied status %d", route.target, response.Code, state.wantDenied)
				}
				if state.wantDenied == http.StatusSeeOther && response.Header().Get("Location") != "/admin/sign-in" {
					t.Fatalf("denied location = %q, want /admin/sign-in", response.Header().Get("Location"))
				}
				for _, secret := range []string{"Stored draft", "Stored body", "/private-page-read"} {
					if strings.Contains(response.Body.String(), secret) || strings.Contains(response.Header().Get("Location"), secret) {
						t.Fatalf("denied response disclosed draft value %q", secret)
					}
				}
			})
		}
	}
	if after := guardItem(t, f, draftID); !reflect.DeepEqual(after, beforeItem) {
		t.Fatal("Page GET routes changed the stored draft")
	}
	afterList, err := f.repository.ListItems(context.Background(), "page")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterList, beforeList) {
		t.Fatal("Page GET routes changed stored Page or snapshot state")
	}
}

func TestPageReadRoutesRejectUnusableCredentialsBeforeDraftDisclosure(t *testing.T) {
	f := newGuardFixture(t, false)
	draftID := guardNewDraft(t, f, "/credential-private")
	valid := pageReadSessionForTest(t, f, 0xb1, false, true)
	revoked := pageReadSessionForTest(t, f, 0xb2, false, true)
	if err := f.repository.RevokeSession(context.Background(), revoked.sessionID, f.now); err != nil {
		t.Fatal(err)
	}
	expiredRaw := bytes.Repeat([]byte{0xb4}, 32)
	expiredDigest := sha256.Sum256(expiredRaw)
	if _, err := f.repository.CreateSession(context.Background(), store.SessionDraft{AccountID: valid.accountID, TokenDigest: expiredDigest[:]}, f.now.Add(-9*time.Hour)); err != nil {
		t.Fatal(err)
	}
	otherSite := newGuardFixture(t, false)
	crossSite := pageReadSessionForTest(t, otherSite, 0xb5, false, true)

	cases := []struct {
		name, credential string
	}{
		{name: "invalid", credential: "not-a-session"},
		{name: "expired", credential: base64.RawURLEncoding.EncodeToString(expiredRaw)},
		{name: "revoked", credential: revoked.credential},
		{name: "session_from_another_site", credential: crossSite.credential},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := pageReadServe(t, f, http.MethodGet, "/admin/pages/"+draftID+"/preview", tc.credential)
			guardAssertSignInRedirect(t, response, "Stored draft", "Stored body", "/credential-private", tc.credential)
		})
	}
}

func TestPageReadPermissionCannotBeBypassedByHEADOrWildcardPaths(t *testing.T) {
	f := newGuardFixture(t, false)
	draftID := guardNewDraft(t, f, "/head-private")
	administrator := pageReadSessionForTest(t, f, 0xc1, true, false)

	server := httptest.NewServer(HandlerWithClock(f.repository, "8443", func() time.Time { return f.now }))
	t.Cleanup(server.Close)
	request, err := http.NewRequest(http.MethodHead, server.URL+"/admin/pages/"+draftID+"/preview", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "127.0.0.1:8443"
	request.Header.Set("Cookie", guardCookie(administrator.credential))
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("HEAD preview returned %d, want 403", response.StatusCode)
	}
	if len(body) != 0 {
		t.Fatalf("denied HEAD preview returned %d on-wire body bytes", len(body))
	}
	for _, secret := range []string{"Stored draft", "Stored body", "/head-private"} {
		if strings.Contains(string(body), secret) || strings.Contains(response.Header.Get("Location"), secret) {
			t.Fatalf("denied HEAD preview disclosed draft value %q", secret)
		}
	}

	wildcard := pageReadServe(t, f, http.MethodGet, "/admin/pages/"+draftID+"/preview/extra", administrator.credential)
	if wildcard.Code != http.StatusNotFound {
		t.Fatalf("wildcard suffix returned %d, want 404", wildcard.Code)
	}
}

func TestPageReadPermissionsLeavePublicPagesHealthAndStylesheetOpen(t *testing.T) {
	f := newGuardFixture(t, true)
	guardNewDraft(t, f, "/unpublished-page-read")
	for _, tc := range []struct {
		target   string
		status   int
		contains string
	}{
		{target: "/example", status: http.StatusOK, contains: "Public body"},
		{target: "/unpublished-page-read", status: http.StatusNotFound},
		{target: "/healthz", status: http.StatusOK, contains: "ok"},
		{target: "/admin/static/admin.css", status: http.StatusOK},
	} {
		response := pageReadServe(t, f, http.MethodGet, tc.target, "")
		if response.Code != tc.status {
			t.Errorf("GET %s returned %d, want %d", tc.target, response.Code, tc.status)
		}
		if tc.contains != "" && !strings.Contains(response.Body.String(), tc.contains) {
			t.Errorf("GET %s omitted %q", tc.target, tc.contains)
		}
	}
}
