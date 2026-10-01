package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
	"github.com/markdlabrecque/composure/internal/store"
)

// Proposed API for #74, confined to session.go:
// SessionMiddleware(loader SessionLoader, now func() time.Time, next http.Handler) http.Handler
// SessionLoader has the existing store.GetSessionByTokenDigest and GetAccount signatures.
// SessionFromContext(context.Context) (store.Session, store.Account, bool)
// SetSessionCookie(http.ResponseWriter, string) and ClearSessionCookie(http.ResponseWriter).
// This loader is not the #81 authentication guard. Refusal tests intentionally do
// not select an HTTP status or redirect policy. They only forbid authenticated
// context or execution of an authenticated consumer. CSRF form/token checks and
// nonce issuance belong to later tickets; duplicate security cookies fail closed here.

const sessionCookieNameForTest = "__Host-composure_session"
const nonceCookieNameForTest = "__Host-composure_csrf_nonce"

type sessionContextKeyForTest struct{}

type sessionLoaderForTest struct {
	session    store.Session
	account    store.Account
	sessionErr error
	accountErr error
	digests    [][]byte
	times      []time.Time
	accountIDs []string
	contexts   []context.Context
}

func (s *sessionLoaderForTest) GetSessionByTokenDigest(ctx context.Context, digest []byte, now time.Time) (store.Session, error) {
	s.digests = append(s.digests, bytes.Clone(digest))
	s.times = append(s.times, now)
	s.contexts = append(s.contexts, ctx)
	return s.session, s.sessionErr
}

func (s *sessionLoaderForTest) GetAccount(ctx context.Context, id string) (store.Account, error) {
	s.accountIDs = append(s.accountIDs, id)
	s.contexts = append(s.contexts, ctx)
	return s.account, s.accountErr
}

func sessionFixtureForTest() (string, []byte, time.Time, *sessionLoaderForTest) {
	// Deterministic test-only bytes, not a generated or live credential.
	raw := bytes.Repeat([]byte{0xfb}, 32)
	credential := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256(raw)
	at := time.Date(2026, 10, 1, 10, 0, 0, 123000000, time.UTC)
	loader := &sessionLoaderForTest{
		session: store.Session{
			ID: "session-fixture", AccountID: "account-fixture", TokenDigest: digest[:],
			CreatedAt: at.Format("2006-01-02T15:04:05.000Z"),
			ExpiresAt: at.Add(8 * time.Hour).Format("2006-01-02T15:04:05.000Z"),
		},
		account: store.Account{
			ID: "account-fixture", Email: "session@example.test", PasswordHash: "private-test-password-hash",
			IsEditor: true, State: "active",
		},
	}
	return credential, digest[:], at, loader
}

func sessionRequestForTest(lines ...string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "https://example.test/session-consumer", nil)
	for _, line := range lines {
		r.Header.Add("Cookie", line)
	}
	return r
}

func assertNoSessionSecretsForTest(t *testing.T, w *httptest.ResponseRecorder, secrets ...string) {
	t.Helper()
	response := w.Body.String()
	for _, values := range w.Header() {
		response += strings.Join(values, "\n")
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(response, secret) {
			t.Fatal("session middleware reflected a private test value")
		}
	}
}

