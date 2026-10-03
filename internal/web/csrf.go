package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
)

// authenticatedCSRFToken is called only after the raw credential has passed
// the persisted session and account checks in SessionMiddleware.
func authenticatedCSRFToken(rawCredential []byte) string {
	mac := hmac.New(sha256.New, rawCredential)
	_, _ = mac.Write([]byte("composure:csrf:authenticated:v1"))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func sessionCSRFToken(r *http.Request) string {
	if _, _, ok := SessionFromContext(r.Context()); !ok {
		return ""
	}
	state := r.Context().Value(sessionContextKey{}).(authenticatedSession)
	return state.csrfToken
}

// authenticatedPost wraps registered mutation routes inside the session guard,
// preserving the mux's method rejection for routes without a POST handler.
func authenticatedPost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := SessionFromContext(r.Context()); !ok {
			http.Redirect(w, r, "/admin/sign-in", http.StatusSeeOther)
			return
		}
		if !parseAdminForm(w, r) || !checkAuthenticatedCSRF(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// checkAuthenticatedCSRF reads only the bounded POST body parsed by the wrapper.
// A query parameter or a pre-authentication nonce cannot authorize a mutation.
func checkAuthenticatedCSRF(w http.ResponseWriter, r *http.Request) bool {
	tokens := r.PostForm["csrf_token"]
	expected := sessionCSRFToken(r)
	if expected == "" || len(tokens) != 1 || !validSignInCredential(tokens[0]) || subtle.ConstantTimeCompare([]byte(tokens[0]), []byte(expected)) != 1 {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return false
	}
	return true
}
