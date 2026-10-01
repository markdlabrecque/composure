package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Retain the credential-error -> correction journey from review round 1.
// A validated CSRF token belongs in the retry form, unlike a CSRF rejection.
func TestSignInRetryCredentialErrorTLSJourney(t *testing.T) {
	for _, mode := range []string{"wrong-password", "unknown-email", "missing-password"} {
		t.Run(mode, func(t *testing.T) {
			f := newSignInFixtureForTest(t)
			logs := signInCaptureLogsForTest(t)
			var routes http.Handler
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
			client.Jar, err = cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			siteURL := mustSignInURLForTest(t, server.URL)
			before := signInDatabaseBytesForTest(t, f.path)

			response, err := client.Get(server.URL + "/admin/sign-in")
			form := signInRetryResponseForTest(t, response, err)
			if form.Code != http.StatusOK {
				t.Fatalf("initial form status=%d, want 200", form.Code)
			}
			nonce := signInCookieForTest(t, form, nonceCookieNameForTest)
			signInAssertCookieForTest(t, nonce, false)
			_ = signInRawForTest(t, nonce.Value)
			expected := signInTokenForTest(t, nonce.Value, "preauth")
			if signInFormTokenForTest(t, form.Body.String()) != expected {
				t.Fatal("initial form token is not the raw-nonce-keyed preauth HMAC")
			}

			// A second open form shares the nonce. A failed attempt must not
			// invalidate this form or the form returned with the error.
			response, err = client.Get(server.URL + "/admin/sign-in")
			otherForm := signInRetryResponseForTest(t, response, err)
			if otherForm.Code != http.StatusOK {
				t.Fatalf("second form status=%d, want 200", otherForm.Code)
			}
			otherToken := signInFormTokenForTest(t, otherForm.Body.String())
			if otherToken != expected {
				t.Fatal("opening another form changed the nonce-derived token")
			}
			signInAssertUnchangedForTest(t, f, before, 0)

			bad := url.Values{"email": {"editor@example.test"}, "password": {signInOpaquePassword}, "csrf_token": {expected}}
			switch mode {
			case "wrong-password":
				bad.Set("password", "private-retry-wrong-password")
			case "unknown-email":
				bad.Set("email", "unknown@example.test")
			case "missing-password":
				bad.Del("password")
			}
			response, err = client.PostForm(server.URL+"/admin/sign-in", bad)
			rejected := signInRetryResponseForTest(t, response, err)
			if rejected.Code != http.StatusUnauthorized {
				t.Fatalf("credential error status=%d, want 401", rejected.Code)
			}
			signInRetryAssertFailureForTest(t, f, rejected, before, nonce.Value)
			signInAssertNoSecretsForTest(t, rejected, nonce.Value, f.hash, signInOpaquePassword, "private-retry-wrong-password")

			// Extract even an empty field so red evidence includes the actual
			// correction POST, not just a token-format assertion.
			var rendered string
			count := 0
			for _, tag := range signInInputPatternForTest.FindAllString(rejected.Body.String(), -1) {
				attrs := signInAttributesForTest(tag)
				if attrs["name"] == "csrf_token" {
					count++
					rendered = attrs["value"]
					if attrs["type"] != "hidden" {
						t.Error("credential-error CSRF field must be hidden")
					}
				}
			}
			if count != 1 || rendered != expected {
				t.Errorf("credential-error form has %d CSRF fields, token length=%d; want exactly one valid nonce-derived hidden token", count, len(rendered))
			} else {
				_ = signInFormTokenForTest(t, rejected.Body.String())
			}
			for _, cookie := range client.Jar.Cookies(siteURL) {
				if cookie.Name == nonceCookieNameForTest && cookie.Value != nonce.Value {
					t.Fatal("credential failure rotated the browser nonce")
				}
				if cookie.Name == sessionCookieNameForTest {
					t.Fatal("credential failure authenticated the browser")
				}
			}
			if !signInRetryJarHasNonceForTest(client.Jar, siteURL, nonce.Value) {
				t.Fatal("credential failure cleared the browser nonce")
			}

			otherBad := url.Values{"email": {"editor@example.test"}, "password": {"private-retry-wrong-password"}, "csrf_token": {otherToken}}
			response, err = client.PostForm(server.URL+"/admin/sign-in", otherBad)
			otherRejected := signInRetryResponseForTest(t, response, err)
			if otherRejected.Code != http.StatusUnauthorized {
				t.Fatalf("other open form status=%d, want credential error 401, not CSRF rejection", otherRejected.Code)
			}
			signInRetryAssertFailureForTest(t, f, otherRejected, before, nonce.Value)
			response, err = client.Get(server.URL + "/admin/pages")
			anonymous := signInRetryResponseForTest(t, response, err)
			if anonymous.Header().Get("X-Test-Session-ID") != "" || anonymous.Header().Get("X-Test-Account-ID") != "" {
				t.Fatal("failed sign-in established authenticated context")
			}
			signInAssertUnchangedForTest(t, f, before, 0)

			correct := url.Values{"email": {"editor@example.test"}, "password": {signInOpaquePassword}, "csrf_token": {rendered}}
			response, err = client.PostForm(server.URL+"/admin/sign-in", correct)
			retry := signInRetryResponseForTest(t, response, err)
			if retry.Code != http.StatusSeeOther {
				t.Fatalf("corrected credentials using error-form token returned %d, want 303", retry.Code)
			}
			location := retry.Header().Get("Location")
			if location != "/admin" && location != "/admin/pages" {
				t.Fatal("retry did not redirect to the local admin landing")
			}
			cookie := signInCookieForTest(t, retry, sessionCookieNameForTest)
			signInAssertCookieForTest(t, cookie, false)
			raw := signInRawForTest(t, cookie.Value)
			if cookie.Value == nonce.Value || cookie.Value == rendered {
				t.Fatal("retry promoted the nonce or CSRF token to a session credential")
			}
			signInAssertCookieForTest(t, signInCookieForTest(t, retry, nonceCookieNameForTest), true)
			for _, remaining := range client.Jar.Cookies(siteURL) {
				if remaining.Name == nonceCookieNameForTest {
					t.Fatal("successful retry did not clear the browser nonce")
				}
			}
			digest := sha256.Sum256(raw)
			session, err := f.repo.Store.GetSessionByTokenDigest(context.Background(), digest[:], time.Now())
			if err != nil || session.ID == "" || session.AccountID != f.accountID || !bytes.Equal(session.TokenDigest, digest[:]) {
				t.Fatal("retry cookie has no matching persisted account/session")
			}
			if len(f.repo.writes) != 1 || f.repo.writes[0].AccountID != f.accountID || !bytes.Equal(f.repo.writes[0].TokenDigest, digest[:]) {
				t.Fatal("retry must create exactly one fresh matching session")
			}
			response, err = client.Get(server.URL + "/admin/pages")
			landing := signInRetryResponseForTest(t, response, err)
			if landing.Code != http.StatusOK || landing.Header().Get("X-Test-Session-ID") != session.ID || landing.Header().Get("X-Test-Account-ID") != f.accountID {
				t.Fatal("retry browser cookie did not load matching authenticated context")
			}
			signInAssertNoSecretsForTest(t, retry, nonce.Value, expected, cookie.Value, f.hash, signInOpaquePassword)
			signInAssertLogsForTest(t, logs, nonce.Value, expected, cookie.Value, f.hash, signInOpaquePassword, "private-retry-wrong-password")
		})
	}
}

