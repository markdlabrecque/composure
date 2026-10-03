package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"errors"
	"html/template"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
	"github.com/markdlabrecque/composure/internal/store"
)

const resetRequestFormBodyLimit = 1 << 20

//go:embed reset_request.html
var resetRequestTemplateFiles embed.FS

var resetRequestTemplate = template.Must(template.ParseFS(resetRequestTemplateFiles, "reset_request.html"))

type resetRequestRepository interface {
	GetAccountByEmail(context.Context, string) (store.Account, error)
	IssueToken(context.Context, store.TokenDraft, time.Time) (store.Token, string, error)
}

type resetRequestClock interface{ Now() time.Time }

type resetRequestPageData struct{ Token string }

func registerResetRequestRoutes(mux *http.ServeMux, repository content.Repository) {
	mux.HandleFunc("GET /reset", func(w http.ResponseWriter, r *http.Request) {
		serveResetRequestForm(w, r)
	})
	mux.HandleFunc("POST /reset", func(w http.ResponseWriter, r *http.Request) {
		serveResetRequestPost(w, r, repository)
	})
}

func serveResetRequestForm(w http.ResponseWriter, r *http.Request) {
	session, sessionCount, sessionMalformed := securityCookie(r, sessionCookieName)
	nonce, nonceCount, nonceMalformed := securityCookie(r, csrfNonceCookieName)
	if sessionMalformed || sessionCount > 1 || (sessionCount == 1 && !validSignInCredential(session)) || nonceMalformed || nonceCount > 1 {
		http.Error(w, "Invalid reset request.", http.StatusBadRequest)
		return
	}
	if nonceCount == 0 || !validSignInCredential(nonce) {
		b := make([]byte, sessionCredentialSize)
		if _, err := io.ReadFull(rand.Reader, b); err != nil {
			http.Error(w, "Reset is temporarily unavailable.", http.StatusInternalServerError)
			return
		}
		nonce = base64.RawURLEncoding.EncodeToString(b)
		setPreAuthNonceCookie(w, nonce)
	}
	raw, _ := base64.RawURLEncoding.Strict().DecodeString(nonce)
	writeResetRequestPage(w, http.StatusOK, resetRequestPageData{Token: preAuthToken(raw)})
}

func serveResetRequestPost(w http.ResponseWriter, r *http.Request, repository content.Repository) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/x-www-form-urlencoded") {
		http.Error(w, "Form must use application/x-www-form-urlencoded.", http.StatusUnsupportedMediaType)
		return
	}
	if r.ContentLength > resetRequestFormBodyLimit {
		http.Error(w, "Form is too large.", http.StatusRequestEntityTooLarge)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, resetRequestFormBodyLimit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "Form is too large.", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "Invalid form.", http.StatusBadRequest)
		}
		return
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		http.Error(w, "Invalid form.", http.StatusBadRequest)
		return
	}
	nonce, nonceCount, nonceMalformed := securityCookie(r, csrfNonceCookieName)
	session, sessionCount, sessionMalformed := securityCookie(r, sessionCookieName)
	if nonceMalformed || nonceCount != 1 || !validSignInCredential(nonce) || sessionMalformed || sessionCount > 1 || (sessionCount == 1 && !validSignInCredential(session)) {
		writeResetRequestCSRFError(w)
		return
	}
	tokens := form["csrf_token"]
	if len(tokens) != 1 || !validSignInCredential(tokens[0]) {
		writeResetRequestCSRFError(w)
		return
	}
	rawNonce, _ := base64.RawURLEncoding.Strict().DecodeString(nonce)
	presented, _ := base64.RawURLEncoding.Strict().DecodeString(tokens[0])
	expected, _ := base64.RawURLEncoding.Strict().DecodeString(preAuthToken(rawNonce))
	if subtle.ConstantTimeCompare(presented, expected) != 1 {
		writeResetRequestCSRFError(w)
		return
	}
	emails := form["email"]
	if len(emails) != 1 || strings.TrimSpace(emails[0]) == "" {
		http.Error(w, "Invalid form.", http.StatusBadRequest)
		return
	}
	repo, ok := repository.(resetRequestRepository)
	if !ok {
		http.Error(w, "Reset is temporarily unavailable.", http.StatusInternalServerError)
		return
	}
	account, err := repo.GetAccountByEmail(r.Context(), canonicalSignInEmail(emails[0]))
	if err != nil && !errors.Is(err, content.ErrNotFound) {
		http.Error(w, "Reset is temporarily unavailable.", http.StatusInternalServerError)
		return
	}
	if err == nil && account.State == "active" {
		at := time.Now()
		if clock, ok := repository.(resetRequestClock); ok {
			at = clock.Now()
		}
		at = at.UTC()
		accountID := account.ID
		_, _, err = repo.IssueToken(r.Context(), store.TokenDraft{
			Purpose: "password_reset", AccountID: &accountID, ExpiresAt: at.Add(time.Hour),
		}, at)
		if err != nil {
			http.Error(w, "Reset is temporarily unavailable.", http.StatusInternalServerError)
			return
		}
	}
	writeResetRequestPage(w, http.StatusOK, resetRequestPageData{})
}

func writeResetRequestPage(w http.ResponseWriter, status int, data resetRequestPageData) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = resetRequestTemplate.Execute(w, data)
}

func writeResetRequestCSRFError(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "Invalid reset request.", http.StatusForbidden)
}
