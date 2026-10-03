package web

import (
	"context"
	"net/http"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
)

type signOutRepository interface {
	RevokeSession(context.Context, string, time.Time) error
}

func serveSignOut(w http.ResponseWriter, r *http.Request, repository content.Repository) {
	session, _, ok := SessionFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/admin/sign-in", http.StatusSeeOther)
		return
	}
	repo, ok := repository.(signOutRepository)
	if !ok {
		http.Error(w, "sign-out is temporarily unavailable", http.StatusInternalServerError)
		return
	}
	if err := repo.RevokeSession(r.Context(), session.ID, time.Now().UTC()); err != nil {
		http.Error(w, "cannot sign out", http.StatusInternalServerError)
		return
	}
	ClearSessionCookie(w)
	http.Redirect(w, r, "/admin/sign-in", http.StatusSeeOther)
}
