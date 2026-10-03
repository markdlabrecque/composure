package web

import (
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

type signInVerifyCallForTest struct {
	password string
	encoded  string
}

type signInVerifierForTest struct {
	calls  []signInVerifyCallForTest
	result bool
	err    error
}

func (s *signInVerifierForTest) verify(password, encoded string) (bool, error) {
	s.calls = append(s.calls, signInVerifyCallForTest{password: password, encoded: encoded})
	return s.result, s.err
}

func signInPostWithDependenciesForTest(t *testing.T, f signInFixtureForTest, values url.Values, remoteAddr string, deps signInDependencies) *httptest.ResponseRecorder {
	t.Helper()
	r := signInRequestForTest(http.MethodPost, "/admin/sign-in", values.Encode(), nonceCookieNameForTest+"="+signInNonceForTest())
	r.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	serveSignInPostWithDependencies(w, r, f.repo, deps)
	return w
}

func signInAssertGenericFailureForTest(t *testing.T, got, want *httptest.ResponseRecorder) {
	t.Helper()
	if got.Code != http.StatusUnauthorized || got.Code != want.Code || got.Body.String() != want.Body.String() {
		t.Fatal("throttled, unknown-account and wrong-password failures must use the same generic response")
	}
	signInAssertNoCredentialForTest(t, got)
}

func TestSignInThrottleRejectsCanonicalAccountBeforePasswordVerification(t *testing.T) {
	for _, tc := range []struct {
		name    string
		email   string
		encoded func(signInFixtureForTest) string
	}{
		{name: "known", email: " Editor@Example.Test ", encoded: func(f signInFixtureForTest) string { return f.hash }},
		{name: "unknown", email: " Missing@Example.Test ", encoded: func(signInFixtureForTest) string { return signInDummyPasswordHash }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSignInFixtureForTest(t)
			at := time.Date(2026, time.October, 3, 9, 30, 0, 456000000, time.FixedZone("test", -7*60*60))
			verifier := &signInVerifierForTest{}
			deps := signInDependencies{now: func() time.Time { return at }, verify: verifier.verify}
			values := signInValuesForTest(t)
			values.Set("email", tc.email)
			values.Set("password", "private-test-attempt")

			var generic *httptest.ResponseRecorder
			for attempt := 1; attempt <= 6; attempt++ {
				w := signInPostWithDependenciesForTest(t, f, values, "192.0.2."+strconv.Itoa(attempt)+":8443", deps)
				if w.Code != http.StatusUnauthorized {
					t.Fatalf("admitted attempt %d returned %d, want 401", attempt, w.Code)
				}
				if attempt == 1 {
					generic = w
				}
			}
			if len(verifier.calls) != 6 {
				t.Fatalf("verifier called %d times for six admitted requests", len(verifier.calls))
			}
			for _, call := range verifier.calls {
				if call.password != "private-test-attempt" || call.encoded != tc.encoded(f) {
					t.Fatal("admitted request did not perform the expected real or dummy password work")
				}
			}

			values.Set("email", "  "+tc.email+"  ")
			w := signInPostWithDependenciesForTest(t, f, values, "198.51.100.77:8443", deps)
			signInAssertGenericFailureForTest(t, w, generic)
			if len(verifier.calls) != 6 {
				t.Fatal("account-throttled request reached password verification")
			}
		})
	}
}

