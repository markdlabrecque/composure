package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/store"
)

type invitationRepositoryForTest struct {
	*store.Store
	issueCalls []invitationIssueForTest
}

type invitationIssueForTest struct {
	draft store.TokenDraft
	at    time.Time
	token store.Token
	raw   string
}

func (r *invitationRepositoryForTest) IssueToken(ctx context.Context, draft store.TokenDraft, at time.Time) (store.Token, string, error) {
	token, raw, err := r.Store.IssueToken(ctx, draft, at)
	if err == nil {
		r.issueCalls = append(r.issueCalls, invitationIssueForTest{draft: draft, at: at, token: token, raw: raw})
	}
	return token, raw, err
}

type invitationFixtureForTest struct {
	repository *invitationRepositoryForTest
	accountID  string
	credential string
	path       string
	now        time.Time
}

func newInvitationFixtureForTest(t *testing.T, administrator, editor bool) invitationFixtureForTest {
	t.Helper()
	guard := newGuardFixture(t, false)
	if err := guard.repository.SetAccountRoles(context.Background(), guard.accountID, administrator, editor, guard.now); err != nil {
		t.Fatal(err)
	}
	rawCredential := bytes.Repeat([]byte{0x91}, 32)
	credential := base64.RawURLEncoding.EncodeToString(rawCredential)
	digest := sha256.Sum256(rawCredential)
	if _, err := guard.repository.CreateSession(context.Background(), store.SessionDraft{AccountID: guard.accountID, TokenDigest: digest[:]}, guard.now); err != nil {
		t.Fatal(err)
	}
	return invitationFixtureForTest{
		repository: &invitationRepositoryForTest{Store: guard.repository},
		accountID:  guard.accountID,
		credential: credential,
		path:       guard.path,
		now:        guard.now,
	}
}

func invitationServeForTest(f invitationFixtureForTest, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	HandlerWithClock(f.repository, "8443", func() time.Time { return f.now }).ServeHTTP(w, r)
	return w
}

func invitationRequestForTest(method, target string, values url.Values, credential string) *http.Request {
	body := ""
	if values != nil {
		body = values.Encode()
	}
	r := guardRequest(method, target, body, nil)
	if method == http.MethodPost {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if credential != "" {
		r.Header.Set("Cookie", guardCookie(credential))
	}
	return r
}

func invitationValuesForTest(t *testing.T, credential, email string) url.Values {
	t.Helper()
	return url.Values{"email": {email}, "csrf_token": {guardCSRFToken(t, credential)}}
}

func TestInvitationAdministratorFormAndIssue(t *testing.T) {
	f := newInvitationFixtureForTest(t, true, false)
	response := invitationServeForTest(f, invitationRequestForTest(http.MethodGet, "/admin/invitations/new", nil, f.credential))
	if response.Code != http.StatusOK {
		t.Fatalf("GET invitation form returned %d, want 200", response.Code)
	}
	body := response.Body.String()
	if !strings.Contains(body, `action="/admin/invitations"`) || !strings.Contains(body, `method="post"`) || !strings.Contains(body, `name="email"`) {
		t.Fatal("invitation form must post an email to /admin/invitations")
	}
	wantCSRF := guardCSRFToken(t, f.credential)
	if strings.Count(body, `name="csrf_token"`) != 1 || !strings.Contains(body, `type="hidden"`) || !strings.Contains(body, `value="`+wantCSRF+`"`) {
		t.Fatal("invitation form must carry exactly one session-derived hidden CSRF token")
	}
	if len(f.repository.issueCalls) != 0 {
		t.Fatal("safe form GET issued an invitation token")
	}

	values := invitationValuesForTest(t, f.credential, "  Invited.User+Tag@EXAMPLE.TEST  ")
	response = invitationServeForTest(f, invitationRequestForTest(http.MethodPost, "/admin/invitations", values, f.credential))
	if response.Code != http.StatusSeeOther {
		t.Fatalf("POST invitation returned %d, want 303", response.Code)
	}
	if len(f.repository.issueCalls) != 1 {
		t.Fatalf("valid submission issued %d tokens, want one", len(f.repository.issueCalls))
	}
	issued := f.repository.issueCalls[0]
	if issued.at != f.now {
		t.Fatalf("issuance time = %s, want injected clock %s", issued.at, f.now)
	}
	if issued.draft.Purpose != "invitation" || issued.draft.AccountID != nil || issued.draft.Email == nil || *issued.draft.Email != "invited.user+tag@example.test" {
		t.Fatalf("issued invitation binding = %+v, want canonical email and no account", issued.draft)
	}
	if want := f.now.Add(7 * 24 * time.Hour); !issued.draft.ExpiresAt.Equal(want) {
		t.Fatalf("invitation expiry = %s, want %s", issued.draft.ExpiresAt, want)
	}
	if issued.token.Email == nil || *issued.token.Email != "invited.user+tag@example.test" || issued.token.Purpose != "invitation" || issued.token.UsedAt != nil {
		t.Fatalf("stored invitation metadata = %+v", issued.token)
	}
	rawBytes, err := base64.RawURLEncoding.Strict().DecodeString(issued.raw)
	if err != nil || len(rawBytes) != 32 {
		t.Fatal("issued credential must be the existing 32-byte token format")
	}
	digest := sha256.Sum256(rawBytes)
	if !bytes.Equal(issued.token.TokenDigest, digest[:]) {
		t.Fatal("stored metadata must contain the credential digest")
	}
	persisted, err := f.repository.GetTokenByTokenDigest(context.Background(), "invitation", digest[:], f.now)
	if err != nil {
		t.Fatalf("read issued invitation from store: %v", err)
	}
	wantCreatedAt := f.now.UTC().Format("2006-01-02T15:04:05.000Z")
	wantExpiresAt := f.now.Add(7 * 24 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")
	if persisted.Purpose != "invitation" || persisted.Email == nil || *persisted.Email != "invited.user+tag@example.test" ||
		persisted.AccountID != nil || persisted.UsedAt != nil || persisted.CreatedAt != wantCreatedAt || persisted.ExpiresAt != wantExpiresAt ||
		!bytes.Equal(persisted.TokenDigest, digest[:]) {
		t.Fatalf("persisted invitation metadata = %+v, want canonical unused invitation created %s and expiring %s", persisted, wantCreatedAt, wantExpiresAt)
	}
	if bytes.Contains(invitationDatabaseBytesForTest(t, f.path), []byte(issued.raw)) {
		t.Fatal("database persisted the raw invitation credential")
	}
}

func TestInvitationRejectsInvalidEmailWithoutIssuing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values func(t *testing.T, f invitationFixtureForTest) url.Values
	}{
		{name: "missing", values: func(t *testing.T, f invitationFixtureForTest) url.Values {
			return url.Values{"csrf_token": {guardCSRFToken(t, f.credential)}}
		}},
		{name: "canonical empty", values: func(t *testing.T, f invitationFixtureForTest) url.Values {
			return invitationValuesForTest(t, f.credential, "   ")
		}},
		{name: "duplicate", values: func(t *testing.T, f invitationFixtureForTest) url.Values {
			values := invitationValuesForTest(t, f.credential, "one@example.test")
			values.Add("email", "two@example.test")
			return values
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newInvitationFixtureForTest(t, true, false)
			response := invitationServeForTest(f, invitationRequestForTest(http.MethodPost, "/admin/invitations", tc.values(t, f), f.credential))
			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("invalid email returned %d, want 422", response.Code)
			}
			if len(f.repository.issueCalls) != 0 {
				t.Fatal("invalid email issued an invitation token")
			}
		})
	}
}