func TestSignInRetryUsesValidatedNonceNotPreparsedToken(t *testing.T) {
	f := newSignInFixtureForTest(t)
	for _, mode := range []string{"wrong-password", "missing-password"} {
		t.Run(mode, func(t *testing.T) {
			values := signInValuesForTest(t)
			values.Set("password", "private-retry-wrong-password")
			if mode == "missing-password" {
				values.Del("password")
			}
			before := signInDatabaseBytesForTest(t, f.path)
			r := signInRequestForTest(http.MethodPost, "/admin/sign-in?csrf_token=private-retry-decoy", values.Encode(), nonceCookieNameForTest+"="+signInNonceForTest())
			r.PostForm = url.Values{"csrf_token": {"private-retry-decoy"}}
			r.Form = r.PostForm
			w := signInServeForTest(f, r)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("valid raw-body CSRF credential error status=%d, want 401", w.Code)
			}
			signInRetryAssertFailureForTest(t, f, w, before, signInNonceForTest())
			signInAssertNoSecretsForTest(t, w, "private-retry-decoy", signInNonceForTest())
			if signInFormTokenForTest(t, w.Body.String()) != signInTokenForTest(t, signInNonceForTest(), "preauth") {
				t.Fatal("retry token did not match validated raw nonce derivation")
			}
		})
	}
}