func TestSignInThrottleRejectsIndependentIPBeforePasswordVerification(t *testing.T) {
	f := newSignInFixtureForTest(t)
	at := time.Date(2026, time.October, 3, 16, 45, 0, 0, time.UTC)
	verifier := &signInVerifierForTest{}
	deps := signInDependencies{now: func() time.Time { return at }, verify: verifier.verify}
	var generic *httptest.ResponseRecorder
	for attempt := 1; attempt <= 6; attempt++ {
		values := signInValuesForTest(t)
		values.Set("email", "unknown-"+strconv.Itoa(attempt)+"@example.test")
		values.Set("password", "private-test-attempt")
		w := signInPostWithDependenciesForTest(t, f, values, "[::ffff:192.0.2.44]:8443", deps)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("admitted attempt %d returned %d, want 401", attempt, w.Code)
		}
		if attempt == 1 {
			generic = w
		}
	}
	if len(verifier.calls) != 6 {
		t.Fatalf("dummy verifier called %d times for six admitted unknown accounts", len(verifier.calls))
	}
	for _, call := range verifier.calls {
		if call.encoded != signInDummyPasswordHash {
			t.Fatal("unknown account did not perform dummy password work")
		}
	}

	values := signInValuesForTest(t)
	values.Set("password", "private-test-attempt")
	w := signInPostWithDependenciesForTest(t, f, values, "192.0.2.44:9000", deps)
	signInAssertGenericFailureForTest(t, w, generic)
	if len(verifier.calls) != 6 {
		t.Fatal("IP-throttled request reached real password verification")
	}
}

func TestSignInDependenciesPreserveBoundariesAndDriveSessionClock(t *testing.T) {
	f := newSignInFixtureForTest(t)
	at := time.Date(2026, time.October, 3, 12, 34, 56, 789000000, time.FixedZone("test", 3*60*60))
	verifier := &signInVerifierForTest{result: true}
	deps := signInDependencies{now: func() time.Time { return at }, verify: verifier.verify}

	badCSRF := signInValuesForTest(t)
	badCSRF.Set("csrf_token", signInNonceForTest())
	if w := signInPostWithDependenciesForTest(t, f, badCSRF, "192.0.2.90:8443", deps); w.Code != http.StatusForbidden {
		t.Fatalf("invalid CSRF returned %d, want 403", w.Code)
	}
	missingEmail := signInValuesForTest(t)
	missingEmail.Del("email")
	if w := signInPostWithDependenciesForTest(t, f, missingEmail, "192.0.2.90:8443", deps); w.Code != http.StatusUnauthorized {
		t.Fatalf("missing email returned %d, want 401", w.Code)
	}
	if len(verifier.calls) != 0 {
		t.Fatal("invalid CSRF or invalid credential form reached password verification")
	}

	w := signInPostWithDependenciesForTest(t, f, signInValuesForTest(t), "192.0.2.90:8443", deps)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("valid sign-in returned %d, want 303", w.Code)
	}
	if len(verifier.calls) != 1 || verifier.calls[0].encoded != f.hash {
		t.Fatal("valid sign-in did not perform one real password verification")
	}
	if len(f.repo.writes) != 1 {
		t.Fatal("valid sign-in did not create one session")
	}
	// CreateSession records its supplied clock in SQLite; the repository spy
	// captures the draft only, so read the issued session through its cookie.
	credential := signInCookieForTest(t, w, sessionCookieNameForTest).Value
	raw := signInRawForTest(t, credential)
	digest := sha256.Sum256(raw)
	session, err := f.repo.Store.GetSessionByTokenDigest(t.Context(), digest[:], at.UTC())
	if err != nil {
		t.Fatal(err)
	}
	if session.CreatedAt != at.UTC().Truncate(time.Millisecond).Format(signInTimeLayout) {
		t.Fatalf("session created_at=%s, want injected clock %s", session.CreatedAt, at.UTC().Truncate(time.Millisecond).Format(signInTimeLayout))
	}
}

func TestSignInHandlerUsesThrottlePath(t *testing.T) {
	f := newSignInFixtureForTest(t)
	values := signInValuesForTest(t)
	values.Set("password", "private-test-wrong-password")
	for attempt := 1; attempt <= 6; attempt++ {
		if w := signInPostForTest(t, f, values); w.Code != http.StatusUnauthorized {
			t.Fatalf("route attempt %d returned %d, want 401", attempt, w.Code)
		}
	}
	if w := signInPostForTest(t, f, signInValuesForTest(t)); w.Code != http.StatusUnauthorized {
		t.Fatalf("Handler route bypassed sign-in throttle: returned %d, want 401", w.Code)
	}
}
