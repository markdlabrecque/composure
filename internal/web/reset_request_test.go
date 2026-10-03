package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log"
	"log/slog"
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

type resetRequestRepositoryForTest struct {
	*store.Store
	now       time.Time
	lookups   []string
	issued    []store.TokenDraft
	issueAt   []time.Time
	tokens    []store.Token
	rawTokens []string
}

func (r *resetRequestRepositoryForTest) Now() time.Time { return r.now }

func (r *resetRequestRepositoryForTest) GetAccountByEmail(ctx context.Context, email string) (store.Account, error) {
	r.lookups = append(r.lookups, email)
	return r.Store.GetAccountByEmail(ctx, email)
}

func (r *resetRequestRepositoryForTest) IssueToken(ctx context.Context, draft store.TokenDraft, at time.Time) (store.Token, string, error) {
	r.issued = append(r.issued, draft)
	r.issueAt = append(r.issueAt, at)
	token, raw, err := r.Store.IssueToken(ctx, draft, at)
	if err == nil {
		r.tokens = append(r.tokens, token)
		r.rawTokens = append(r.rawTokens, raw)
	}
	return token, raw, err
}

type resetRequestFixtureForTest struct {
	repo              *resetRequestRepositoryForTest
	path              string
	activeAccountID   string
	inactiveAccountID string
}

func newResetRequestFixtureForTest(t *testing.T) resetRequestFixtureForTest {
	t.Helper()
	now := time.Date(2026, 10, 3, 9, 8, 7, 654321000, time.FixedZone("test offset", -7*60*60))
	path := filepath.Join(t.TempDir(), "composure.db")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(context.Background(), path, "reset-request-test-site", now, nil); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	activeID, err := s.CreateAccount(context.Background(), store.AccountDraft{Email: "editor@example.test", PasswordHash: "opaque", IsEditor: true, State: "active"}, now)
	if err != nil {
		t.Fatal(err)
	}
	inactiveID, err := s.CreateAccount(context.Background(), store.AccountDraft{Email: "inactive@example.test", PasswordHash: "opaque", IsEditor: true, State: "deactivated"}, now)
	if err != nil {
		t.Fatal(err)
	}
	return resetRequestFixtureForTest{
		repo: &resetRequestRepositoryForTest{Store: s, now: now}, path: path,
		activeAccountID: activeID, inactiveAccountID: inactiveID,
	}
}

func resetRequestServeForTest(f resetRequestFixtureForTest, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	Handler(f.repo, "8443").ServeHTTP(w, r)
	return w
}

func resetRequestFormForTest(t *testing.T, f resetRequestFixtureForTest, cookies ...string) (*httptest.ResponseRecorder, string, string) {
	t.Helper()
	r := signInRequestForTest(http.MethodGet, "/reset", "", cookies...)
	w := resetRequestServeForTest(f, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /reset returned %d, want 200", w.Code)
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), `action="/reset"`) || !strings.Contains(w.Body.String(), `name="email"`) {
		t.Fatal("GET /reset did not render the reset email form")
	}
	nonce := signInCookieForTest(t, w, nonceCookieNameForTest).Value
	token := signInFormTokenForTest(t, w.Body.String())
	if token != signInTokenForTest(t, nonce, "preauth") {
		t.Fatal("reset form did not use pre-authentication nonce CSRF mode")
	}
	return w, nonce, token
}

func resetRequestPostForTest(f resetRequestFixtureForTest, values url.Values, cookies ...string) *httptest.ResponseRecorder {
	r := signInRequestForTest(http.MethodPost, "/reset", values.Encode(), cookies...)
	r.RemoteAddr = "127.0.0.1:43123"
	return resetRequestServeForTest(f, r)
}

func TestResetRequestGETUsesPreAuthNonceDespiteSessionCookie(t *testing.T) {
	f := newResetRequestFixtureForTest(t)
	session := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x6a}, 32))
	_, nonce, token := resetRequestFormForTest(t, f, sessionCookieNameForTest+"="+session)
	if nonce == session || token == signInTokenForTest(t, session, "authenticated") {
		t.Fatal("session cookie changed reset form from pre-authentication nonce mode")
	}
	if len(f.repo.lookups) != 0 || len(f.repo.issued) != 0 {
		t.Fatal("safe reset form read looked up an account or issued a token")
	}
}

func TestResetRequestPOSTKnownUnknownAndDeactivatedArePubliclyIndistinguishable(t *testing.T) {
	type result struct {
		status int
		body   string
	}
	cases := []struct {
		name, email, wantLookup string
		wantIssues              int
		wantAccountID           string
	}{
		{name: "known-active", email: "  EDITOR@Example.TEST  ", wantLookup: "editor@example.test", wantIssues: 1},
		{name: "unknown", email: "  MISSING@Example.TEST  ", wantLookup: "missing@example.test"},
		{name: "known-deactivated", email: " INACTIVE@Example.TEST ", wantLookup: "inactive@example.test"},
	}
	var public []result
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newResetRequestFixtureForTest(t)
			_, nonce, csrf := resetRequestFormForTest(t, f)
			values := url.Values{"email": {tc.email}, "csrf_token": {csrf}}
			w := resetRequestPostForTest(f, values, nonceCookieNameForTest+"="+nonce)
			if w.Code != http.StatusOK {
				t.Fatalf("POST /reset returned %d, want generic 200", w.Code)
			}
			if len(f.repo.lookups) != 1 || f.repo.lookups[0] != tc.wantLookup {
				t.Fatalf("account lookup=%q, want canonical %q", f.repo.lookups, tc.wantLookup)
			}
			if len(f.repo.issued) != tc.wantIssues {
				t.Fatalf("issued %d tokens, want %d", len(f.repo.issued), tc.wantIssues)
			}
			if tc.wantIssues == 1 {
				if f.repo.tokens[0].AccountID == nil || *f.repo.tokens[0].AccountID != f.activeAccountID {
					t.Fatal("reset token was not bound to the active account")
				}
			}
			public = append(public, result{status: w.Code, body: w.Body.String()})
		})
	}
	for i := 1; i < len(public); i++ {
		if public[i] != public[0] {
			t.Fatal("known, unknown, and deactivated addresses returned distinguishable public responses")
		}
	}
}