func assertSessionRefusedForTest(t *testing.T, loader *sessionLoaderForTest, now time.Time, r *http.Request, secrets ...string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	SessionMiddleware(loader, func() time.Time { return now }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, ok := SessionFromContext(r.Context())
		if ok {
			t.Error("unusable credential reached a consumer with authenticated context")
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(w, r)
	assertNoSessionSecretsForTest(t, w, secrets...)
	return w
}

func TestSessionMiddlewareMissingCookie(t *testing.T) {
	_, _, at, loader := sessionFixtureForTest()
	for _, lines := range [][]string{nil, {"theme=dark"}, {nonceCookieNameForTest + "=unrelated-preauth-value"}} {
		r := sessionRequestForTest(lines...)
		called := false
		w := httptest.NewRecorder()
		SessionMiddleware(loader, func() time.Time { return at }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			if _, _, ok := SessionFromContext(r.Context()); ok {
				t.Error("missing session cookie authenticated the request")
			}
			w.WriteHeader(http.StatusNoContent)
		})).ServeHTTP(w, r)
		if !called {
			t.Fatal("context loader must delegate a request with no session cookie; route guards are separate")
		}
		if len(loader.digests) != 0 || len(loader.accountIDs) != 0 || len(w.Header().Values("Set-Cookie")) != 0 {
			t.Fatal("missing cookie caused a lookup or cookie creation/deletion")
		}
	}
	if _, _, ok := SessionFromContext(context.Background()); ok {
		t.Fatal("empty context contains authenticated state")
	}
}

func TestSessionMiddlewareStrictCredentialParsing(t *testing.T) {
	credential, digest, at, _ := sessionFixtureForTest()
	// A decoder without Strict accepts nonzero unused bits in the final digit.
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	last := strings.IndexByte(alphabet, credential[len(credential)-1])
	noncanonical := credential[:42] + string(alphabet[last+1])
	if raw, err := base64.RawURLEncoding.DecodeString(noncanonical); err != nil || base64.RawURLEncoding.EncodeToString(raw) != credential {
		t.Fatal("noncanonical fixture must decode to the canonical credential with a permissive decoder")
	}
	cases := []struct{ name, value string }{
		{"empty", ""},
		{"short", credential[:42]},
		{"long", credential + "A"},
		{"padded", credential + "="},
		{"standard-base64", strings.ReplaceAll(credential, "-", "+")},
		{"standard-base64-slash", strings.ReplaceAll(credential, "_", "/")},
		{"percent-encoded", "%" + credential[1:]},
		{"nonzero-padding-bits", noncanonical},
		{"leading-space", " " + credential},
		{"embedded-newline", credential[:10] + "\n" + credential[10:]},
		{"non-ascii", credential[:42] + "é"},
		{"31-raw-bytes", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 31))},
		{"33-raw-bytes", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 33))},
		{"oversized-value", strings.Repeat("A", 1<<20)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, loader := sessionFixtureForTest()
			assertSessionRefusedForTest(t, loader, at, sessionRequestForTest(sessionCookieNameForTest+"="+tc.value), credential, hex.EncodeToString(digest))
			if len(loader.digests) != 0 || len(loader.accountIDs) != 0 {
				t.Fatal("malformed credential reached persistent lookup")
			}
		})
	}
}

func TestSessionMiddlewareDuplicateSecurityCookies(t *testing.T) {
	credential, _, at, _ := sessionFixtureForTest()
	valid := sessionCookieNameForTest + "=" + credential
	nonce := nonceCookieNameForTest + "=" + credential
	cases := []struct {
		name  string
		lines []string
	}{
		{"identical", []string{valid + "; " + valid}},
		{"different", []string{valid + "; " + sessionCookieNameForTest + "=" + strings.Repeat("A", 43)}},
		{"separate-header-lines", []string{valid, valid}},
		{"invalid-first", []string{sessionCookieNameForTest + "=bad; " + valid}},
		{"invalid-last", []string{valid + "; " + sessionCookieNameForTest + "=bad"}},
		// net/http.Request.Cookies silently drops syntactically bad values.
		{"parser-drops-invalid-value", []string{sessionCookieNameForTest + "=é; " + valid}},
		{"duplicate-nonce", []string{valid + "; " + nonce + "; " + nonce}},
		{"duplicate-nonce-header-lines", []string{valid, nonce, nonce}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, loader := sessionFixtureForTest()
			assertSessionRefusedForTest(t, loader, at, sessionRequestForTest(tc.lines...), credential)
			if len(loader.digests) != 0 || len(loader.accountIDs) != 0 {
				t.Fatal("duplicate security cookies reached persistent lookup")
			}
		})
	}
}

