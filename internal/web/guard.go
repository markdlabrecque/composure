package web

import (
	"net/http"
	"strings"
)

// adminGuard keeps the admin route group behind the validated session context.
// Sign-in and its stylesheet remain reachable before authentication.
func adminGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAdminPath(r.URL.Path) || isPublicAdminRoute(r) {
			next.ServeHTTP(w, r)
			return
		}
		if _, _, ok := SessionFromContext(r.Context()); !ok {
			http.Redirect(w, r, "/admin/sign-in", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isAdminPath(path string) bool {
	return path == "/admin" || strings.HasPrefix(path, "/admin/")
}

func isPublicAdminRoute(r *http.Request) bool {
	switch {
	case r.URL.Path == "/admin/sign-in" && (r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodPost):
		return true
	case r.URL.Path == "/admin/static/admin.css" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		return true
	default:
		return false
	}
}