func TestSignInRetryCSRFRejectionDoesNotRenderToken(t *testing.T) {
	f := newSignInFixtureForTest(t)
	logs := signInCaptureLogsForTest(t)
	var generic string
	for _, mode := range []string{"missing", "mismatch", "duplicate", "missing-password-mismatch"} {
		t.Run(mode, func(t *testing.T) {
			values := signInValuesForTest(t)
			values.Set("password", "private-retry-wrong-password")
			switch mode {
			case "missing":
				values.Del("csrf_token")
			case "mismatch", "missing-password-mismatch":
				values.Set("csrf_token", signInNonceForTest())
				if mode == "missing-password-mismatch" {
					values.Del("password")
				}
			case "duplicate":
				values.Add("csrf_token", values.Get("csrf_token"))
			}
			before := signInDatabaseBytesForTest(t, f.path)
			lookups := len(f.repo.lookups)
			w := signInPostForTest(t, f, values)
			if w.Code != http.StatusForbidden || len(f.repo.lookups) != lookups {
				t.Fatal("invalid CSRF must return generic 403 before credential lookup")
			}
			if generic == "" {
				generic = w.Body.String()
			} else if w.Body.String() != generic {
				t.Fatal("CSRF rejection responses were distinguishable")
			}
			if len(w.Result().Cookies()) != 0 {
				t.Fatal("CSRF rejection set or cleared a security cookie")
			}
			signInAssertNoSecretsForTest(t, w, signInNonceForTest(), signInTokenForTest(t, signInNonceForTest(), "preauth"), f.hash, "private-retry-wrong-password")
			signInAssertLogsForTest(t, logs, signInNonceForTest(), signInTokenForTest(t, signInNonceForTest(), "preauth"), f.hash, "private-retry-wrong-password")
			signInAssertUnchangedForTest(t, f, before, 0)
		})
	}
}

func signInRetryResponseForTest(t *testing.T, response *http.Response, err error) *httptest.ResponseRecorder {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	w := httptest.NewRecorder()
	for name, values := range response.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(body)
	return w
}

func signInRetryAssertFailureForTest(t *testing.T, f signInFixtureForTest, w *httptest.ResponseRecorder, before map[string][]byte, nonce string) {
	t.Helper()
	if w.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatal("credential-error form must be uncached HTML")
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == sessionCookieNameForTest {
			t.Fatal("credential error must not set or clear a session cookie")
		}
		if cookie.Name == nonceCookieNameForTest {
			if cookie.Value != nonce {
				t.Fatal("credential error changed or cleared the nonce")
			}
			signInAssertCookieForTest(t, cookie, false)
		}
	}
	signInAssertUnchangedForTest(t, f, before, 0)
}

func signInRetryJarHasNonceForTest(jar http.CookieJar, siteURL *url.URL, nonce string) bool {
	for _, cookie := range jar.Cookies(siteURL) {
		if cookie.Name == nonceCookieNameForTest && cookie.Value == nonce {
			return true
		}
	}
	return false
}