func TestSessionMiddlewareValidContextAndReadOnlyRequest(t *testing.T) {
	for _, role := range []string{"editor", "administrator", "both"} {
		t.Run(role, func(t *testing.T) {
			credential, digest, at, loader := sessionFixtureForTest()
			loader.account.IsAdministrator = role != "editor"
			loader.account.IsEditor = role != "administrator"
			now := at.Add(7 * time.Hour)
			r := httptest.NewRequest(http.MethodPost, "https://example.test/session-consumer?keep=value", strings.NewReader("unchanged-body"))
			r.Header.Set("Cookie", "theme=dark; "+sessionCookieNameForTest+"="+credential)
			r = r.WithContext(context.WithValue(r.Context(), sessionContextKeyForTest{}, "request-marker"))
			headers := r.Header.Clone()
			called := false
			w := httptest.NewRecorder()
			SessionMiddleware(loader, func() time.Time { return now }, http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
				called = true
				session, account, ok := SessionFromContext(got.Context())
				if !ok || !reflect.DeepEqual(session, loader.session) || !reflect.DeepEqual(account, loader.account) || session.AccountID != account.ID {
					t.Error("valid request did not receive the matching persisted session and account")
				}
				body, err := io.ReadAll(got.Body)
				if err != nil || string(body) != "unchanged-body" || got.PostForm != nil || got.Form != nil || got.URL.String() != r.URL.String() || got.Method != r.Method || !reflect.DeepEqual(got.Header, headers) {
					t.Error("session loading consumed or parsed the body, or changed request inputs")
				}
				if got.Context().Value(sessionContextKeyForTest{}) != "request-marker" {
					t.Error("session context discarded existing request context")
				}
				w.WriteHeader(http.StatusNoContent)
			})).ServeHTTP(w, r)
			if !called {
				t.Fatal("valid session did not reach the next handler")
			}
			if len(loader.digests) != 1 || !bytes.Equal(loader.digests[0], digest) || len(loader.times) != 1 || !loader.times[0].Equal(now) || !reflect.DeepEqual(loader.accountIDs, []string{loader.session.AccountID}) {
				t.Fatal("lookup must use SHA-256 of decoded raw bytes, injected current time, and the session account ID")
			}
			for _, ctx := range loader.contexts {
				if ctx.Value(sessionContextKeyForTest{}) != "request-marker" {
					t.Error("persistent lookup did not use the request context")
				}
			}
			if _, _, ok := SessionFromContext(r.Context()); ok {
				t.Error("middleware mutated the caller's request context")
			}
			if len(w.Header().Values("Set-Cookie")) != 0 {
				t.Error("ordinary authenticated read reissued a cookie")
			}
			assertNoSessionSecretsForTest(t, w, credential, hex.EncodeToString(digest), loader.account.PasswordHash)
		})
	}
}

func TestSessionMiddlewareUnusableLookupNeverAuthenticates(t *testing.T) {
	cases := []struct {
		name   string
		change func(*sessionLoaderForTest, time.Time)
	}{
		{"unknown", func(s *sessionLoaderForTest, _ time.Time) { s.sessionErr = content.ErrNotFound }},
		{"session-read-error", func(s *sessionLoaderForTest, _ time.Time) { s.sessionErr = errors.New("session backend unavailable") }},
		{"missing-account", func(s *sessionLoaderForTest, _ time.Time) { s.accountErr = content.ErrNotFound }},
		{"account-read-error", func(s *sessionLoaderForTest, _ time.Time) { s.accountErr = errors.New("account backend unavailable") }},
		{"deactivated-account", func(s *sessionLoaderForTest, _ time.Time) { s.account.State = "deactivated" }},
		{"mismatched-account", func(s *sessionLoaderForTest, _ time.Time) { s.account.ID = "other-account" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			credential, digest, at, loader := sessionFixtureForTest()
			tc.change(loader, at)
			assertSessionRefusedForTest(t, loader, at, sessionRequestForTest(sessionCookieNameForTest+"="+credential), credential, hex.EncodeToString(digest), loader.account.PasswordHash)
			if len(loader.digests) != 1 {
				t.Fatal("well-formed credential must reach the session lookup")
			}
			if loader.sessionErr != nil && len(loader.accountIDs) != 0 {
				t.Fatal("failed session lookup still loaded an account")
			}
		})
	}
}

