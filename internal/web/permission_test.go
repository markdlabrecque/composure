package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/auth"
)

func TestPermissionMiddlewareRoleAndSessionStates(t *testing.T) {
	const (
		administratorAction = auth.Action("accounts.manage")
		editorAction        = auth.Action("content.read")
	)

	cases := []struct {
		name          string
		authenticated bool
		administrator bool
		editor        bool
		state         string
		action        auth.Action
		wantStatus    int
	}{
		{"administrator_granted_administrator_action", true, true, false, "active", administratorAction, http.StatusNoContent},
		{"administrator_denied_editor_action", true, true, false, "active", editorAction, http.StatusForbidden},
		{"editor_denied_administrator_action", true, false, true, "active", administratorAction, http.StatusForbidden},
		{"editor_granted_editor_action", true, false, true, "active", editorAction, http.StatusNoContent},
		{"dual_role_granted_administrator_action", true, true, true, "active", administratorAction, http.StatusNoContent},
		{"dual_role_granted_editor_action", true, true, true, "active", editorAction, http.StatusNoContent},
		{"deactivated_account", true, true, true, "deactivated", editorAction, http.StatusSeeOther},
		{"unauthenticated_request", false, false, false, "", editorAction, http.StatusSeeOther},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			credential, _, at, loader := sessionFixtureForTest()
			loader.account.IsAdministrator = tc.administrator
			loader.account.IsEditor = tc.editor
			loader.account.State = tc.state

			called := false
			controlledRoute := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			})
			handler := SessionMiddleware(loader, func() time.Time { return at }, PermissionMiddleware(tc.action, controlledRoute))
			request := httptest.NewRequest(http.MethodGet, "https://example.test/admin/controlled", nil)
			if tc.authenticated {
				request.Header.Set("Cookie", sessionCookieNameForTest+"="+credential)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tc.wantStatus)
			}
			if called != (tc.wantStatus == http.StatusNoContent) {
				t.Fatalf("controlled route called = %v, want %v", called, tc.wantStatus == http.StatusNoContent)
			}
			if tc.wantStatus == http.StatusSeeOther && response.Header().Get("Location") != "/admin/sign-in" {
				t.Fatalf("redirect location = %q, want /admin/sign-in", response.Header().Get("Location"))
			}
		})
	}
}

func TestPermissionMiddlewareRejectsUnknownAccountStateAndActions(t *testing.T) {
	unknownState := authenticatedSession{
		session: sessionFixtureForPermissionTest().session,
		account: sessionFixtureForPermissionTest().account,
	}
	unknownState.account.State = "pending"

	cases := []struct {
		name    string
		action  auth.Action
		context context.Context
	}{
		{"unknown_account_state", auth.Action("content.read"), context.WithValue(context.Background(), sessionContextKey{}, unknownState)},
		{"unknown_action", auth.Action("content.unknown"), activePermissionContextForTest()},
		{"empty_action", auth.Action(""), activePermissionContextForTest()},
		{"malformed_action", auth.Action(" content.read"), activePermissionContextForTest()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			handler := PermissionMiddleware(tc.action, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				called = true
			}))
			request := httptest.NewRequest(http.MethodGet, "https://example.test/admin/controlled", nil).WithContext(tc.context)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
			}
			if called {
				t.Fatal("denied permission reached the controlled route")
			}
		})
	}
}

func sessionFixtureForPermissionTest() authenticatedSession {
	_, _, _, loader := sessionFixtureForTest()
	loader.account.IsAdministrator = true
	loader.account.IsEditor = true
	return authenticatedSession{session: loader.session, account: loader.account}
}

func activePermissionContextForTest() context.Context {
	return context.WithValue(context.Background(), sessionContextKey{}, sessionFixtureForPermissionTest())
}