func TestInvitationRoutesRequireActiveAdministrator(t *testing.T) {
	tests := []struct {
		name                  string
		administrator, editor bool
		deactivate            bool
		credential            bool
		want                  int
	}{
		{name: "administrator", administrator: true, credential: true, want: http.StatusOK},
		{name: "editor", editor: true, credential: true, want: http.StatusForbidden},
		{name: "deactivated administrator", administrator: true, deactivate: true, credential: true, want: http.StatusSeeOther},
		{name: "unauthenticated", administrator: true, want: http.StatusSeeOther},
	}
	for _, tc := range tests {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			t.Run(tc.name+"_"+method, func(t *testing.T) {
				f := newInvitationFixtureForTest(t, tc.administrator, tc.editor)
				if tc.deactivate {
					other, err := f.repository.CreateAccount(context.Background(), store.AccountDraft{Email: "remaining@example.test", PasswordHash: "test", IsAdministrator: true, State: "active"}, f.now)
					if err != nil || other == "" {
						t.Fatal(err)
					}
					if err := f.repository.DeactivateAccount(context.Background(), f.accountID, f.now); err != nil {
						t.Fatal(err)
					}
				}
				credential := ""
				if tc.credential {
					credential = f.credential
				}
				target := "/admin/invitations/new"
				var values url.Values
				want := tc.want
				if method == http.MethodPost {
					target = "/admin/invitations"
					values = invitationValuesForTest(t, f.credential, "denied@example.test")
					if tc.name == "administrator" {
						want = http.StatusSeeOther
					}
				}
				response := invitationServeForTest(f, invitationRequestForTest(method, target, values, credential))
				if response.Code != want {
					t.Fatalf("%s returned %d, want %d", target, response.Code, want)
				}
				wantIssues := 0
				if tc.name == "administrator" && method == http.MethodPost {
					wantIssues = 1
				}
				if len(f.repository.issueCalls) != wantIssues {
					t.Fatalf("request issued %d tokens, want %d", len(f.repository.issueCalls), wantIssues)
				}
			})
		}
	}
}

func TestInvitationPostKeepsAuthenticatedCSRFAndCrossOriginBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*http.Request)
	}{
		{name: "missing csrf", prepare: func(r *http.Request) {
			values, _ := url.ParseQuery(readInvitationBodyForTest(t, r))
			values.Del("csrf_token")
			r.Body = io.NopCloser(strings.NewReader(values.Encode()))
			r.ContentLength = int64(len(values.Encode()))
		}},
		{name: "cross origin", prepare: func(r *http.Request) {
			r.Header.Set("Sec-Fetch-Site", "cross-site")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newInvitationFixtureForTest(t, true, false)
			r := invitationRequestForTest(http.MethodPost, "/admin/invitations", invitationValuesForTest(t, f.credential, "blocked@example.test"), f.credential)
			tc.prepare(r)
			response := invitationServeForTest(f, r)
			if response.Code != http.StatusForbidden {
				t.Fatalf("rejected POST returned %d, want 403", response.Code)
			}
			if len(f.repository.issueCalls) != 0 {
				t.Fatal("rejected POST issued an invitation token")
			}
		})
	}
}

func invitationDatabaseBytesForTest(t *testing.T, path string) []byte {
	t.Helper()
	// SQLite storage is inspected through the already-open site's file path in
	// the guard fixture, without adding a token lookup or exposing SQL to web.
	// The raw credential is base64url text, so a direct byte search detects the
	// forbidden persisted representation while the digest remains opaque.
	paths, err := filepath.Glob(path + "*")
	if err != nil {
		t.Fatal(err)
	}
	var contents []byte
	for _, candidate := range paths {
		part, err := os.ReadFile(candidate)
		if err != nil {
			t.Fatal(err)
		}
		contents = append(contents, part...)
	}
	return contents
}

func readInvitationBodyForTest(t *testing.T, r *http.Request) string {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
