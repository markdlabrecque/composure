package web

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"html"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/auth"
	"github.com/markdlabrecque/composure/internal/content"
	"github.com/markdlabrecque/composure/internal/store"
)

// #75 tests use the existing Handler entry point and #74 middleware. No new
// production API is required. Keep these tests sequential: entropy and log
// failure checks temporarily replace process globals, as the auth tests do.
// Accounts, sessions and all SQL remain behind the actual store APIs.
const signInOpaquePassword = " \tMiXeD caf\u00e9 e\u0301\x00\xff" + " private test suffix\n"
const signInTimeLayout = "2006-01-02T15:04:05.000Z"

type signInRepositoryForTest struct {
	*store.Store
	lookups             []string
	writes              []store.SessionDraft
	lookupErr, writeErr error
}

func (s *signInRepositoryForTest) GetAccountByEmail(ctx context.Context, email string) (store.Account, error) {
	s.lookups = append(s.lookups, email)
	if s.lookupErr != nil {
		return store.Account{}, s.lookupErr
	}
	return s.Store.GetAccountByEmail(ctx, email)
}

func (s *signInRepositoryForTest) CreateSession(ctx context.Context, draft store.SessionDraft, at time.Time) (string, error) {
	draft.TokenDigest = bytes.Clone(draft.TokenDigest)
	s.writes = append(s.writes, draft)
	if s.writeErr != nil {
		return "", s.writeErr
	}
	return s.Store.CreateSession(ctx, draft, at)
}

type signInFixtureForTest struct {
	repo                  *signInRepositoryForTest
	path, accountID, hash string
	at                    time.Time
}

func newSignInFixtureForTest(t *testing.T) signInFixtureForTest {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Millisecond)
	path := filepath.Join(t.TempDir(), "composure.db")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(context.Background(), path, "signin-test-site", at, nil); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	hash, err := auth.Hash(signInOpaquePassword)
	if err != nil {
		t.Fatal("cannot hash private test fixture")
	}
	id, err := s.CreateAccount(context.Background(), store.AccountDraft{Email: "editor@example.test", PasswordHash: hash, IsEditor: true, State: "active"}, at)
	if err != nil {
		t.Fatal(err)
	}
	return signInFixtureForTest{repo: &signInRepositoryForTest{Store: s}, path: path, accountID: id, hash: hash, at: at}
}

func signInNonceForTest() string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0xfb}, 32))
}

func signInTokenForTest(t *testing.T, credential, mode string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.Strict().DecodeString(credential)
	if err != nil {
		t.Fatal("bad private credential fixture")
	}
	mac := hmac.New(sha256.New, raw)
	_, _ = mac.Write([]byte("composure:csrf:" + mode + ":v1"))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func signInValuesForTest(t *testing.T) url.Values {
	return url.Values{"email": {"editor@example.test"}, "password": {signInOpaquePassword}, "csrf_token": {signInTokenForTest(t, signInNonceForTest(), "preauth")}}
}

func signInRequestForTest(method, target, body string, cookies ...string) *http.Request {
	r := httptest.NewRequest(method, "https://127.0.0.1:8443"+target, strings.NewReader(body))
	if method == http.MethodPost {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, cookie := range cookies {
		r.Header.Add("Cookie", cookie)
	}
	return r
}

func signInServeForTest(f signInFixtureForTest, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	Handler(f.repo, "8443").ServeHTTP(w, r)
	return w
}

func signInPostForTest(t *testing.T, f signInFixtureForTest, values url.Values, extraCookies ...string) *httptest.ResponseRecorder {
	t.Helper()
	cookies := append([]string{nonceCookieNameForTest + "=" + signInNonceForTest()}, extraCookies...)
	return signInServeForTest(f, signInRequestForTest(http.MethodPost, "/admin/sign-in", values.Encode(), cookies...))
}

func signInCookieForTest(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	var found *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == name {
			if found != nil {
				t.Fatal("response duplicated a security cookie")
			}
			found = cookie
		}
	}
	if found == nil {
		t.Fatalf("response omitted required %s cookie", name)
	}
	return found
}

func signInAssertCookieForTest(t *testing.T, cookie *http.Cookie, deletion bool) {
	t.Helper()
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" || strings.Contains(strings.ToLower(cookie.Raw), "domain=") {
		t.Fatal("security cookie must be Secure, HttpOnly, Lax, Path=/ and omit Domain")
	}
	if deletion {
		if cookie.Value != "" || !(cookie.MaxAge < 0 || (!cookie.Expires.IsZero() && cookie.Expires.Before(time.Now()))) {
			t.Fatal("successful sign-in did not delete the nonce")
		}
	} else if cookie.MaxAge != 0 || !cookie.Expires.IsZero() || strings.Contains(strings.ToLower(cookie.Raw), "max-age=") || strings.Contains(strings.ToLower(cookie.Raw), "expires=") {
		t.Fatal("new security credential must be a browser-session cookie")
	}
}

func signInRawForTest(t *testing.T, value string) []byte {
	t.Helper()
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if len(value) != 43 || err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != value {
		t.Fatal("credential must encode exactly 32 bytes as canonical 43-character base64url")
	}
	return raw
}