func TestSessionMiddlewareErrorsDoNotLogOrReflectSecrets(t *testing.T) {
	credential, digest, at, _ := sessionFixtureForTest()
	var logs bytes.Buffer
	oldWriter, oldFlags, oldPrefix := log.Writer(), log.Flags(), log.Prefix()
	oldSlog := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(oldSlog)
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
		log.SetPrefix(oldPrefix)
	})
	log.SetOutput(&logs)
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	for _, stage := range []string{"session", "account"} {
		t.Run(stage, func(t *testing.T) {
			_, _, _, loader := sessionFixtureForTest()
			private := errors.New(credential + " " + hex.EncodeToString(digest) + " " + loader.account.PasswordHash)
			if stage == "session" {
				loader.sessionErr = private
			} else {
				loader.accountErr = private
			}
			logs.Reset()
			assertSessionRefusedForTest(t, loader, at, sessionRequestForTest(sessionCookieNameForTest+"="+credential), credential, hex.EncodeToString(digest), loader.account.PasswordHash)
			for _, secret := range []string{credential, hex.EncodeToString(digest), loader.account.PasswordHash} {
				if strings.Contains(logs.String(), secret) {
					t.Fatal("session middleware logged a private test value")
				}
			}
		})
	}
}

func sessionStoreForTest(t *testing.T, at time.Time) (*store.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "composure.db")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(context.Background(), path, "session-site-fixture", at, nil); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func assertSessionCookieScopeForTest(t *testing.T, cookie *http.Cookie) {
	t.Helper()
	if cookie.Name != sessionCookieNameForTest || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatal("session cookie must be Secure, HttpOnly, SameSite=Lax, Path=/, and host-only")
	}
	if strings.Contains(strings.ToLower(cookie.Raw), "domain=") {
		t.Fatal("session cookie must omit Domain, not merely set an empty Domain")
	}
}

func assertSessionCookieDeletedForTest(t *testing.T, w *httptest.ResponseRecorder, now time.Time) {
	t.Helper()
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("unusable persisted session emitted %d cookies, want one deletion", len(cookies))
	}
	cookie := cookies[0]
	assertSessionCookieScopeForTest(t, cookie)
	if cookie.Value != "" || !(cookie.MaxAge < 0 || (!cookie.Expires.IsZero() && cookie.Expires.Before(now))) {
		t.Fatal("cookie clearing must remove the credential and include a deletion directive")
	}
}

