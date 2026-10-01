package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSessionMiddlewareRegressionNestedRecheck(t *testing.T) {
	cases := []struct {
		name              string
		change            func(*sessionLoaderForTest, time.Time)
		cookie            func(string) string
		wantAuthenticated bool
		wantSessionLoads  int
		wantAccountLoads  int
		wantDeletion      bool
	}{
		{
			name: "valid-second-check", wantAuthenticated: true,
			wantSessionLoads: 2, wantAccountLoads: 2,
		},
		{
			name: "second-session-read-fails",
			change: func(l *sessionLoaderForTest, _ time.Time) {
				l.sessionErr = errors.New("session backend unavailable on recheck")
			},
			wantSessionLoads: 2, wantAccountLoads: 1, wantDeletion: true,
		},
		{
			name: "second-account-read-fails",
			change: func(l *sessionLoaderForTest, _ time.Time) {
				l.accountErr = errors.New("account backend unavailable on recheck")
			},
			wantSessionLoads: 2, wantAccountLoads: 2, wantDeletion: true,
		},
		{
			name: "revoked-before-second-check",
			change: func(l *sessionLoaderForTest, at time.Time) {
				revoked := at.Format("2006-01-02T15:04:05.000Z")
				l.session.RevokedAt = &revoked
			},
			wantSessionLoads: 2, wantAccountLoads: 2, wantDeletion: true,
		},
		{
			name: "expired-before-second-check",
			change: func(l *sessionLoaderForTest, at time.Time) {
				l.session.ExpiresAt = at.Format("2006-01-02T15:04:05.000Z")
			},
			wantSessionLoads: 2, wantAccountLoads: 2, wantDeletion: true,
		},
		{
			name: "deactivated-before-second-check",
			change: func(l *sessionLoaderForTest, _ time.Time) {
				l.account.State = "deactivated"
			},
			wantSessionLoads: 2, wantAccountLoads: 2, wantDeletion: true,
		},
		{
			name: "missing-second-credential", cookie: func(string) string { return "" },
			wantSessionLoads: 1, wantAccountLoads: 1,
		},
		{
			name:             "malformed-second-credential",
			cookie:           func(string) string { return sessionCookieNameForTest + "=bad" },
			wantSessionLoads: 1, wantAccountLoads: 1,
		},
		{
			name:             "duplicate-second-credential",
			cookie:           func(valid string) string { return valid + "; " + valid },
			wantSessionLoads: 1, wantAccountLoads: 1,
		},
		{
			name: "duplicate-second-nonce",
			cookie: func(valid string) string {
				return valid + "; " + nonceCookieNameForTest + "=x; " + nonceCookieNameForTest + "=x"
			},
			wantSessionLoads: 1, wantAccountLoads: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			credential, _, at, loader := sessionFixtureForTest()
			validCookie := sessionCookieNameForTest + "=" + credential
			r := httptest.NewRequest(http.MethodPost, "https://example.test/session-consumer?keep=value", strings.NewReader("unchanged-body"))
			r.Header.Set("Cookie", validCookie)
			r.Header.Set("X-Request-Marker", "keep-header")
			r = r.WithContext(context.WithValue(r.Context(), sessionContextKeyForTest{}, "keep-context"))
			originalHeaders := r.Header.Clone()
			wantHeaders := originalHeaders
			consumerCalls := 0
			consumer := http.HandlerFunc(func(_ http.ResponseWriter, got *http.Request) {
				consumerCalls++
				_, _, authenticated := SessionFromContext(got.Context())
				if authenticated != tc.wantAuthenticated {
					t.Errorf("second check authenticated=%v, want %v; refused recheck must not retain first-check authentication", authenticated, tc.wantAuthenticated)
				}
				if got.Context().Value(sessionContextKeyForTest{}) != "keep-context" {
					t.Error("second check discarded unrelated request context")
				}
				body, err := io.ReadAll(got.Body)
				if err != nil || string(body) != "unchanged-body" || got.Form != nil || got.PostForm != nil || got.Method != r.Method || got.URL.String() != r.URL.String() || !reflect.DeepEqual(got.Header, wantHeaders) {
					t.Error("nested checks consumed or parsed the body, or changed request inputs")
				}
			})
			recheck := SessionMiddleware(loader, func() time.Time { return at }, consumer)
			outer := SessionMiddleware(loader, func() time.Time { return at }, http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
				// Authentication comes only from the public middleware API. No
				// private authentication key or fabricated context state is used.
				if _, _, authenticated := SessionFromContext(got.Context()); !authenticated {
					t.Fatal("first check must authenticate before exercising a refusal")
				}
				if tc.change != nil {
					tc.change(loader, at)
				}
				// Persisted-state cases pass the identical Cookie header to both
				// nested guards. Early-refusal cases recheck a child request with
				// changed credentials, without mutating the caller's headers.
				if tc.cookie != nil {
					got = got.Clone(got.Context())
					got.Header.Del("Cookie")
					if line := tc.cookie(validCookie); line != "" {
						got.Header.Set("Cookie", line)
					}
					wantHeaders = got.Header.Clone()
				}
				recheck.ServeHTTP(w, got)
			}))
			w := httptest.NewRecorder()
			outer.ServeHTTP(w, r)
			if consumerCalls != 1 {
				t.Errorf("consumer called %d times, want one delegation after recheck", consumerCalls)
			}
			if len(loader.digests) != tc.wantSessionLoads || len(loader.accountIDs) != tc.wantAccountLoads {
				t.Errorf("session/account loads=%d/%d, want %d/%d", len(loader.digests), len(loader.accountIDs), tc.wantSessionLoads, tc.wantAccountLoads)
			}
			if tc.wantDeletion {
				assertSessionCookieDeletedForTest(t, w, at)
			}
			if _, _, authenticated := SessionFromContext(r.Context()); authenticated {
				t.Error("nested checks mutated the caller's authentication context")
			}
			if !reflect.DeepEqual(r.Header, originalHeaders) {
				t.Error("nested checks mutated the caller's headers")
			}
		})
	}
}

