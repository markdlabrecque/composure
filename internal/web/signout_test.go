package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
	"github.com/markdlabrecque/composure/internal/store"
)

type signOutRepositoryForTest struct {
	*store.Store
	revokeErr error
}

func (s *signOutRepositoryForTest) RevokeSession(ctx context.Context, sessionID string, at time.Time) error {
	if s.revokeErr != nil {
		return s.revokeErr
	}
	return s.Store.RevokeSession(ctx, sessionID, at)
}

type signOutFixtureForTest struct {
	repository                               *signOutRepositoryForTest
	path, currentCredential, otherCredential string
	currentDigest, otherDigest               []byte
	now                                      time.Time
}

func newSignOutFixtureForTest(t *testing.T) signOutFixtureForTest {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	path := filepath.Join(t.TempDir(), "composure.db")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(context.Background(), path, "signout-test-site", now, nil); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	accountID, err := s.CreateAccount(context.Background(), store.AccountDraft{
		Email: "signout@example.test", PasswordHash: "private-test-hash", IsEditor: true, State: "active",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	credential := func(value byte) (string, []byte) {
		raw := bytes.Repeat([]byte{value}, 32)
		digest := sha256.Sum256(raw)
		return base64.RawURLEncoding.EncodeToString(raw), digest[:]
	}
	currentCredential, currentDigest := credential(0x76)
	otherCredential, otherDigest := credential(0x77)
	for _, digest := range [][]byte{currentDigest, otherDigest} {
		if _, err := s.CreateSession(context.Background(), store.SessionDraft{AccountID: accountID, TokenDigest: digest}, now); err != nil {
			t.Fatal(err)
		}
	}
	return signOutFixtureForTest{
		repository: &signOutRepositoryForTest{Store: s}, path: path,
		currentCredential: currentCredential, otherCredential: otherCredential,
		currentDigest: currentDigest, otherDigest: otherDigest, now: now,
	}
}

func signOutRequestForTest(t *testing.T, method string, credential string) *http.Request {
	t.Helper()
	body := ""
	if method == http.MethodPost && credential != "" {
		body = url.Values{"csrf_token": {guardCSRFToken(t, credential)}}.Encode()
	}
	r := httptest.NewRequest(method, "https://127.0.0.1:8443/admin/sign-out", strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if credential != "" {
		r.Header.Set("Cookie", sessionCookieName+"="+credential)
	}
	return r
}

func signOutServeForTest(f signOutFixtureForTest, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	Handler(f.repository, "8443").ServeHTTP(w, r)
	return w
}

func signOutAssertUsableForTest(t *testing.T, s *store.Store, digest []byte, at time.Time, usable bool) {
	t.Helper()
	_, err := s.GetSessionByTokenDigest(context.Background(), digest, at)
	if usable && err != nil {
		t.Fatalf("session unexpectedly unusable: %v", err)
	}
	if !usable && !errors.Is(err, content.ErrNotFound) {
		t.Fatalf("session remained usable or failed unexpectedly: %v", err)
	}
}

func TestSignOutRevokesOnlyCurrentPersistedSessionAndClearsCookie(t *testing.T) {
	f := newSignOutFixtureForTest(t)
	w := signOutServeForTest(f, signOutRequestForTest(t, http.MethodPost, f.currentCredential))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/sign-in" {
		t.Fatalf("sign-out response = %d location %q, want 303 /admin/sign-in", w.Code, w.Header().Get("Location"))
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("sign-out emitted %d cookies, want one session deletion", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != sessionCookieName || cookie.Value != "" || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" || strings.Contains(strings.ToLower(cookie.Raw), "domain=") {
		t.Fatal("sign-out must clear the host-only session cookie with its Secure, HttpOnly, SameSite=Lax and Path=/ scope")
	}
	if cookie.MaxAge >= 0 && (cookie.Expires.IsZero() || !cookie.Expires.Before(time.Now())) {
		t.Fatal("sign-out cookie did not carry a deletion directive")
	}

	signOutAssertUsableForTest(t, f.repository.Store, f.currentDigest, f.now, false)
	signOutAssertUsableForTest(t, f.repository.Store, f.otherDigest, f.now, true)
	if err := f.repository.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(context.Background(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	signOutAssertUsableForTest(t, reopened, f.currentDigest, f.now, false)
	signOutAssertUsableForTest(t, reopened, f.otherDigest, f.now, true)
}

func TestSignOutRefusesMissingInvalidAndDuplicateCredentialsWithoutRevocation(t *testing.T) {
	unknown := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x78}, 32))
	cases := []struct {
		name    string
		cookies []string
	}{
		{name: "missing"},
		{name: "malformed", cookies: []string{sessionCookieName + "=malformed"}},
		{name: "unknown", cookies: []string{sessionCookieName + "=" + unknown}},
		{name: "duplicate", cookies: []string{sessionCookieName + "=" + unknown, sessionCookieName + "=" + unknown}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSignOutFixtureForTest(t)
			r := signOutRequestForTest(t, http.MethodPost, "")
			for _, cookie := range tc.cookies {
				r.Header.Add("Cookie", cookie)
			}
			w := signOutServeForTest(f, r)
			if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/sign-in" {
				t.Fatalf("refused sign-out = %d location %q, want sign-in redirect", w.Code, w.Header().Get("Location"))
			}
			signOutAssertUsableForTest(t, f.repository.Store, f.currentDigest, f.now, true)
			signOutAssertUsableForTest(t, f.repository.Store, f.otherDigest, f.now, true)
		})
	}
}

func TestSignOutSafeMethodsAndCrossOriginPostCannotRevoke(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			f := newSignOutFixtureForTest(t)
			w := signOutServeForTest(f, signOutRequestForTest(t, method, f.currentCredential))
			if w.Code == http.StatusSeeOther && w.Header().Get("Location") == "/admin/sign-in" {
				t.Fatal("safe method reported successful sign-out")
			}
			signOutAssertUsableForTest(t, f.repository.Store, f.currentDigest, f.now, true)
		})
	}

	t.Run("cross-origin POST", func(t *testing.T) {
		f := newSignOutFixtureForTest(t)
		r := signOutRequestForTest(t, http.MethodPost, f.currentCredential)
		r.Header.Set("Origin", "https://attacker.example")
		w := signOutServeForTest(f, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("cross-origin sign-out returned %d, want 403", w.Code)
		}
		signOutAssertUsableForTest(t, f.repository.Store, f.currentDigest, f.now, true)
	})
}

func TestSignOutStorageFailureCannotReportSuccessOrClearCookie(t *testing.T) {
	f := newSignOutFixtureForTest(t)
	f.repository.revokeErr = errors.New("private test revocation failure")
	w := signOutServeForTest(f, signOutRequestForTest(t, http.MethodPost, f.currentCredential))
	if w.Code < 500 || w.Code >= 600 {
		t.Fatalf("failed revocation returned %d, want a server error", w.Code)
	}
	if w.Header().Get("Location") != "" {
		t.Fatal("failed revocation redirected as though sign-out succeeded")
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == sessionCookieName && cookie.Value == "" {
			t.Fatal("failed revocation cleared the browser credential")
		}
	}
	signOutAssertUsableForTest(t, f.repository.Store, f.currentDigest, f.now, true)
}
