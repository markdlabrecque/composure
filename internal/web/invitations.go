package web

import (
	"context"
	"embed"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/markdlabrecque/composure/internal/auth"
	"github.com/markdlabrecque/composure/internal/content"
	"github.com/markdlabrecque/composure/internal/store"
)

//go:embed admin_invitation.html
var invitationTemplateFiles embed.FS

var invitationTemplate = template.Must(template.ParseFS(invitationTemplateFiles, "admin_invitation.html"))

type invitationIssuer interface {
	IssueToken(context.Context, store.TokenDraft, time.Time) (store.Token, string, error)
}

type invitationClock interface {
	InvitationNow() time.Time
}

type invitationFormData struct {
	Email, CSRFToken string
	Error            string
}

func registerInvitationRoutes(mux *http.ServeMux, repository content.Repository, handleAdminPost func(string, http.HandlerFunc)) {
	mux.HandleFunc("GET /admin/invitations/new", func(w http.ResponseWriter, r *http.Request) {
		if !invitationAdministrator(w, r) {
			return
		}
		writeInvitationForm(w, http.StatusOK, r, invitationFormData{})
	})
	handleAdminPost("POST /admin/invitations", func(w http.ResponseWriter, r *http.Request) {
		if !invitationAdministrator(w, r) {
			return
		}
		emails := r.PostForm["email"]
		if len(emails) != 1 {
			writeInvitationForm(w, http.StatusUnprocessableEntity, r, invitationFormData{Error: "Enter one email address."})
			return
		}
		email := canonicalInvitationEmail(emails[0])
		if email == "" {
			writeInvitationForm(w, http.StatusUnprocessableEntity, r, invitationFormData{Email: emails[0], Error: "Enter one email address."})
			return
		}
		issuer, ok := repository.(invitationIssuer)
		if !ok {
			http.Error(w, "invitations are temporarily unavailable", http.StatusInternalServerError)
			return
		}
		at := time.Now().UTC()
		if clock, ok := repository.(invitationClock); ok {
			at = clock.InvitationNow().UTC()
		}
		_, _, err := issuer.IssueToken(r.Context(), store.TokenDraft{
			Purpose: "invitation", Email: &email, ExpiresAt: at.Add(7 * 24 * time.Hour),
		}, at)
		if err != nil {
			http.Error(w, "cannot issue invitation", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/admin/invitations/new?notice=issued", http.StatusSeeOther)
	})
}

func invitationAdministrator(w http.ResponseWriter, r *http.Request) bool {
	_, account, ok := SessionFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/admin/sign-in", http.StatusSeeOther)
		return false
	}
	if !auth.AllowsRoleAction(auth.Roles{Administrator: account.IsAdministrator, Editor: account.IsEditor}, auth.AccountState(account.State), "accounts.manage") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func canonicalInvitationEmail(email string) string {
	canonical := strings.Trim(email, " ")
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, canonical)
}

func writeInvitationForm(w http.ResponseWriter, status int, r *http.Request, data invitationFormData) {
	data.CSRFToken = sessionCSRFToken(r)
	if r.URL.Query().Get("notice") == "issued" {
		data.Error = "Invitation issued."
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	_ = invitationTemplate.Execute(w, data)
}