func TestSessionMiddlewareSQLiteExpiryRevocationAndRestart(t *testing.T) {
	credential, digest, at, _ := sessionFixtureForTest()
	s, path := sessionStoreForTest(t, at)
	ctx := context.Background()
	accountID, err := s.CreateAccount(ctx, store.AccountDraft{Email: "sqlite@example.test", PasswordHash: "private-test-password-hash", IsEditor: true, State: "active"}, at)
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := s.CreateSession(ctx, store.SessionDraft{AccountID: accountID, TokenDigest: digest}, at)
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.GetSessionByTokenDigest(ctx, digest, at)
	if err != nil {
		t.Fatal(err)
	}
	verify := func(s *store.Store, now time.Time, wantAuthenticated bool, wantDeletion bool) {
		t.Helper()
		called := false
		w := httptest.NewRecorder()
		SessionMiddleware(s, func() time.Time { return now }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			session, account, ok := SessionFromContext(r.Context())
			if ok != wantAuthenticated {
				t.Error("SQLite middleware authentication differs from persisted session validity")
			}
			if ok && (session.ID != sessionID || session.AccountID != accountID || account.ID != accountID) {
				t.Error("SQLite middleware loaded the wrong session/account")
			}
			w.WriteHeader(http.StatusNoContent)
		})).ServeHTTP(w, sessionRequestForTest(sessionCookieNameForTest+"="+credential))
		if wantAuthenticated && !called {
			t.Fatal("persisted valid session did not reach its consumer")
		}
		if wantDeletion {
			assertSessionCookieDeletedForTest(t, w, now)
		} else if len(w.Header().Values("Set-Cookie")) != 0 {
			t.Fatal("read reissued the browser-session cookie")
		}
		assertNoSessionSecretsForTest(t, w, credential, hex.EncodeToString(digest), "private-test-password-hash")
	}
	for _, elapsed := range []time.Duration{0, time.Hour, 8*time.Hour - time.Nanosecond} {
		verify(s, at.Add(elapsed), true, false)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	verify(reopened, at.Add(7*time.Hour), true, false)
	verify(reopened, at.Add(8*time.Hour), false, true)
	verify(reopened, at.Add(8*time.Hour+time.Nanosecond), false, true)
	// The injected test clock can move back solely to inspect the persisted row.
	// A changed expiry/revocation value proves that a middleware read wrote state.
	after, err := reopened.GetSessionByTokenDigest(ctx, digest, at)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("middleware lookup altered the persisted session or extended absolute expiry")
	}
	if err := reopened.RevokeSession(ctx, sessionID, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	verify(reopened, at.Add(2*time.Hour), false, true)
}

func TestSessionMiddlewareCookieHelpersOverTLS(t *testing.T) {
	credential, _, at, loader := sessionFixtureForTest()
	consumer := SessionMiddleware(loader, func() time.Time { return at }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := SessionFromContext(r.Context()); ok {
			w.Header().Set("X-Test-Authenticated", "yes")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	server := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/test-issue":
			SetSessionCookie(w, credential)
		case "/test-clear":
			ClearSessionCookie(w)
		default:
			consumer.ServeHTTP(w, r)
		}
	}))
	server.StartTLS()
	client := server.Client()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar = jar
	get := func(path string) *http.Response {
		t.Helper()
		response, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		return response
	}
	issued := get("/test-issue")
	cookies := issued.Cookies()
	if len(cookies) != 1 {
		t.Fatal("session setter must emit exactly one cookie")
	}
	cookie := cookies[0]
	assertSessionCookieScopeForTest(t, cookie)
	if cookie.Value != credential || !cookie.Expires.IsZero() || cookie.MaxAge != 0 || strings.Contains(strings.ToLower(cookie.Raw), "expires=") || strings.Contains(strings.ToLower(cookie.Raw), "max-age=") {
		t.Fatal("issued credential must be a browser-session cookie without Expires or Max-Age")
	}
	if get("/test-read").Header.Get("X-Test-Authenticated") != "yes" {
		t.Fatal("Secure cookie did not authenticate over local TLS")
	}
	httpURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	httpURL.Scheme = "http"
	for _, cookie := range jar.Cookies(httpURL) {
		if cookie.Name == sessionCookieNameForTest {
			t.Fatal("local cookie policy weakened Secure for HTTP")
		}
	}
	cleared := get("/test-clear")
	w := httptest.NewRecorder()
	w.Header().Add("Set-Cookie", cleared.Header.Get("Set-Cookie"))
	assertSessionCookieDeletedForTest(t, w, at)
	if get("/test-read").Header.Get("X-Test-Authenticated") != "" {
		t.Fatal("cookie deletion did not remove the browser credential")
	}
}
