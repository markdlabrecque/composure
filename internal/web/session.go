package web

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/markdlabrecque/composure/internal/store"
)

const sessionCookieName = "__Host-composure_session"
const csrfNonceCookieName = "__Host-composure_csrf_nonce"
const sessionCredentialSize = 32
const sessionExpiryLayout = "2006-01-02T15:04:05.000Z"

type SessionLoader interface {
	GetSessionByTokenDigest(context.Context, []byte, time.Time) (store.Session, error)
	GetAccount(context.Context, string) (store.Account, error)
}

type sessionContextKey struct{}

type authenticatedSession struct {
	session store.Session
	account store.Account
}

// SessionMiddleware loads valid session state into a child request context. It
// deliberately leaves authentication refusal status and redirect policy to
// downstream route guards.
func SessionMiddleware(loader SessionLoader, now func() time.Time, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Shadow any session established by an outer middleware instance before
		// checking this request's credentials. A refusal must not retain stale auth.
		r = r.WithContext(context.WithValue(r.Context(), sessionContextKey{}, authenticatedSession{}))
		credential, count, malformed := securityCookie(r, sessionCookieName)
		_, nonceCount, nonceMalformed := securityCookie(r, csrfNonceCookieName)
		if malformed || nonceMalformed || count > 1 || nonceCount > 1 {
			next.ServeHTTP(w, r)
			return
		}
		if count == 0 {
			next.ServeHTTP(w, r)
			return
		}

		if len(credential) != 43 {
			next.ServeHTTP(w, r)
			return
		}
		raw, err := base64.RawURLEncoding.Strict().DecodeString(credential)
		if err != nil || len(raw) != sessionCredentialSize || base64.RawURLEncoding.EncodeToString(raw) != credential {
			next.ServeHTTP(w, r)
			return
		}
		currentTime := now()
		digest := sha256.Sum256(raw)
		session, err := loader.GetSessionByTokenDigest(r.Context(), digest[:], currentTime)
		if err != nil {
			clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}
		account, err := loader.GetAccount(r.Context(), session.AccountID)
		if err != nil {
			clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}
		expiresAt, parseErr := time.Parse(sessionExpiryLayout, session.ExpiresAt)
		if parseErr != nil || session.ID == "" || session.AccountID == "" || session.AccountID != account.ID || account.ID == "" || account.State != "active" || session.RevokedAt != nil || !currentTime.Before(expiresAt) || len(session.TokenDigest) != sha256.Size || !equalBytes(session.TokenDigest, digest[:]) {
			clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}

		state := authenticatedSession{session: session, account: account}
		ctx := context.WithValue(r.Context(), sessionContextKey{}, state)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// SessionFromContext returns persisted session and account state only when the
// middleware established a fully matching, active authenticated context.
func SessionFromContext(ctx context.Context) (store.Session, store.Account, bool) {
	state, ok := ctx.Value(sessionContextKey{}).(authenticatedSession)
	if !ok || state.session.ID == "" || state.session.AccountID == "" || state.account.ID == "" {
		return store.Session{}, store.Account{}, false
	}
	return state.session, state.account, true
}

// SetSessionCookie sets the host-only, secure browser-session credential.
func SetSessionCookie(w http.ResponseWriter, credential string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: credential, Path: "/", Secure: true,
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie expires the credential at the same host and path scope.
func ClearSessionCookie(w http.ResponseWriter) {
	clearSessionCookie(w)
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", Secure: true,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1,
		Expires: time.Unix(1, 0).UTC(),
	})
}

// securityCookie reads raw Cookie header pairs rather than Request.Cookie,
// which silently drops some malformed values and cannot detect every duplicate.
func securityCookie(r *http.Request, wanted string) (string, int, bool) {
	var value string
	count := 0
	malformed := false
	for _, line := range r.Header.Values("Cookie") {
		for _, pair := range strings.Split(line, ";") {
			pair = strings.TrimLeft(pair, " \t")
			name, raw, found := strings.Cut(pair, "=")
			if !found {
				if strings.TrimSpace(pair) == wanted {
					count++
					malformed = true
				}
				continue
			}
			name = strings.TrimSpace(name)
			if name != wanted {
				continue
			}
			count++
			if raw == "" || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\r\n;\"\\") {
				malformed = true
			}
			value = raw
		}
	}
	return value, count, malformed
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
