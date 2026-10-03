package web

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"html/template"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/markdlabrecque/composure/internal/auth"
	"github.com/markdlabrecque/composure/internal/content"
	"github.com/markdlabrecque/composure/internal/store"
)

const signInFormBodyLimit = 1 << 20
const signInThrottleNamespace = "signin"
const signInThrottleCapacity = 8192
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

type signInDependencies struct {
	now    func() time.Time
	verify func(password, encoded string) (bool, error)
}

type signInThrottleContextKey struct{}

type signInThrottleProvider interface {
	NewThrottle(capacity int, options ...store.ThrottleOption) (*store.Throttle, error)
}

func registerSignInRoutes(mux *http.ServeMux, repository content.Repository) {
	var limiter *store.Throttle
	if provider, ok := repository.(signInThrottleProvider); ok {
		limiter, _ = provider.NewThrottle(signInThrottleCapacity)
	}
	mux.HandleFunc("GET /admin/sign-in", func(w http.ResponseWriter, r *http.Request) {
		serveSignInForm(w, r)
	})
	mux.HandleFunc("POST /admin/sign-in", func(w http.ResponseWriter, r *http.Request) {
		if limiter != nil {
			r = r.WithContext(context.WithValue(r.Context(), signInThrottleContextKey{}, limiter))
		}
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
	serveSignInPostWithDependencies(w, r, repository, signInDependencies{now: time.Now, verify: auth.Verify})
}

func serveSignInPostWithDependencies(w http.ResponseWriter, r *http.Request, repository content.Repository, deps signInDependencies) {
	if deps.now == nil {
		deps.now = time.Now
	}
	if deps.verify == nil {
		deps.verify = auth.Verify
	}
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
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		http.Error(w, "Sign-in is temporarily unavailable.", http.StatusInternalServerError)
		return
	}
	clientIP, err := netip.ParseAddr(remoteHost)
	if err != nil {
		http.Error(w, "Sign-in is temporarily unavailable.", http.StatusInternalServerError)
		return
	}
	clientIP = clientIP.Unmap()
	canonicalEmail := canonicalSignInEmail(emails[0])
	accountDigest := signInThrottleDigest("signin", 0x02, []byte(canonicalEmail))
	ipTag := byte(0x04)
	if clientIP.Is4() {
		ipTag = 0x03
	}
	ipBytes := clientIP.AsSlice()
	ipDigest := signInThrottleDigest("signin", ipTag, ipBytes)
	limiter, _ := r.Context().Value(signInThrottleContextKey{}).(*store.Throttle)
	var limiterCleanup func()
	if limiter == nil {
		if provider, ok := repository.(signInThrottleProvider); ok {
			owner, cancel := context.WithCancel(r.Context())
			limiter, err = provider.NewThrottle(signInThrottleCapacity, store.WithThrottleMaintenance(owner, nil))
			if err != nil {
				cancel()
			} else {
				limiterCleanup = func() {
					cancel()
					<-limiter.MaintenanceDone()
				}
			}
		}
	}
	if limiterCleanup != nil {
		defer limiterCleanup()
	}
	if limiter == nil || err != nil {
		http.Error(w, "Sign-in is temporarily unavailable.", http.StatusInternalServerError)
		return
	}
	accountKey := store.AccountThrottleDigest(accountDigest)
	ipKey := store.IPThrottleDigest(ipDigest)
	admitted, err := limiter.Admit(r.Context(), signInThrottleNamespace, accountKey, ipKey, deps.now())
	if err != nil {
		http.Error(w, "Sign-in is temporarily unavailable.", http.StatusInternalServerError)
		return
	}
	if !admitted {
		writeSignInPage(w, http.StatusUnauthorized, signInPageData{Token: retryToken, Error: true})
		return
	}
	account, lookupErr := repo.GetAccountByEmail(r.Context(), canonicalEmail)
	if errors.Is(lookupErr, content.ErrNotFound) {
		_, _ = deps.verify(passwords[0], signInDummyPasswordHash)
		writeSignInPage(w, http.StatusUnauthorized, signInPageData{Token: retryToken, Error: true})
		return
	}
	if lookupErr != nil {
		_, _ = deps.verify(passwords[0], signInDummyPasswordHash)
		writeSignInPage(w, http.StatusUnauthorized, signInPageData{Error: true})
		return
	}
	verified, verifyErr := deps.verify(passwords[0], account.PasswordHash)
	if verifyErr != nil {
		verified, _ = deps.verify(passwords[0], signInDummyPasswordHash)
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
	createdAt := deps.now().UTC()
	if _, err := repo.CreateSession(r.Context(), store.SessionDraft{AccountID: account.ID, TokenDigest: digest[:]}, createdAt); err != nil {
		http.Error(w, "Sign-in is temporarily unavailable.", http.StatusInternalServerError)
		return
	}
	SetSessionCookie(w, credential)
	clearPreAuthNonceCookie(w)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func signInThrottleDigest(namespace string, tag byte, subject []byte) [32]byte {
	var encoded bytes.Buffer
	encoded.Write([]byte{'C', 'T', 'H', 'K', 0x01})
	_ = binary.Write(&encoded, binary.BigEndian, uint32(len(namespace)))
	encoded.WriteString(namespace)
	encoded.WriteByte(tag)
	_ = binary.Write(&encoded, binary.BigEndian, uint32(len(subject)))
	encoded.Write(subject)
	return sha256.Sum256(encoded.Bytes())
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