var signInInputPatternForTest = regexp.MustCompile(`(?is)<input\b[^>]*>`)
var signInAttributePatternForTest = regexp.MustCompile(`([a-zA-Z_-]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)

func signInAttributesForTest(tag string) map[string]string {
	attrs := map[string]string{}
	for _, match := range signInAttributePatternForTest.FindAllStringSubmatch(tag, -1) {
		attrs[strings.ToLower(match[1])] = html.UnescapeString(match[2] + match[3] + match[4])
	}
	return attrs
}

func signInFormTokenForTest(t *testing.T, body string) string {
	t.Helper()
	var token string
	count := 0
	for _, tag := range signInInputPatternForTest.FindAllString(body, -1) {
		a := signInAttributesForTest(tag)
		if a["name"] == "csrf_token" {
			count++
			if a["type"] != "hidden" {
				t.Fatal("CSRF field must be hidden")
			}
			token = a["value"]
		}
	}
	if count != 1 {
		t.Fatalf("form has %d csrf_token fields, want exactly one", count)
	}
	_ = signInRawForTest(t, token)
	outsideHiddenField := signInInputPatternForTest.ReplaceAllStringFunc(body, func(tag string) string {
		if signInAttributesForTest(tag)["name"] == "csrf_token" {
			return ""
		}
		return tag
	})
	if strings.Contains(outsideHiddenField, token) {
		t.Fatal("CSRF token appeared outside its hidden field, such as in a URL or script")
	}
	return token
}

func signInDatabaseBytesForTest(t *testing.T, path string) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	// Do not inspect shared-memory reader locks. SQLite persists mutations in
	// the main database or WAL; opening a read transaction may alter only SHM.
	for _, suffix := range []string{"", "-wal"} {
		data, err := os.ReadFile(path + suffix)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		result[suffix] = data
	}
	return result
}

func signInAssertUnchangedForTest(t *testing.T, f signInFixtureForTest, before map[string][]byte, writes int) {
	t.Helper()
	if len(f.repo.writes) != writes || !reflect.DeepEqual(before, signInDatabaseBytesForTest(t, f.path)) {
		t.Fatal("rejected request or safe form read mutated SQLite")
	}
}

func signInAssertNoSecretsForTest(t *testing.T, w *httptest.ResponseRecorder, secrets ...string) {
	t.Helper()
	text := w.Body.String() + w.Header().Get("Location")
	for _, secret := range secrets {
		if secret != "" && (strings.Contains(text, secret) || strings.Contains(text, html.EscapeString(secret)) || strings.Contains(text, url.QueryEscape(secret))) {
			t.Fatal("response body or redirect reflected a private test value")
		}
	}
}

func signInAssertNoCredentialForTest(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == sessionCookieNameForTest && cookie.Value != "" {
			t.Fatal("rejected sign-in emitted an authenticating credential")
		}
		if cookie.Name == nonceCookieNameForTest && cookie.Value == "" {
			t.Fatal("failed sign-in consumed the preauthentication nonce")
		}
	}
}

func TestSignInGETFormAndFreshNonce(t *testing.T) {
	f := newSignInFixtureForTest(t)
	before := signInDatabaseBytesForTest(t, f.path)
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		w := signInServeForTest(f, signInRequestForTest(http.MethodGet, "/admin/sign-in?redirect=https://attacker.example.test/collect", ""))
		if w.Code != http.StatusOK {
			t.Fatalf("GET sign-in returned %d, want 200", w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			t.Fatal("sign-in form must be uncached HTML")
		}
		body := w.Body.String()
		for name, kind := range map[string]string{"email": "email", "password": "password"} {
			count := 0
			for _, tag := range signInInputPatternForTest.FindAllString(body, -1) {
				a := signInAttributesForTest(tag)
				if a["name"] != name {
					continue
				}
				count++
				if a["type"] != kind {
					t.Fatal("sign-in credential control has the wrong type")
				}
				labelled := a["aria-label"] != "" || a["aria-labelledby"] != ""
				for _, label := range regexp.MustCompile(`(?is)<label\b[^>]*>.*?</label>`).FindAllString(body, -1) {
					if (a["id"] != "" && signInAttributesForTest(label)["for"] == a["id"]) || strings.Contains(label, tag) {
						labelled = true
					}
				}
				if !labelled {
					t.Fatal("credential input lacks an accessible label")
				}
				if name == "password" && a["value"] != "" {
					t.Fatal("form prefilled a password")
				}
			}
			if count != 1 {
				t.Fatal("form must have exactly one email and password input")
			}
		}
		forms := regexp.MustCompile(`(?is)<form\b[^>]*>`).FindAllString(body, -1)
		if len(forms) != 1 {
			t.Fatal("sign-in screen must have one sign-in form")
		}
		a := signInAttributesForTest(forms[0])
		if strings.ToLower(a["method"]) != "post" || a["action"] != "/admin/sign-in" {
			t.Fatal("form must POST credentials to the sign-in route")
		}
		nonce := signInCookieForTest(t, w, nonceCookieNameForTest)
		signInAssertCookieForTest(t, nonce, false)
		_ = signInRawForTest(t, nonce.Value)
		if seen[nonce.Value] {
			t.Fatal("independent form requests reused a nonce")
		}
		seen[nonce.Value] = true
		if token := signInFormTokenForTest(t, body); token != signInTokenForTest(t, nonce.Value, "preauth") {
			t.Fatal("hidden CSRF value is not the required raw-nonce-keyed preauth HMAC")
		}
		signInAssertNoSecretsForTest(t, w, nonce.Value, "https://attacker.example.test/collect")
		signInAssertNoCredentialForTest(t, w)
		signInAssertUnchangedForTest(t, f, before, 0)
	}
}

func TestSignInGETNonceReuseReplacementAndDuplicates(t *testing.T) {
	f := newSignInFixtureForTest(t)
	nonce := signInNonceForTest()
	for _, tc := range []struct{ name, value string }{
		{"canonical", nonce}, {"missing", ""}, {"short", nonce[:42]}, {"padded", nonce + "="}, {"noncanonical", nonce[:42] + "t"}, {"quoted", `"` + nonce + `"`}, {"newline", nonce[:10] + "\n" + nonce[10:]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cookies := []string{}
			if tc.name != "missing" {
				cookies = append(cookies, nonceCookieNameForTest+"="+tc.value)
			}
			before := signInDatabaseBytesForTest(t, f.path)
			for restart := 0; restart < 2; restart++ {
				// Construct a fresh Handler each time. Its form derivation cannot
				// rely on process-local CSRF state or an application secret.
				w := signInServeForTest(f, signInRequestForTest(http.MethodGet, "/admin/sign-in", "", cookies...))
				if w.Code != 200 {
					t.Fatalf("form read returned %d, want 200", w.Code)
				}
				used := nonce
				if tc.name == "canonical" {
					for _, c := range w.Result().Cookies() {
						if c.Name == nonceCookieNameForTest && c.Value != nonce {
							t.Fatal("well-formed nonce rotated and invalidated another open form")
						}
					}
				} else {
					c := signInCookieForTest(t, w, nonceCookieNameForTest)
					signInAssertCookieForTest(t, c, false)
					_ = signInRawForTest(t, c.Value)
					used = c.Value
					if used == tc.value {
						t.Fatal("malformed nonce was reused")
					}
				}
				if signInFormTokenForTest(t, w.Body.String()) != signInTokenForTest(t, used, "preauth") {
					t.Fatal("form token changed mode or depended on handler-local state")
				}
				signInAssertNoSecretsForTest(t, w, used)
				signInAssertUnchangedForTest(t, f, before, 0)
			}
		})
	}
	for _, name := range []string{nonceCookieNameForTest, sessionCookieNameForTest} {
		t.Run("duplicate-"+name, func(t *testing.T) {
			before := signInDatabaseBytesForTest(t, f.path)
			w := signInServeForTest(f, signInRequestForTest(http.MethodGet, "/admin/sign-in", "", name+"="+nonce, name+"="+nonce))
			if w.Code < 400 || w.Code >= 500 || w.Code == 404 || w.Code == 405 {
				t.Fatal("duplicate security cookies must fail closed on the sign-in form GET")
			}
			if len(w.Result().Cookies()) != 0 {
				t.Fatal("duplicate security cookies must not issue replacement credentials")
			}
			signInAssertUnchangedForTest(t, f, before, 0)
		})
	}
}

func TestSignInPOSTCSRFRejectsBeforeMutation(t *testing.T) {
	f := newSignInFixtureForTest(t)
	nonce := signInNonceForTest()
	token := signInTokenForTest(t, nonce, "preauth")
	nonceCookie := nonceCookieNameForTest + "=" + nonce
	cases := []struct {
		name, target, body string
		cookies            []string
	}{
		{"missing-nonce", "/admin/sign-in", "csrf_token=" + token, nil},
		{"missing-token", "/admin/sign-in", "email=editor%40example.test&password=anything", []string{nonceCookie}},
		{"query-token-only", "/admin/sign-in?csrf_token=" + token, "email=editor%40example.test&password=anything", []string{nonceCookie}},
		{"empty-token", "/admin/sign-in", "csrf_token=", []string{nonceCookie}},
		{"duplicate-token", "/admin/sign-in", "csrf_token=" + token + "&csrf_token=" + token, []string{nonceCookie}},
		{"different-duplicate-token", "/admin/sign-in", "csrf_token=" + token + "&csrf_token=bad", []string{nonceCookie}},
		{"short-token", "/admin/sign-in", "csrf_token=" + token[:42], []string{nonceCookie}},
		{"padded-token", "/admin/sign-in", "csrf_token=" + token + "%3D", []string{nonceCookie}},
		{"newline-token", "/admin/sign-in", "csrf_token=" + token[:10] + "%0A" + token[10:], []string{nonceCookie}},
		{"noncanonical-token", "/admin/sign-in", "csrf_token=" + signInNoncanonicalForTest(token), []string{nonceCookie}},
		{"wrong-token", "/admin/sign-in", "csrf_token=" + base64.RawURLEncoding.EncodeToString(make([]byte, 32)), []string{nonceCookie}},
		{"raw-nonce-as-token", "/admin/sign-in", "csrf_token=" + nonce, []string{nonceCookie}},
		{"authenticated-mode-token", "/admin/sign-in", "csrf_token=" + signInTokenForTest(t, nonce, "authenticated"), []string{nonceCookie}},
		{"short-nonce", "/admin/sign-in", "csrf_token=" + token, []string{nonceCookieNameForTest + "=" + nonce[:42]}},
		{"padded-nonce", "/admin/sign-in", "csrf_token=" + token, []string{nonceCookie + "="}},
		{"noncanonical-nonce", "/admin/sign-in", "csrf_token=" + token, []string{nonceCookieNameForTest + "=" + signInNoncanonicalForTest(nonce)}},
		{"duplicate-nonce", "/admin/sign-in", "csrf_token=" + token, []string{nonceCookie + "; " + nonceCookie}},
		{"duplicate-nonce-lines", "/admin/sign-in", "csrf_token=" + token, []string{nonceCookie, nonceCookie}},
		{"duplicate-session", "/admin/sign-in", "csrf_token=" + token, []string{nonceCookie, sessionCookieNameForTest + "=" + nonce, sessionCookieNameForTest + "=" + nonce}},
		{"parser-dropped-duplicate", "/admin/sign-in", "csrf_token=" + token, []string{nonceCookie, nonceCookieNameForTest + "=é"}},
	}
	logs := signInCaptureLogsForTest(t)
	var generic string
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := signInDatabaseBytesForTest(t, f.path)
			w := signInServeForTest(f, signInRequestForTest(http.MethodPost, tc.target, tc.body, tc.cookies...))
			if w.Code != http.StatusForbidden {
				t.Fatalf("invalid CSRF returned %d, want generic 403", w.Code)
			}
			if generic == "" {
				generic = w.Body.String()
			} else if generic != w.Body.String() {
				t.Fatal("CSRF rejection revealed which private input failed")
			}
			signInAssertNoCredentialForTest(t, w)
			signInAssertNoSecretsForTest(t, w, nonce, token, signInOpaquePassword, f.hash)
			signInAssertLogsForTest(t, logs, nonce, token, signInOpaquePassword, f.hash)
			submitted, err := url.ParseQuery(tc.body)
			if err != nil {
				t.Fatal("invalid private CSRF test fixture")
			}
			for _, value := range submitted["csrf_token"] {
				signInAssertNoSecretsForTest(t, w, value)
				signInAssertLogsForTest(t, logs, value)
			}
			signInAssertUnchangedForTest(t, f, before, 0)
		})
	}
}

func signInNoncanonicalForTest(canonical string) string {
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	index := strings.IndexByte(alphabet, canonical[42])
	return canonical[:42] + string(alphabet[index+1])
}

func TestSignInValidCredentialsCreateFreshPersistedSessions(t *testing.T) {
	f := newSignInFixtureForTest(t)
	ctx := context.Background()
	logs := signInCaptureLogsForTest(t)
	var previous string
	for i := 0; i < 2; i++ {
		values := signInValuesForTest(t)
		values.Set("email", "  EDITOR@EXAMPLE.TEST  ")
		values.Set("redirect", "https://attacker.example.test/collect")
		extra := []string{}
		if previous != "" {
			extra = append(extra, sessionCookieNameForTest+"="+previous)
		}
		start := time.Now().UTC().Truncate(time.Millisecond)
		cookies := append([]string{nonceCookieNameForTest + "=" + signInNonceForTest()}, extra...)
		w := signInServeForTest(f, signInRequestForTest(http.MethodPost, "/admin/sign-in?redirect=https://attacker.example.test/collect&csrf_token=query-only-decoy", values.Encode(), cookies...))
		end := time.Now().UTC().Add(time.Millisecond)
		if w.Code != http.StatusSeeOther {
			t.Fatalf("valid credentials returned %d, want 303", w.Code)
		}
		if location := w.Header().Get("Location"); location != "/admin" && location != "/admin/pages" {
			t.Fatal("success must use the existing local admin landing, not a submitted redirect or token URL")
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("sign-in response must be no-store")
		}
		cookie := signInCookieForTest(t, w, sessionCookieNameForTest)
		signInAssertCookieForTest(t, cookie, false)
		raw := signInRawForTest(t, cookie.Value)
		if cookie.Value == previous || cookie.Value == signInNonceForTest() {
			t.Fatal("successful sign-in reused a session or promoted the nonce")
		}
		deleted := signInCookieForTest(t, w, nonceCookieNameForTest)
		signInAssertCookieForTest(t, deleted, true)
		digest := sha256.Sum256(raw)
		session, err := f.repo.Store.GetSessionByTokenDigest(ctx, digest[:], start)
		if err != nil {
			t.Fatal("cookie did not identify a persisted session by SHA-256 of raw credential bytes")
		}
		if len(f.repo.writes) != i+1 || f.repo.writes[i].AccountID != f.accountID || !bytes.Equal(f.repo.writes[i].TokenDigest, digest[:]) {
			t.Fatal("sign-in did not create exactly one matching account session")
		}
		created, createdErr := time.Parse(signInTimeLayout, session.CreatedAt)
		expires, expiresErr := time.Parse(signInTimeLayout, session.ExpiresAt)
		if session.ID == "" || session.AccountID != f.accountID || session.RevokedAt != nil || createdErr != nil || expiresErr != nil || created.Before(start) || created.After(end) || expires.Sub(created) != 8*time.Hour {
			t.Fatal("persisted session has incorrect identity, creation time or absolute eight-hour lifetime")
		}
		encodedDigest := sha256.Sum256([]byte(cookie.Value))
		if _, err := f.repo.Store.GetSessionByTokenDigest(ctx, encodedDigest[:], start); !errors.Is(err, content.ErrNotFound) {
			t.Fatal("session digest incorrectly hashes encoded text instead of raw bytes")
		}
		signInAssertNoSecretsForTest(t, w, cookie.Value, signInNonceForTest(), f.hash, signInOpaquePassword, hex.EncodeToString(digest[:]), values.Get("csrf_token"), "https://attacker.example.test/collect")
		signInAssertLogsForTest(t, logs, cookie.Value, signInNonceForTest(), f.hash, signInOpaquePassword, hex.EncodeToString(digest[:]), values.Get("csrf_token"))
		if previous != "" {
			oldRaw := signInRawForTest(t, previous)
			oldDigest := sha256.Sum256(oldRaw)
			old, err := f.repo.Store.GetSessionByTokenDigest(ctx, oldDigest[:], start)
			if err != nil || old.ID == session.ID {
				t.Fatal("fresh sign-in did not create a distinct session row")
			}
		}
		previous = cookie.Value
	}
}

func TestSignInInvalidCredentialsAreIndistinguishableAndExact(t *testing.T) {
	f := newSignInFixtureForTest(t)
	ctx := context.Background()
	for _, draft := range []store.AccountDraft{
		{Email: "inactive@example.test", PasswordHash: f.hash, IsEditor: true, State: "deactivated"},
		{Email: "malformed@example.test", PasswordHash: "private-test-invalid-hash", IsEditor: true, State: "active"},
		{Email: "café@example.test", PasswordHash: f.hash, IsEditor: true, State: "active"},
	} {
		if _, err := f.repo.CreateAccount(ctx, draft, f.at); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct{ name, email, password string }{
		{"unknown", "unknown@example.test", signInOpaquePassword},
		{"wrong", "editor@example.test", "private-test-wrong-password"},
		{"deactivated", "inactive@example.test", signInOpaquePassword},
		{"malformed-hash", "malformed@example.test", signInOpaquePassword},
		{"password-trimmed", "editor@example.test", strings.TrimSpace(signInOpaquePassword)},
		{"password-folded", "editor@example.test", strings.ToLower(signInOpaquePassword)},
		{"password-unicode-normalized", "editor@example.test", strings.ReplaceAll(signInOpaquePassword, "é", "e\u0301")},
		{"password-truncated", "editor@example.test", strings.Split(signInOpaquePassword, "\x00")[0]},
		{"email-unicode-fold", "CAFÉ@EXAMPLE.TEST", signInOpaquePassword},
		{"email-unicode-trim", "\u00a0editor@example.test\u00a0", signInOpaquePassword},
		{"email-tab-trim", "\teditor@example.test\t", signInOpaquePassword},
	}
	logs := signInCaptureLogsForTest(t)
	var generic string
	var status int
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := signInValuesForTest(t)
			values.Set("email", tc.email)
			values.Set("password", tc.password)
			before := signInDatabaseBytesForTest(t, f.path)
			w := signInPostForTest(t, f, values)
			if w.Code < 200 || w.Code >= 500 || (w.Code >= 300 && w.Code < 400) || w.Code == 404 || w.Code == 405 {
				t.Fatalf("invalid credentials returned %d, want a sign-in error screen", w.Code)
			}
			if generic == "" {
				generic, status = w.Body.String(), w.Code
			} else if w.Body.String() != generic || w.Code != status {
				t.Fatal("unknown, wrong, deactivated or invalid-hash attempts had distinguishable public responses")
			}
			if strings.TrimSpace(w.Body.String()) == "" {
				t.Fatal("invalid credentials did not show an error")
			}
			signInAssertNoCredentialForTest(t, w)
			signInAssertNoSecretsForTest(t, w, signInOpaquePassword, tc.password, f.hash, signInNonceForTest(), "private-test-invalid-hash")
			signInAssertLogsForTest(t, logs, signInOpaquePassword, tc.password, f.hash, signInNonceForTest(), values.Get("csrf_token"))
			signInAssertUnchangedForTest(t, f, before, 0)
		})
	}
	// ASCII case folding must not fold the Unicode local-part itself.
	values := signInValuesForTest(t)
	values.Set("email", " CAFé@EXAMPLE.TEST ")
	if w := signInPostForTest(t, f, values); w.Code != 303 {
		t.Fatalf("ASCII-only canonicalization of Unicode email returned %d, want 303", w.Code)
	}
}

func TestSignInMissingAndDuplicateCredentialFields(t *testing.T) {
	f := newSignInFixtureForTest(t)
	for _, field := range []string{"email", "password"} {
		for _, mode := range []string{"missing", "empty", "identical-duplicate", "different-duplicate", "query-only"} {
			t.Run(field+"-"+mode, func(t *testing.T) {
				v := signInValuesForTest(t)
				target := "/admin/sign-in"
				switch mode {
				case "missing":
					v.Del(field)
				case "empty":
					v.Set(field, "")
				case "identical-duplicate":
					v.Add(field, v.Get(field))
				case "different-duplicate":
					v.Add(field, "ambiguous-private-test-value")
				case "query-only":
					target += "?" + url.Values{field: {v.Get(field)}}.Encode()
					v.Del(field)
				}
				before := signInDatabaseBytesForTest(t, f.path)
				w := signInServeForTest(f, signInRequestForTest(http.MethodPost, target, v.Encode(), nonceCookieNameForTest+"="+signInNonceForTest()))
				if w.Code < 200 || w.Code >= 500 || (w.Code >= 300 && w.Code < 400) || w.Code == 404 || w.Code == 405 {
					t.Fatal("missing or ambiguous credential field must reject on the sign-in route")
				}
				signInAssertNoCredentialForTest(t, w)
				signInAssertNoSecretsForTest(t, w, signInOpaquePassword, f.hash, "ambiguous-private-test-value")
				signInAssertUnchangedForTest(t, f, before, 0)
			})
		}
	}
}

func TestSignInPOSTUsesNonceModeEvenWithAuthenticatedSession(t *testing.T) {
	f := newSignInFixtureForTest(t)
	raw := bytes.Repeat([]byte{0x42}, 32)
	credential := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256(raw)
	if _, err := f.repo.Store.CreateSession(context.Background(), store.SessionDraft{AccountID: f.accountID, TokenDigest: digest[:]}, f.at); err != nil {
		t.Fatal(err)
	}
	handler := SessionMiddleware(f.repo, time.Now, Handler(f.repo, "8443"))
	for _, mode := range []string{"authenticated", "missing-nonce", "preauth"} {
		t.Run(mode, func(t *testing.T) {
			values := signInValuesForTest(t)
			if mode == "authenticated" {
				values.Set("csrf_token", signInTokenForTest(t, credential, mode))
			}
			before := signInDatabaseBytesForTest(t, f.path)
			w := httptest.NewRecorder()
			cookies := []string{sessionCookieNameForTest + "=" + credential}
			if mode != "missing-nonce" {
				cookies = append(cookies, nonceCookieNameForTest+"="+signInNonceForTest())
			}
			handler.ServeHTTP(w, signInRequestForTest(http.MethodPost, "/admin/sign-in", values.Encode(), cookies...))
			if mode != "preauth" {
				if w.Code != 403 {
					t.Fatalf("session could not replace nonce-mode CSRF: returned %d, want 403", w.Code)
				}
				signInAssertUnchangedForTest(t, f, before, 0)
				signInAssertNoCredentialForTest(t, w)
			} else {
				if w.Code != 303 {
					t.Fatalf("preauth token with a valid session returned %d, want 303", w.Code)
				}
				if got := signInCookieForTest(t, w, sessionCookieNameForTest).Value; got == credential || got == signInNonceForTest() {
					t.Fatal("existing session or nonce was reused")
				}
			}
		})
	}
}

func TestSignInRequestBoundaries(t *testing.T) {
	f := newSignInFixtureForTest(t)
	for _, tc := range []struct {
		name   string
		status int
		change func(*http.Request)
	}{
		{"unexpected-host", 400, func(r *http.Request) { r.Host = "attacker.example.test:8443" }},
		{"cross-site-metadata", 403, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }},
		{"cross-site-origin", 403, func(r *http.Request) { r.Header.Set("Origin", "https://attacker.example.test") }},
		{"json", 415, func(r *http.Request) { r.Header.Set("Content-Type", "application/json") }},
		{"oversized-known-length", 413, func(r *http.Request) {
			body := strings.Repeat("x", (1<<20)+1)
			r.Body = io.NopCloser(strings.NewReader(body))
			r.ContentLength = int64(len(body))
		}},
		{"oversized-stream", 413, func(r *http.Request) {
			r.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", (1<<20)+1)))
			r.ContentLength = -1
		}},
		{"invalid-form-encoding", 400, func(r *http.Request) {
			r.Body = io.NopCloser(strings.NewReader("csrf_token=%zz"))
			r.ContentLength = -1
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := signInDatabaseBytesForTest(t, f.path)
			lookups := len(f.repo.lookups)
			r := signInRequestForTest(http.MethodPost, "/admin/sign-in", signInValuesForTest(t).Encode(), nonceCookieNameForTest+"="+signInNonceForTest())
			tc.change(r)
			w := signInServeForTest(f, r)
			if w.Code != tc.status {
				t.Fatalf("boundary returned %d, want %d", w.Code, tc.status)
			}
			if len(f.repo.lookups) != lookups {
				t.Fatal("rejected boundary reached account lookup")
			}
			signInAssertUnchangedForTest(t, f, before, 0)
			signInAssertNoCredentialForTest(t, w)
		})
	}
	// The old local prototype is not globally guarded until #81.
	if w := signInServeForTest(f, signInRequestForTest(http.MethodGet, "/admin/pages", "")); w.Code != 200 {
		t.Fatal("#75 unexpectedly guarded unrelated prototype routes")
	}
	// Secure is mandatory even when the same handler is reached over HTTP.
	r := signInRequestForTest(http.MethodGet, "/admin/sign-in", "")
	r.TLS = nil
	r.URL.Scheme = "http"
	w := signInServeForTest(f, r)
	if w.Code != 200 {
		t.Fatalf("local form GET returned %d, want 200", w.Code)
	}
	signInAssertCookieForTest(t, signInCookieForTest(t, w, nonceCookieNameForTest), false)
}

type signInEntropyErrorForTest struct{}

func (signInEntropyErrorForTest) Read([]byte) (int, error) {
	return 0, errors.New("private-test-entropy-unavailable")
}

func TestSignInEntropyAndStoreFailuresAreAtomicAndRedacted(t *testing.T) {
	f := newSignInFixtureForTest(t)
	var logs bytes.Buffer
	oldLog, oldSlog := log.Writer(), slog.Default()
	t.Cleanup(func() { slog.SetDefault(oldSlog); log.SetOutput(oldLog) })
	log.SetOutput(&logs)
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	for _, tc := range []struct {
		name                        string
		entropy                     io.Reader
		lookupFailure, writeFailure bool
		method                      string
	}{
		{name: "nonce-entropy-error", entropy: signInEntropyErrorForTest{}, method: http.MethodGet},
		{name: "nonce-short-entropy", entropy: strings.NewReader("short-private-test"), method: http.MethodGet},
		{name: "session-entropy-error", entropy: signInEntropyErrorForTest{}, method: http.MethodPost},
		{name: "session-short-entropy", entropy: strings.NewReader("short-private-test"), method: http.MethodPost},
		{name: "account-read-error", lookupFailure: true, method: http.MethodPost},
		{name: "session-write-error", writeFailure: true, method: http.MethodPost},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := signInDatabaseBytesForTest(t, f.path)
			writes := len(f.repo.writes)
			private := errors.New(signInOpaquePassword + " " + f.hash + " " + signInNonceForTest() + " " + signInTokenForTest(t, signInNonceForTest(), "preauth"))
			if tc.lookupFailure {
				f.repo.lookupErr = private
			}
			if tc.writeFailure {
				f.repo.writeErr = private
			}
			t.Cleanup(func() { f.repo.lookupErr = nil; f.repo.writeErr = nil })
			if tc.entropy != nil {
				old := rand.Reader
				rand.Reader = tc.entropy
				t.Cleanup(func() { rand.Reader = old })
			}
			logs.Reset()
			r := signInRequestForTest(tc.method, "/admin/sign-in", "")
			if tc.method == http.MethodPost {
				r = signInRequestForTest(tc.method, "/admin/sign-in", signInValuesForTest(t).Encode(), nonceCookieNameForTest+"="+signInNonceForTest())
			}
			w := signInServeForTest(f, r)
			if w.Code < 400 || w.Code >= 600 || w.Code == 404 || w.Code == 405 {
				t.Fatalf("operational failure returned %d, want a sign-in failure response", w.Code)
			}
			wantWrites := writes
			if tc.writeFailure {
				wantWrites++
			}
			if len(f.repo.writes) != wantWrites || !reflect.DeepEqual(before, signInDatabaseBytesForTest(t, f.path)) {
				t.Fatal("operational failure left a session row or attempted a write before entropy was complete")
			}
			signInAssertNoCredentialForTest(t, w)
			if tc.method == http.MethodGet && len(w.Result().Cookies()) != 0 {
				t.Fatal("nonce entropy failure emitted a partial nonce")
			}
			signInAssertNoSecretsForTest(t, w, signInOpaquePassword, f.hash, signInNonceForTest(), signInTokenForTest(t, signInNonceForTest(), "preauth"))
			for _, secret := range []string{signInOpaquePassword, f.hash, signInNonceForTest(), signInTokenForTest(t, signInNonceForTest(), "preauth")} {
				if strings.Contains(logs.String(), secret) {
					t.Fatal("operational failure logged private test data")
				}
			}
		})
	}
}

func TestSignInTLSJourneyAndRestart(t *testing.T) {
	f := newSignInFixtureForTest(t)
	var routes http.Handler
	// Handler's normal Host/COP boundary remains in front. A test-only consumer
	// observes #74 context on the real admin landing, without adding auth policy.
	server := httptest.NewTestServer(t, SessionMiddleware(f.repo, time.Now, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/pages" {
			if session, account, ok := SessionFromContext(r.Context()); ok {
				w.Header().Set("X-Test-Session-ID", session.ID)
				w.Header().Set("X-Test-Account-ID", account.ID)
			}
		}
		routes.ServeHTTP(w, r)
	})))
	server.StartTLS()
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	routes = Handler(f.repo, port)
	client := server.Client()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Get(server.URL + "/admin/sign-in")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		t.Fatalf("TLS form returned %d, want 200", response.StatusCode)
	}
	token := signInFormTokenForTest(t, string(body))
	response, err = client.PostForm(server.URL+"/admin/sign-in", url.Values{"email": {" EDITOR@EXAMPLE.TEST "}, "password": {signInOpaquePassword}, "csrf_token": {token}})
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 303 {
		t.Fatalf("TLS credential POST returned %d, want 303", response.StatusCode)
	}
	location := response.Header.Get("Location")
	if location != "/admin" && location != "/admin/pages" {
		t.Fatal("TLS sign-in did not redirect to a local admin landing")
	}
	var credential string
	for _, cookie := range response.Cookies() {
		if cookie.Name == sessionCookieNameForTest {
			signInAssertCookieForTest(t, cookie, false)
			credential = cookie.Value
		}
	}
	raw := signInRawForTest(t, credential)
	digest := sha256.Sum256(raw)
	before, err := f.repo.Store.GetSessionByTokenDigest(context.Background(), digest[:], time.Now())
	if err != nil {
		t.Fatal("TLS credential has no persisted session")
	}
	landing, err := client.Get(server.URL + location)
	if err != nil {
		t.Fatal(err)
	}
	_ = landing.Body.Close()
	if location == "/admin" && landing.StatusCode == 303 && landing.Header.Get("Location") == "/admin/pages" {
		landing, err = client.Get(server.URL + "/admin/pages")
		if err != nil {
			t.Fatal(err)
		}
		_ = landing.Body.Close()
	}
	if landing.StatusCode != 200 || landing.Header.Get("X-Test-Session-ID") != before.ID || landing.Header.Get("X-Test-Account-ID") != f.accountID {
		t.Fatal("browser cookie did not reach middleware-loaded matching session/account context")
	}
	for _, cookie := range jar.Cookies(mustSignInURLForTest(t, server.URL)) {
		if cookie.Name == nonceCookieNameForTest {
			t.Fatal("successful TLS sign-in left the nonce in the browser jar")
		}
	}
	// A later form GET still uses nonce mode despite the authenticated cookie,
	// and creates a new nonce only because success cleared the previous one.
	databaseBeforeForm := signInDatabaseBytesForTest(t, f.path)
	form, err := client.Get(server.URL + "/admin/sign-in")
	if err != nil {
		t.Fatal(err)
	}
	formBody, err := io.ReadAll(form.Body)
	_ = form.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if form.StatusCode != 200 {
		t.Fatalf("authenticated browser's sign-in GET returned %d, want 200", form.StatusCode)
	}
	var freshNonce string
	for _, cookie := range form.Cookies() {
		if cookie.Name == nonceCookieNameForTest {
			signInAssertCookieForTest(t, cookie, false)
			freshNonce = cookie.Value
		}
	}
	_ = signInRawForTest(t, freshNonce)
	freshToken := signInFormTokenForTest(t, string(formBody))
	if freshToken != signInTokenForTest(t, freshNonce, "preauth") || freshToken == token || freshToken == signInTokenForTest(t, credential, "authenticated") {
		t.Fatal("authenticated browser's form used session mode or reused the consumed nonce")
	}
	signInAssertUnchangedForTest(t, f, databaseBeforeForm, 1)
	// A new connection/Store and a new middleware instance represent restart.
	reopened, err := store.Open(context.Background(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	created, err := time.Parse(signInTimeLayout, before.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	for _, elapsed := range []time.Duration{7 * time.Hour, 8 * time.Hour} {
		w := httptest.NewRecorder()
		SessionMiddleware(reopened, func() time.Time { return created.Add(elapsed) }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session, account, ok := SessionFromContext(r.Context())
			if ok != (elapsed < 8*time.Hour) {
				t.Error("restart or exact absolute expiry changed authentication validity")
			}
			if ok && (session.ID != before.ID || account.ID != f.accountID) {
				t.Error("restart loaded different persisted identity")
			}
			w.WriteHeader(204)
		})).ServeHTTP(w, signInRequestForTest(http.MethodGet, "/admin/pages", "", sessionCookieNameForTest+"="+credential))
	}
	after, err := reopened.GetSessionByTokenDigest(context.Background(), digest[:], created)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("restart/read extended expiry or changed persisted session")
	}
	isolated, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	host := mustSignInURLForTest(t, "https://cookie.example.test/")
	isolated.SetCookies(host, response.Cookies())
	host.Scheme = "http"
	for _, cookie := range isolated.Cookies(host) {
		if cookie.Name == sessionCookieNameForTest {
			t.Fatal("Secure credential was available over non-loopback HTTP")
		}
	}
}

func signInCaptureLogsForTest(t *testing.T) *bytes.Buffer {
	t.Helper()
	logs := new(bytes.Buffer)
	oldLog, oldSlog := log.Writer(), slog.Default()
	t.Cleanup(func() { slog.SetDefault(oldSlog); log.SetOutput(oldLog) })
	log.SetOutput(logs)
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	return logs
}

func signInAssertLogsForTest(t *testing.T, logs *bytes.Buffer, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		quoted := strconv.Quote(secret)
		if secret != "" && (strings.Contains(logs.String(), secret) || strings.Contains(logs.String(), quoted[1:len(quoted)-1]) || strings.Contains(logs.String(), url.QueryEscape(secret))) {
			t.Fatal("sign-in logged private test data")
		}
	}
}

type signInRecordingEntropyForTest struct {
	source io.Reader
	reads  [][]byte
}

func (r *signInRecordingEntropyForTest) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	r.reads = append(r.reads, bytes.Clone(p[:n]))
	return n, err
}

func TestSignInCredentialsUseCryptographicSource(t *testing.T) {
	f := newSignInFixtureForTest(t)
	original := rand.Reader
	recording := &signInRecordingEntropyForTest{source: original}
	rand.Reader = recording
	t.Cleanup(func() { rand.Reader = original })
	get := signInServeForTest(f, signInRequestForTest(http.MethodGet, "/admin/sign-in", ""))
	if get.Code != 200 {
		t.Fatalf("form GET returned %d, want 200", get.Code)
	}
	nonce := signInCookieForTest(t, get, nonceCookieNameForTest).Value
	rawNonce := signInRawForTest(t, nonce)
	if len(recording.reads) != 1 || !bytes.Equal(recording.reads[0], rawNonce) {
		t.Fatal("nonce must use exactly 32 fresh bytes from crypto/rand.Reader")
	}
	recording.reads = nil
	values := signInValuesForTest(t)
	values.Set("csrf_token", signInFormTokenForTest(t, get.Body.String()))
	post := signInServeForTest(f, signInRequestForTest(http.MethodPost, "/admin/sign-in", values.Encode(), nonceCookieNameForTest+"="+nonce))
	if post.Code != 303 {
		t.Fatalf("credential POST returned %d, want 303", post.Code)
	}
	rawCredential := signInRawForTest(t, signInCookieForTest(t, post, sessionCookieNameForTest).Value)
	// The store also consumes cryptographic bytes for the UUIDv7 row ID.
	// Credential bytes must come from the source, not a time/ID/nonce digest.
	found := false
	for _, read := range recording.reads {
		if bytes.Equal(read, rawCredential) && len(read) == 32 {
			found = true
		}
	}
	if !found || bytes.Equal(rawCredential, rawNonce) {
		t.Fatal("session credential was not 32 fresh cryptographic source bytes distinct from the nonce")
	}
}

func mustSignInURLForTest(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
