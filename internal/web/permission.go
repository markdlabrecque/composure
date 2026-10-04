package web

import (
	"net/http"

	"github.com/markdlabrecque/composure/internal/auth"
)

// PermissionMiddleware requires an authenticated account with a role that
// permits action before passing the request to next.
func PermissionMiddleware(action auth.Action, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, account, ok := SessionFromContext(r.Context())
		if !ok {
			http.Redirect(w, r, "/admin/sign-in", http.StatusSeeOther)
			return
		}

		roles := auth.Roles{
			Administrator: account.IsAdministrator,
			Editor:        account.IsEditor,
		}
		if !auth.AllowsRoleAction(roles, auth.AccountState(account.State), action) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