func TestSessionMiddlewareRegressionOversizedCredentialAllocation(t *testing.T) {
	_, _, at, loader := sessionFixtureForTest()
	// Construct the valid-alphabet 512 KiB header and all request fixtures before
	// measuring. Rejection must not allocate a decoded copy of this credential.
	r := sessionRequestForTest(sessionCookieNameForTest + "=" + strings.Repeat("A", 512<<10))
	w := httptest.NewRecorder()
	consumerCalls, authenticatedCalls := 0, 0
	h := SessionMiddleware(loader, func() time.Time { return at }, http.HandlerFunc(func(_ http.ResponseWriter, got *http.Request) {
		consumerCalls++
		if _, _, authenticated := SessionFromContext(got.Context()); authenticated {
			authenticatedCalls++
		}
	}))
	h.ServeHTTP(w, r) // Warm up before measuring; no fixture allocation in the loop.
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	// Fixed iterations bound this check to about 25 MiB even on the broken
	// candidate. Do not run in parallel: TotalAlloc is process-wide.
	const requests = 64
	for i := 0; i < requests; i++ {
		h.ServeHTTP(w, r)
	}
	runtime.ReadMemStats(&after)
	bytesPerRequest := (after.TotalAlloc - before.TotalAlloc) / requests
	t.Logf("oversized credential rejection allocated %d bytes/request across %d measured requests", bytesPerRequest, requests)
	if bytesPerRequest > 64<<10 {
		t.Errorf("oversized credential rejection allocated %d bytes/request, want at most 64 KiB; enforce fixed credential length before decoding", bytesPerRequest)
	}
	if len(loader.digests) != 0 || len(loader.accountIDs) != 0 {
		t.Error("oversized credential reached persistent lookup")
	}
	if authenticatedCalls != 0 {
		t.Error("oversized credential authenticated a consumer")
	}
	if consumerCalls != requests+1 {
		t.Errorf("consumer called %d times, want %d anonymous delegations including warmup", consumerCalls, requests+1)
	}
}