func TestResetRequestIssuesPurposeBoundDigestOnlyTokenFromInjectedUTCClock(t *testing.T) {
	f := newResetRequestFixtureForTest(t)
	var logs bytes.Buffer
	oldLogWriter := log.Writer()
	oldSlog := slog.Default()
	log.SetOutput(&logs)
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { log.SetOutput(oldLogWriter); slog.SetDefault(oldSlog) })

	_, nonce, csrf := resetRequestFormForTest(t, f)
	w := resetRequestPostForTest(f, url.Values{"email": {"editor@example.test"}, "csrf_token": {csrf}}, nonceCookieNameForTest+"="+nonce)
	if w.Code != http.StatusOK || len(f.repo.tokens) != 1 || len(f.repo.rawTokens) != 1 {
		t.Fatalf("reset request status=%d issued=%d, want 200 and one token", w.Code, len(f.repo.tokens))
	}
	draft, issued, at := f.repo.issued[0], f.repo.tokens[0], f.repo.issueAt[0]
	if draft.Purpose != "password_reset" || issued.Purpose != "password_reset" || draft.Email != nil || issued.Email != nil {
		t.Fatal("reset request issued the wrong token purpose or email binding")
	}
	if draft.AccountID == nil || *draft.AccountID != f.activeAccountID {
		t.Fatal("reset token draft was not account-bound")
	}
	wantAt := f.repo.now.UTC()
	if !at.Equal(wantAt) || at.Location() != time.UTC {
		t.Fatalf("issuance clock=%v location=%v, want injected UTC %v", at, at.Location(), wantAt)
	}
	if !draft.ExpiresAt.Equal(wantAt.Add(time.Hour)) || draft.ExpiresAt.Location() != time.UTC {
		t.Fatalf("expiry=%v, want exactly one hour after injected UTC clock", draft.ExpiresAt)
	}
	if issued.CreatedAt != "2026-10-03T16:08:07.654Z" || issued.ExpiresAt != "2026-10-03T17:08:07.654Z" || issued.UsedAt != nil {
		t.Fatalf("persisted timestamps differ: created=%q expires=%q used=%v", issued.CreatedAt, issued.ExpiresAt, issued.UsedAt)
	}
	raw := f.repo.rawTokens[0]
	rawBytes, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || len(rawBytes) != 32 {
		t.Fatal("issued credential was not a canonical 32-byte token")
	}
	digest := sha256.Sum256(rawBytes)
	if !bytes.Equal(issued.TokenDigest, digest[:]) {
		t.Fatal("stored token metadata did not contain the raw-token digest")
	}
	if strings.Contains(w.Body.String(), raw) || strings.Contains(logs.String(), raw) {
		t.Fatal("raw reset token appeared in the HTTP response or logs")
	}
	for _, suffix := range []string{"", "-wal"} {
		data, err := os.ReadFile(f.path + suffix)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(raw)) || bytes.Contains(data, rawBytes) {
			t.Fatal("raw reset token appeared in SQLite storage")
		}
	}
	got, err := f.repo.GetTokenByTokenDigest(context.Background(), "password_reset", digest[:], wantAt)
	if err != nil || got.ID != issued.ID {
		t.Fatalf("persisted reset token lookup failed: %v", err)
	}
	if _, err := f.repo.GetTokenByTokenDigest(context.Background(), "invitation", digest[:], wantAt); !errors.Is(err, content.ErrNotFound) {
		t.Fatal("reset credential was usable for invitation purpose")
	}
}

func TestResetRequestRejectsInvalidCSRFAndUnsafeBodiesBeforeLookup(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
		wantStatus  int
	}{
		{name: "wrong-media-type", contentType: "application/json", body: `{}`, wantStatus: http.StatusUnsupportedMediaType},
		{name: "oversized", contentType: "application/x-www-form-urlencoded", body: strings.Repeat("x", (1<<20)+1), wantStatus: http.StatusRequestEntityTooLarge},
		{name: "missing-csrf", contentType: "application/x-www-form-urlencoded", body: "email=editor%40example.test", wantStatus: http.StatusForbidden},
		{name: "duplicate-email", contentType: "application/x-www-form-urlencoded", body: "email=one%40example.test&email=two%40example.test&csrf_token=" + signInTokenForTest(t, signInNonceForTest(), "preauth"), wantStatus: http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newResetRequestFixtureForTest(t)
			r := httptest.NewRequest(http.MethodPost, "https://127.0.0.1:8443/reset", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			r.Header.Set("Cookie", nonceCookieNameForTest+"="+signInNonceForTest())
			r.RemoteAddr = "127.0.0.1:43123"
			w := resetRequestServeForTest(f, r)
			if w.Code != tc.wantStatus {
				t.Fatalf("status=%d, want %d", w.Code, tc.wantStatus)
			}
			if len(f.repo.lookups) != 0 || len(f.repo.issued) != 0 {
				t.Fatal("rejected request reached account lookup or token issuance")
			}
		})
	}
}
