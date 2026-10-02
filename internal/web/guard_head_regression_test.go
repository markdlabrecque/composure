package web

import (
	"net/http"
	"strings"
	"testing"
)

func TestAdminGuardLeavesPublicHEADRoutesOpen(t *testing.T) {
	f := newGuardFixture(t, false)
	cases := []struct {
		name            string
		target          string
		wantContentType string
	}{
		{name: "sign_in", target: "/admin/sign-in", wantContentType: "text/html"},
		{name: "stylesheet", target: "/admin/static/admin.css", wantContentType: "text/css"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := guardServe(f, guardRequest(http.MethodHead, tc.target, "", nil))
			if w.Code != http.StatusOK {
				t.Errorf("HEAD %s returned %d, want %d", tc.target, w.Code, http.StatusOK)
			}
			if location := w.Header().Get("Location"); location != "" {
				t.Errorf("HEAD %s redirected to %q, want no redirect", tc.target, location)
			}
			if contentType := w.Header().Get("Content-Type"); !strings.HasPrefix(contentType, tc.wantContentType) {
				t.Errorf("HEAD %s Content-Type=%q, want %q", tc.target, contentType, tc.wantContentType)
			}
		})
	}
}
