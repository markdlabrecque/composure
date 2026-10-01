package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
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

	"github.com/markdlabrecque/composure/internal/auth"
	"github.com/markdlabrecque/composure/internal/content"
	"github.com/markdlabrecque/composure/internal/store"
)

const signInFormBodyLimit = 1 << 20
const signInDummyPasswordHash = "$argon2id$v=19$m=65536,t=3,p=4$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

//go:embed admin_signin.html
var signInTemplateFiles embed.FS

var signInTemplate = template.Must(template.ParseFS(signInTemplateFiles, "admin_signin.html"))

type signInRepository interface {
	content.Repository
	GetAccountByEmail(context.Context, string) (store.Account, error)
	CreateSession(context.Context, store.SessionDraft, time.Time) (string, error)
}

type signInPageData struct {
	Token string
	Error bool
}

func registerSignInRoutes(mux *http.ServeMux, repository content.Repository) {
	mux.HandleFunc("GET /admin/sign-in", func(w http.ResponseWriter, r *http.Request) {
		serveSignInForm(w, r)
	})
	mux.HandleFunc("POST /admin/sign-in", func(w http.ResponseWriter, r *http.Request) {
		serveSignInPost(w, r, repository)
	})
}

func serveSignInForm(w http.ResponseWriter, r *http.Request) {
	session, sessionCount, sessionMalformed := securityCookie(r, sessionCookieName)
	if sessionMalformed || sessionCount > 1 || (sessionCount == 1 && !validSignInCredential(session)) {
		http.Error(w, "Invalid sign-in request.", http.StatusBadRequest)
		return
	}
	nonce, nonceCount, nonceMalformed := securityCookie(r, csrfNonceCookieName)
	if nonceCount > 1 {
		http.Error(w, "Invalid sign-in request.", http.StatusBadRequest)
		return
	}
	if nonceMalformed || nonceCount == 0 || !validSignInCredential(nonce) {
		raw := make([]byte, sessionCredentialSize)
		if _, err := io.ReadFull(rand.Reader, raw); err != nil {
			http.Error(w, "Sign-in is temporarily unavailable.", http.StatusInternalServerError)
			return
		}
		nonce = base64.RawURLEncoding.EncodeToString(raw)
		setPreAuthNonceCookie(w, nonce)
	}
	rawNonce, _ := base64.RawURLEncoding.Strict().DecodeString(nonce)
	token := preAuthToken(rawNonce)
	writeSignInPage(w, http.StatusOK, signInPageData{Token: token})
}

func serveSignInPost(w http.ResponseWriter, r *http.Request, repository content.Repository) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/x-www-form-urlencoded") {
		http.Error(w, "Form must use application/x-www-form-urlencoded.", http.StatusUnsupportedMediaType)
		return
	}
	if r.ContentLength > signInFormBodyLimit {
		http.Error(w, "Form is too large.", http.StatusRequestEntityTooLarge)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, signInFormBodyLimit))
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
		writeSignInCSRFError(w)
		return
	}
	tokens := form["csrf_token"]
	if len(tokens) != 1 || !validSignInCredential(tokens[0]) {
		writeSignInCSRFError(w)
		return
	}
	rawNonce, _ := base64.RawURLEncoding.Strict().DecodeString(nonce)
	presented, _ := base64.RawURLEncoding.Strict().DecodeString(tokens[0])
	expected, _ := base64.RawURLEncoding.Strict().DecodeString(preAuthToken(rawNonce))
	if subtle.ConstantTimeCompare(presented, expected) != 1 {
		writeSignInCSRFError(w)
		return
	}
	retryToken := preAuthToken(rawNonce)

	emails, passwords := form["email"], form["password"]
	if len(emails) != 1 || len(passwords) != 1 || emails[0] == "" || passwords[0] == "" {
		writeSignInPage(w, http.StatusUnauthorized, signInPageData{Token: retryToken, Error: true})
		return
	}
	repo, ok := repository.(signInRepository)
	if !ok {
		http.Error(w, "Sign-in is temporarily unavailable.", http.StatusInternalServerError)
		return
	}
	account, lookupErr := repo.GetAccountByEmail(r.Context(), canonicalSignInEmail(emails[0]))
	if errors.Is(lookupErr, content.ErrNotFound) {
		_, _ = auth.Verify(passwords[0], signInDummyPasswordHash)
		writeSignInPage(w, http.StatusUnauthorized, signInPageData{Token: retryToken, Error: true})
		return
	}
	if lookupErr != nil {
		_, _ = auth.Verify(passwords[0], signInDummyPasswordHash)
		writeSignInPage(w, http.StatusUnauthorized, signInPageData{Error: true})
		return
	}
	verified, verifyErr := auth.Verify(passwords[0], account.PasswordHash)
	if verifyErr != nil {
		verified, _ = auth.Verify(passwords[0], signInDummyPasswordHash)
	}
	if !verified || account.State != "active" {
		writeSignInPage(w, http.StatusUnauthorized, signInPageData{Token: retryToken, Error: true})
		return
	}

	rawCredential := make([]byte, sessionCredentialSize)
	if _, err := io.ReadFull(rand.Reader, rawCredential); err != nil {
		http.Error(w, "Sign-in is temporarily unavailable.", http.StatusInternalServerError)
		return
	}
	credential := base64.RawURLEncoding.EncodeToString(rawCredential)
	digest := sha256.Sum256(rawCredential)
	createdAt := time.Now().UTC()
	if _, err := repo.CreateSession(r.Context(), store.SessionDraft{AccountID: account.ID, TokenDigest: digest[:]}, createdAt); err != nil {
		http.Error(w, "Sign-in is temporarily unavailable.", http.StatusInternalServerError)
		return
	}
	SetSessionCookie(w, credential)
	clearPreAuthNonceCookie(w)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func canonicalSignInEmail(email string) string {
	value := []byte(email)
	start, end := 0, len(value)
	for start < end && value[start] == ' ' {
		start++
	}
	for end > start && value[end-1] == ' ' {
		end--
	}
	value = value[start:end]
	for i, b := range value {
		if b >= 'A' && b <= 'Z' {
			value[i] = b + ('a' - 'A')
		}
	}
	return string(value)
}

func validSignInCredential(value string) bool {
	if len(value) != 43 {
		return false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	return err == nil && len(raw) == sessionCredentialSize && base64.RawURLEncoding.EncodeToString(raw) == value
}

func preAuthToken(rawNonce []byte) string {
	mac := hmac.New(sha256.New, rawNonce)
	_, _ = mac.Write([]byte("composure:csrf:preauth:v1"))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func setPreAuthNonceCookie(w http.ResponseWriter, nonce string) {
	http.SetCookie(w, &http.Cookie{Name: csrfNonceCookieName, Value: nonce, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func clearPreAuthNonceCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: csrfNonceCookieName, Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0).UTC()})
}

func writeSignInPage(w http.ResponseWriter, status int, data signInPageData) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = signInTemplate.Execute(w, data)
}

func writeSignInCSRFError(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "Invalid sign-in request.", http.StatusForbidden)
}
