package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
)

// Proposed bounded API, following the account/session timestamp conventions:
// TokenDraft { Purpose string; AccountID, Email *string; ExpiresAt time.Time }
// Token { ID, Purpose string; TokenDigest []byte; AccountID, Email *string;
//         CreatedAt, ExpiresAt string; UsedAt *string }
// IssueToken(ctx, draft, at) (Token, string, error) returns metadata and a raw
// credential separately. GetTokenByTokenDigest(ctx, purpose, digest, now)
// returns only usable metadata, or content.ErrNotFound.
// consumeToken(ctx, purpose, digest, now, func(*sql.Tx, Token) error) error is
// PRIVATE to store. It commits the authorized operation and consumption in one
// transaction; a nil callback is an error, never a claim-only operation.
// issueTokenWithRandom(ctx, draft, at, io.Reader) uses injected token entropy
// for failure tests. IssueToken must use crypto/rand.Reader in production.
// No public SQL transaction, token lifetime default, or later workflow policy
// is introduced by these tests. Durations below are caller-selected fixtures.

func tokenAt() time.Time {
	return time.Date(2026, 10, 1, 12, 34, 56, 789000000, time.UTC)
}

func tokenString(value string) *string { return &value }

func tokenInvite(email string, expires time.Time) TokenDraft {
	return TokenDraft{Purpose: "invitation", Email: tokenString(email), ExpiresAt: expires}
}

func tokenReset(accountID string, expires time.Time) TokenDraft {
	return TokenDraft{Purpose: "password_reset", AccountID: tokenString(accountID), ExpiresAt: expires}
}

func tokenIssue(t *testing.T, s *Store, draft TokenDraft, at time.Time) (Token, string) {
	t.Helper()
	got, raw, err := s.IssueToken(context.Background(), draft, at)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`).MatchString(raw) {
		t.Fatal("credential must be canonical unpadded base64url encoding of 32 random bytes")
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || len(decoded) != 32 {
		t.Fatal("credential does not decode to 32 bytes")
	}
	digest := sha256.Sum256(decoded)
	if !bytes.Equal(got.TokenDigest, digest[:]) {
		t.Fatal("token digest must be SHA-256 of raw random bytes, not the encoded link")
	}
	return got, raw
}

func tokenStored(t *testing.T, s *Store, id string) Token {
	t.Helper()
	var got Token
	var account, email, used sql.NullString
	err := s.db.QueryRow(`SELECT id,purpose,token_digest,account_id,email,created_at,expires_at,used_at FROM tokens WHERE id=?`, id).
		Scan(&got.ID, &got.Purpose, &got.TokenDigest, &account, &email, &got.CreatedAt, &got.ExpiresAt, &used)
	if err != nil {
		t.Fatal(err)
	}
	if account.Valid {
		got.AccountID = tokenString(account.String)
	}
	if email.Valid {
		got.Email = tokenString(email.String)
	}
	if used.Valid {
		got.UsedAt = tokenString(used.String)
	}
	return got
}

func tokenDenied(t *testing.T, s *Store, purpose string, digest []byte, now time.Time) {
	t.Helper()
	got, err := s.GetTokenByTokenDigest(context.Background(), purpose, digest, now)
	if !errors.Is(err, content.ErrNotFound) || !reflect.DeepEqual(got, Token{}) {
		t.Fatal("missing or unusable token must return ErrNotFound and no metadata")
	}
	called := false
	err = s.consumeToken(context.Background(), purpose, digest, now, func(*sql.Tx, Token) error {
		called = true
		return nil
	})
	if !errors.Is(err, content.ErrNotFound) || called {
		t.Fatal("missing or unusable token must deny before the authorized callback")
	}
}

func TestTokenIssueRoundTripAndRestart(t *testing.T) {
	s, path := accountSite(t)
	at := time.Date(2026, 10, 1, 5, 34, 56, 789123456, time.FixedZone("offset", -7*3600))
	accountID := sessionAccount(t, s, "reset@example.test", at)
	drafts := []TokenDraft{tokenInvite("  A.B+Tag@EXAMPLE.TEST  ", at.Add(17*time.Minute)), tokenReset(accountID, at.Add(31*time.Minute))}
	var issued []Token
	for _, draft := range drafts {
		got, _ := tokenIssue(t, s, draft, at)
		if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(got.ID) {
			t.Fatal("token ID must be server-assigned UUIDv7")
		}
		ms, err := strconv.ParseInt(strings.ReplaceAll(got.ID[:13], "-", ""), 16, 64)
		if err != nil || ms != at.UnixMilli() {
			t.Fatal("token UUID must use supplied issuance clock")
		}
		if got.Purpose != draft.Purpose || got.CreatedAt != "2026-10-01T12:34:56.789Z" || got.ExpiresAt != draft.ExpiresAt.UTC().Format("2006-01-02T15:04:05.000Z") || got.UsedAt != nil {
			t.Fatal("issuance metadata must use explicit expiry, UTC milliseconds, and NULL used_at")
		}
		if draft.Purpose == "invitation" {
			if got.AccountID != nil || got.Email == nil || *got.Email != "a.b+tag@example.test" {
				t.Fatal("invitation must bind canonical email and NULL account_id")
			}
		} else if got.Email != nil || got.AccountID == nil || *got.AccountID != accountID {
			t.Fatal("reset must bind existing account ID and NULL email")
		}
		issued = append(issued, got)
	}
	verify := func(s *Store) {
		t.Helper()
		for _, want := range issued {
			got, err := s.GetTokenByTokenDigest(context.Background(), want.Purpose, want.TokenDigest, at.Add(time.Minute))
			if err != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(tokenStored(t, s, want.ID), want) {
				t.Fatal("token must round-trip exactly through API and persisted nullable columns")
			}
		}
	}
	verify(s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	verify(reopened)
}

func TestTokenInvitationEmailNormalizationAndOwnership(t *testing.T) {
	s, _ := accountSite(t)
	at := tokenAt()
	cases := []struct{ input, want string }{
		{" Mixed@Example.TEST ", "mixed@example.test"},
		{"ÄÉ@EXAMPLE.TEST", "ÄÉ@example.test"},
		{"äé@example.test", "äé@example.test"},
		{"\u00a0User@EXAMPLE.TEST\u00a0", "\u00a0user@example.test\u00a0"},
		{"\tUser@EXAMPLE.TEST\t", "\tuser@example.test\t"},
		{"A.B+Tag@EXAMPLE.TEST", "a.b+tag@example.test"},
		{"AB+Tag@EXAMPLE.TEST", "ab+tag@example.test"},
		{"E\u0301@EXAMPLE.TEST", "e\u0301@example.test"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			draft := tokenInvite(tc.input, at.Add(time.Hour))
			got, _ := tokenIssue(t, s, draft, at)
			if got.Email == nil || *got.Email != tc.want || *draft.Email != tc.input {
				t.Fatal("canonicalize ASCII only without mutating caller data or applying aliases")
			}
			*draft.Email = "changed@example.test"
			if *got.Email != tc.want {
				t.Fatal("returned metadata aliases caller email pointer")
			}
			originalDigest := append([]byte(nil), got.TokenDigest...)
			*got.Email = "changed again"
			got.TokenDigest[0] ^= 0xff
			loaded, err := s.GetTokenByTokenDigest(context.Background(), "invitation", originalDigest, at)
			if err != nil || loaded.Email == nil || *loaded.Email != tc.want || !bytes.Equal(loaded.TokenDigest, originalDigest) {
				t.Fatal("mutating returned metadata changed persisted token")
			}
		})
	}
}

func TestTokenResetAccountBindingSurvivesEmailChangeAndDeactivation(t *testing.T) {
	s, _ := accountSite(t)
	at := tokenAt()
	id := sessionAccount(t, s, "old@example.test", at)
	draft := tokenReset(id, at.Add(time.Hour))
	got, _ := tokenIssue(t, s, draft, at)
	*draft.AccountID = "caller mutation"
	if got.AccountID == nil || *got.AccountID != id {
		t.Fatal("returned account binding aliases caller pointer")
	}
	if _, err := s.db.Exec("UPDATE accounts SET email='new@example.test',state='deactivated' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	stored := tokenStored(t, s, got.ID)
	if stored.AccountID == nil || *stored.AccountID != id || stored.Email != nil || stored.UsedAt != nil {
		t.Fatal("email change/deactivation erased or redirected token account binding")
	}
	if account, err := s.GetAccount(context.Background(), id); err != nil || account.State != "deactivated" {
		t.Fatal("deactivation must preserve the account identity")
	}
}

func TestTokenIssueRejectsInvalidDraftsWithoutRows(t *testing.T) {
	s, _ := accountSite(t)
	at := tokenAt()
	id := sessionAccount(t, s, "valid@example.test", at)
	expires := at.Add(time.Hour)
	cases := []struct {
		name  string
		draft TokenDraft
	}{
		{"unknown purpose", TokenDraft{Purpose: "session", Email: tokenString("a@example.test"), ExpiresAt: expires}},
		{"empty purpose", TokenDraft{Email: tokenString("a@example.test"), ExpiresAt: expires}},
		{"invitation no email", TokenDraft{Purpose: "invitation", ExpiresAt: expires}},
		{"invitation account", TokenDraft{Purpose: "invitation", AccountID: tokenString(id), Email: tokenString("a@example.test"), ExpiresAt: expires}},
		{"reset no account", TokenDraft{Purpose: "password_reset", ExpiresAt: expires}},
		{"reset email", TokenDraft{Purpose: "password_reset", AccountID: tokenString(id), Email: tokenString("a@example.test"), ExpiresAt: expires}},
		{"reset dead foreign key", tokenReset("missing", expires)},
		{"unspecified expiry", tokenInvite("a@example.test", time.Time{})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, raw, err := s.IssueToken(context.Background(), tc.draft, at)
			if err == nil || raw != "" || !reflect.DeepEqual(got, Token{}) {
				t.Fatal("invalid issuance must fail without credential or metadata")
			}
		})
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM tokens").Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid issuance left rows: count=%d, err=%v", count, err)
	}
}

func TestTokenEntropyProfileFailureAndDuplicate(t *testing.T) {
	s, _ := accountSite(t)
	at := tokenAt()
	draft := tokenInvite("entropy@example.test", at.Add(time.Hour))
	// Injection affects only the token random value, not UUID randomness.
	fixture := bytes.Repeat([]byte{0xa5}, 32)
	got, raw, err := s.issueTokenWithRandom(context.Background(), draft, at, bytes.NewReader(fixture))
	digest := sha256.Sum256(fixture)
	if err != nil || raw != base64.RawURLEncoding.EncodeToString(fixture) || !bytes.Equal(got.TokenDigest, digest[:]) {
		t.Fatal("issuance must read all 32 random bytes and digest the decoded credential")
	}
	for _, length := range []int{0, 1, 31} {
		t.Run(fmt.Sprintf("short entropy %d", length), func(t *testing.T) {
			failed, credential, err := s.issueTokenWithRandom(context.Background(), draft, at, bytes.NewReader(fixture[:length]))
			if err == nil || credential != "" || !reflect.DeepEqual(failed, Token{}) {
				t.Fatal("entropy failure must abort issuance without exposing partial credential")
			}
		})
	}
	failed, credential, err := s.issueTokenWithRandom(context.Background(), draft, at, tokenErrorReader{})
	if !errors.Is(err, io.ErrClosedPipe) || credential != "" || !reflect.DeepEqual(failed, Token{}) {
		t.Fatal("entropy source error must propagate without issuance")
	}
	// Digest uniqueness is global, including different purposes.
	id := sessionAccount(t, s, "duplicate@example.test", at)
	failed, credential, err = s.issueTokenWithRandom(context.Background(), tokenReset(id, at.Add(time.Hour)), at, bytes.NewReader(fixture))
	if err == nil || credential != "" || !reflect.DeepEqual(failed, Token{}) {
		t.Fatal("duplicate random value must not overwrite a token or issue another credential")
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM tokens").Scan(&count); err != nil || count != 1 || !reflect.DeepEqual(tokenStored(t, s, got.ID), got) {
		t.Fatal("entropy or duplicate failure changed persisted tokens")
	}
	seen := map[string]bool{raw: true}
	for i := 0; i < 16; i++ {
		_, value := tokenIssue(t, s, draft, at)
		if seen[value] {
			t.Fatal("default issuance reused a random credential")
		}
		seen[value] = true
	}
}

type tokenErrorReader struct{}

func (tokenErrorReader) Read([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestTokenPurposeAndAbsoluteExpiryDenyBeforeOperation(t *testing.T) {
	s, _ := accountSite(t)
	at := tokenAt()
	id := sessionAccount(t, s, "expiry@example.test", at)
	for _, draft := range []TokenDraft{tokenInvite("invite@example.test", at.Add(11*time.Minute)), tokenReset(id, at.Add(23*time.Minute))} {
		t.Run(draft.Purpose, func(t *testing.T) {
			got, _ := tokenIssue(t, s, draft, at)
			for _, now := range []time.Time{at, draft.ExpiresAt.Add(-time.Nanosecond)} {
				loaded, err := s.GetTokenByTokenDigest(context.Background(), draft.Purpose, got.TokenDigest, now)
				if err != nil || !reflect.DeepEqual(loaded, got) {
					t.Fatal("unused token must remain eligible strictly before fixed expiry")
				}
			}
			other := "invitation"
			if draft.Purpose == other {
				other = "password_reset"
			}
			tokenDenied(t, s, other, got.TokenDigest, at)
			tokenDenied(t, s, "unknown", got.TokenDigest, at)
			tokenDenied(t, s, draft.Purpose, sessionDigest("unknown token"), at)
			tokenDenied(t, s, draft.Purpose, nil, at)
			for _, now := range []time.Time{draft.ExpiresAt, draft.ExpiresAt.Add(time.Nanosecond), draft.ExpiresAt.Add(time.Hour)} {
				tokenDenied(t, s, draft.Purpose, got.TokenDigest, now)
			}
			if !reflect.DeepEqual(tokenStored(t, s, got.ID), got) {
				t.Fatal("reads and rejected consumption must not extend expiry or set used_at")
			}
			// Consumption, not only lookup, must allow the final instant before
			// expiry. Each callback performs an actual account operation.
			newID, err := content.NewID(at)
			if err != nil {
				t.Fatal(err)
			}
			err = s.consumeToken(context.Background(), draft.Purpose, got.TokenDigest, draft.ExpiresAt.Add(-time.Nanosecond), func(tx *sql.Tx, token Token) error {
				if token.Purpose == "invitation" {
					_, err := tx.Exec(`INSERT INTO accounts(id,email,password_hash,is_administrator,is_editor,state,created_at,updated_at) VALUES(?,?,?,0,1,'active',?,?)`, newID, *token.Email, "boundary hash fixture", got.CreatedAt, got.CreatedAt)
					return err
				}
				_, err := tx.Exec("UPDATE accounts SET password_hash='boundary hash fixture' WHERE id=?", *token.AccountID)
				return err
			})
			if err != nil {
				t.Fatal("authorized consumption must succeed strictly before expiry")
			}
		})
	}
}

func TestTokenConsumptionCommitsAuthorizedOperationAndPersistsReuseDenial(t *testing.T) {
	s, path := accountSite(t)
	ctx := context.Background()
	at := tokenAt()
	invitation, _ := tokenIssue(t, s, tokenInvite("new@example.test", at.Add(time.Hour)), at)
	newID, err := content.NewID(at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	usedAt := time.Date(2026, 10, 1, 9, 36, 0, 123456789, time.FixedZone("offset", -3*3600))
	stamp := "2026-10-01T12:36:00.123Z"
	err = s.consumeToken(ctx, "invitation", invitation.TokenDigest, usedAt, func(tx *sql.Tx, token Token) error {
		if token.ID != invitation.ID || token.Email == nil || *token.Email != "new@example.test" || token.AccountID != nil {
			return errors.New("callback did not receive the purpose-bound invitation")
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO accounts(id,email,password_hash,is_administrator,is_editor,state,created_at,updated_at) VALUES(?,?,?,0,1,'active',?,?)`, newID, *token.Email, "initial hash fixture", stamp, stamp)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if account, err := s.GetAccount(ctx, newID); err != nil || account.Email != "new@example.test" {
		t.Fatal("successful consumption did not commit account creation")
	}
	reset, _ := tokenIssue(t, s, tokenReset(newID, at.Add(2*time.Hour)), at)
	err = s.consumeToken(ctx, "password_reset", reset.TokenDigest, usedAt, func(tx *sql.Tx, token Token) error {
		if token.AccountID == nil || *token.AccountID != newID || token.Email != nil {
			return errors.New("callback did not receive account-bound reset")
		}
		_, err := tx.ExecContext(ctx, "UPDATE accounts SET password_hash=?,updated_at=? WHERE id=?", "replacement hash fixture", stamp, *token.AccountID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	verify := func(s *Store) {
		t.Helper()
		for _, original := range []Token{invitation, reset} {
			stored := tokenStored(t, s, original.ID)
			original.UsedAt = tokenString(stamp)
			if !reflect.DeepEqual(stored, original) {
				t.Fatal("consumption must change only used_at to supplied UTC clock")
			}
			tokenDenied(t, s, original.Purpose, original.TokenDigest, usedAt.Add(time.Minute))
		}
		if account, err := s.GetAccount(ctx, newID); err != nil || account.PasswordHash != "replacement hash fixture" {
			t.Fatal("authorized password change did not persist")
		}
	}
	verify(s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	verify(reopened)
}

func TestTokenCallbackFailureAndNilCallbackDoNotConsume(t *testing.T) {
	s, _ := accountSite(t)
	ctx := context.Background()
	at := tokenAt()
	id := sessionAccount(t, s, "rollback@example.test", at)
	got, _ := tokenIssue(t, s, tokenReset(id, at.Add(time.Hour)), at)
	wantErr := errors.New("authorized operation failed")
	err := s.consumeToken(ctx, got.Purpose, got.TokenDigest, at.Add(time.Minute), func(tx *sql.Tx, token Token) error {
		if _, err := tx.ExecContext(ctx, "UPDATE accounts SET password_hash='must rollback' WHERE id=?", *token.AccountID); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatal("callback error must propagate")
	}
	if account, err := s.GetAccount(ctx, id); err != nil || account.PasswordHash != "opaque hash" || !reflect.DeepEqual(tokenStored(t, s, got.ID), got) {
		t.Fatal("callback failure must roll back both authorized write and token use")
	}
	if err := s.consumeToken(ctx, got.Purpose, got.TokenDigest, at, nil); err == nil {
		t.Fatal("nil callback must not allow standalone token consumption")
	}
	if !reflect.DeepEqual(tokenStored(t, s, got.ID), got) {
		t.Fatal("nil callback consumed token")
	}
	if err := s.consumeToken(ctx, got.Purpose, got.TokenDigest, at, func(tx *sql.Tx, token Token) error {
		_, err := tx.ExecContext(ctx, "UPDATE accounts SET password_hash='retry succeeded' WHERE id=?", *token.AccountID)
		return err
	}); err != nil {
		t.Fatal("rolled-back token must be reusable for a successful authorized operation")
	}
}

func TestTokenDatabaseFailureRollsBackAuthorizedChange(t *testing.T) {
	s, _ := accountSite(t)
	ctx := context.Background()
	at := tokenAt()
	id := sessionAccount(t, s, "fault@example.test", at)
	got, _ := tokenIssue(t, s, tokenReset(id, at.Add(time.Hour)), at)
	// A deferred FK forces failure at COMMIT, after callback and used_at writes.
	if _, err := s.db.Exec(`CREATE TABLE token_test_commit_fault(account_id TEXT REFERENCES accounts(id) DEFERRABLE INITIALLY DEFERRED) STRICT`); err != nil {
		t.Fatal(err)
	}
	err := s.consumeToken(ctx, got.Purpose, got.TokenDigest, at, func(tx *sql.Tx, token Token) error {
		if _, err := tx.ExecContext(ctx, "UPDATE accounts SET password_hash='commit must fail' WHERE id=?", *token.AccountID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO token_test_commit_fault VALUES('missing')")
		return err
	})
	if err == nil {
		t.Fatal("transaction commit fault must be reported")
	}
	if account, err := s.GetAccount(ctx, id); err != nil || account.PasswordHash != "opaque hash" || !reflect.DeepEqual(tokenStored(t, s, got.ID), got) {
		t.Fatal("failed commit persisted authorized change or token use")
	}
	if _, err := s.db.Exec(`CREATE TRIGGER token_test_use_fault BEFORE UPDATE OF used_at ON tokens BEGIN SELECT RAISE(ABORT,'token use fault'); END`); err != nil {
		t.Fatal(err)
	}
	err = s.consumeToken(ctx, got.Purpose, got.TokenDigest, at, func(tx *sql.Tx, token Token) error {
		_, err := tx.ExecContext(ctx, "UPDATE accounts SET password_hash='use must fail' WHERE id=?", *token.AccountID)
		return err
	})
	if err == nil {
		t.Fatal("used_at SQL fault must be reported")
	}
	if account, err := s.GetAccount(ctx, id); err != nil || account.PasswordHash != "opaque hash" || !reflect.DeepEqual(tokenStored(t, s, got.ID), got) {
		t.Fatal("token-use SQL fault committed partial state")
	}
}

func TestTokenCancellationRollsBackAndDeniesBeforeCallback(t *testing.T) {
	s, _ := accountSite(t)
	at := tokenAt()
	id := sessionAccount(t, s, "cancel@example.test", at)
	got, _ := tokenIssue(t, s, tokenReset(id, at.Add(time.Hour)), at)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := s.consumeToken(ctx, got.Purpose, got.TokenDigest, at, func(*sql.Tx, Token) error { called = true; return nil })
	if !errors.Is(err, context.Canceled) || called {
		t.Fatal("pre-canceled consumption must return cancellation before callback")
	}
	failed, raw, err := s.IssueToken(ctx, tokenInvite("cancel@example.test", at.Add(time.Hour)), at)
	if !errors.Is(err, context.Canceled) || raw != "" || !reflect.DeepEqual(failed, Token{}) {
		t.Fatal("pre-canceled issuance must return cancellation without credential")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	err = s.consumeToken(ctx, got.Purpose, got.TokenDigest, at, func(tx *sql.Tx, token Token) error {
		if _, err := tx.ExecContext(ctx, "UPDATE accounts SET password_hash='canceled change' WHERE id=?", *token.AccountID); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if err == nil {
		t.Fatal("cancellation after authorized write must prevent successful commit")
	}
	if account, err := s.GetAccount(context.Background(), id); err != nil || account.PasswordHash != "opaque hash" || !reflect.DeepEqual(tokenStored(t, s, got.ID), got) {
		t.Fatal("canceled transaction persisted partial state")
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM tokens").Scan(&count); err != nil || count != 1 {
		t.Fatal("canceled issuance left a row")
	}
}

func TestTokenConcurrentStoresAuthorizeAtMostOneOperation(t *testing.T) {
	s, path := accountSite(t)
	at := tokenAt()
	id := sessionAccount(t, s, "race@example.test", at)
	got, _ := tokenIssue(t, s, tokenReset(id, at.Add(time.Hour)), at)
	if _, err := s.db.Exec("CREATE TABLE token_test_effects(token_id TEXT NOT NULL) STRICT"); err != nil {
		t.Fatal(err)
	}
	const attempts = 8
	stores := []*Store{s}
	for i := 1; i < attempts; i++ {
		other, err := Open(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { other.Close() })
		stores = append(stores, other)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, attempts)
	var callbacks atomic.Int32
	var wg sync.WaitGroup
	for _, candidate := range stores {
		wg.Add(1)
		go func(s *Store) {
			defer wg.Done()
			<-start
			results <- s.consumeToken(ctx, got.Purpose, got.TokenDigest, at.Add(time.Minute), func(tx *sql.Tx, token Token) error {
				callbacks.Add(1)
				if _, err := tx.ExecContext(ctx, "UPDATE accounts SET password_hash='race succeeded' WHERE id=?", *token.AccountID); err != nil {
					return err
				}
				// No UNIQUE constraint may hide an accidental second callback.
				_, err := tx.ExecContext(ctx, "INSERT INTO token_test_effects VALUES(?)", token.ID)
				return err
			})
		}(candidate)
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("concurrent loser must see token denial, not an unrelated SQL failure: %v", err)
		}
	}
	var effects int
	if err := s.db.QueryRow("SELECT count(*) FROM token_test_effects WHERE token_id=?", got.ID).Scan(&effects); err != nil {
		t.Fatal(err)
	}
	if successes != 1 || callbacks.Load() != 1 || effects != 1 {
		t.Fatalf("concurrent claims: successes=%d callbacks=%d committed effects=%d, want one each", successes, callbacks.Load(), effects)
	}
	if account, err := s.GetAccount(context.Background(), id); err != nil || account.PasswordHash != "race succeeded" {
		t.Fatal("winning operation did not commit")
	}
	stored := tokenStored(t, s, got.ID)
	if stored.UsedAt == nil || *stored.UsedAt != at.Add(time.Minute).Format("2006-01-02T15:04:05.000Z") || stored.ExpiresAt != got.ExpiresAt {
		t.Fatal("winning consumption timestamp or fixed expiry differs")
	}
}

func TestTokenSchemaConstraintsAndDigestOnlyStorage(t *testing.T) {
	s, path := accountSite(t)
	at := tokenAt()
	id := sessionAccount(t, s, "schema-token@example.test", at)
	created, expires := "2026-10-01T12:34:56.789Z", "2026-10-01T13:34:56.789Z"
	insert := func(id string, purpose, digest, account, email, created, expires, used any) error {
		_, err := s.db.Exec(`INSERT INTO tokens(id,purpose,token_digest,account_id,email,created_at,expires_at,used_at) VALUES(?,?,?,?,?,?,?,?)`, id, purpose, digest, account, email, created, expires, used)
		return err
	}
	digest := sessionDigest("schema-token")
	if err := insert("valid invite", "invitation", digest, nil, "a@example.test", created, expires, nil); err != nil {
		t.Fatal(err)
	}
	if err := insert("valid reset", "password_reset", sessionDigest("schema-reset"), id, nil, created, expires, created); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name                                              string
		purpose, digest, account, email, created, expires any
	}{
		{"unknown purpose", "unknown", sessionDigest("purpose"), nil, "a@example.test", created, expires},
		{"null purpose", nil, sessionDigest("null purpose"), nil, "a@example.test", created, expires},
		{"invite no email", "invitation", sessionDigest("no email"), nil, nil, created, expires},
		{"invite account", "invitation", sessionDigest("invite account"), id, "a@example.test", created, expires},
		{"reset no account", "password_reset", sessionDigest("no account"), nil, nil, created, expires},
		{"reset email", "password_reset", sessionDigest("reset email"), id, "a@example.test", created, expires},
		{"dead foreign key", "password_reset", sessionDigest("dead foreign key"), "missing", nil, created, expires},
		{"uppercase email", "invitation", sessionDigest("upper email"), nil, "A@example.test", created, expires},
		{"spaces email", "invitation", sessionDigest("space email"), nil, " a@example.test ", created, expires},
		{"duplicate digest across purposes", "password_reset", digest, id, nil, created, expires},
		{"null digest", "invitation", nil, nil, "a@example.test", created, expires},
		{"short digest", "invitation", make([]byte, 31), nil, "a@example.test", created, expires},
		{"long digest", "invitation", make([]byte, 33), nil, "a@example.test", created, expires},
		{"text digest", "invitation", strings.Repeat("x", 32), nil, "a@example.test", created, expires},
		{"null created", "invitation", sessionDigest("null created"), nil, "a@example.test", nil, expires},
		{"null expires", "invitation", sessionDigest("null expires"), nil, "a@example.test", created, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := insert(tc.name, tc.purpose, tc.digest, tc.account, tc.email, tc.created, tc.expires, nil); err == nil {
				t.Fatal("tokens schema accepted invalid row")
			}
		})
	}
	if err := insert("valid invite", "invitation", sessionDigest("duplicate ID"), nil, "a@example.test", created, expires, nil); err == nil {
		t.Fatal("tokens schema accepted duplicate ID")
	}
	var count, strict, indexed int
	if err := s.db.QueryRow("SELECT count(*) FROM tokens").Scan(&count); err != nil || count != 2 {
		t.Fatal("failed schema inserts changed row count")
	}
	if err := s.db.QueryRow("SELECT strict FROM pragma_table_list WHERE name='tokens'").Scan(&strict); err != nil || strict != 1 {
		t.Fatal("tokens must be STRICT")
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM pragma_index_list('tokens') l JOIN pragma_index_info(l.name) i WHERE i.seqno=0 AND i.name='account_id'`).Scan(&indexed); err != nil || indexed < 1 {
		t.Fatal("tokens must index account_id")
	}
	rows, err := s.db.Query("SELECT name FROM pragma_table_info('tokens') ORDER BY cid")
	if err != nil {
		t.Fatal(err)
	}
	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		columns = append(columns, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || !reflect.DeepEqual(columns, []string{"id", "purpose", "token_digest", "account_id", "email", "created_at", "expires_at", "used_at"}) {
		t.Fatal("token schema must contain only contracted columns, never raw credential")
	}
	_, raw := tokenIssue(t, s, tokenInvite("secret-storage@example.test", at.Add(time.Hour)), at)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal"} {
		data, err := os.ReadFile(path + suffix)
		if os.IsNotExist(err) && suffix != "" {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(raw)) || bytes.Contains(data, decoded) {
			t.Fatal("database or WAL contains reusable raw token material")
		}
	}
}

func TestTokenOperationsPreserveUnrelatedDataAndSchema(t *testing.T) {
	s, path := accountSite(t)
	ctx := context.Background()
	at := tokenAt()
	id := sessionAccount(t, s, "preserve-token@example.test", at)
	createSessionForTest(t, s, id, sessionDigest("preserve-token session"), at)
	pageID, err := s.CreateItem(ctx, content.ItemDraft{Title: "Published", Path: "/token-preserve", Fields: map[string]string{"body": "public body"}}, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish(ctx, pageID, 1, at, "local-prototype", content.ValidateStoredPageDraft); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDraft(ctx, pageID, 1, content.ItemDraft{Title: "Draft", Path: "/token-preserve", Fields: map[string]string{"body": "private body"}}, at.Add(time.Minute), "local-prototype"); err != nil {
		t.Fatal(err)
	}
	snapshot := func(s *Store) map[string][][]any {
		t.Helper()
		result := map[string][][]any{}
		for _, table := range []string{"site", "active_config", "items", "snapshots", "routes", "sessions", "site_settings", "throttle_counters", "sqlite_schema"} {
			rows, err := s.db.Query("SELECT * FROM " + table + " ORDER BY 1")
			if err != nil {
				t.Fatal(err)
			}
			columns, err := rows.Columns()
			if err != nil {
				rows.Close()
				t.Fatal(err)
			}
			for rows.Next() {
				values, destinations := make([]any, len(columns)), make([]any, len(columns))
				for i := range values {
					destinations[i] = &values[i]
				}
				if err := rows.Scan(destinations...); err != nil {
					rows.Close()
					t.Fatal(err)
				}
				for i, value := range values {
					if b, ok := value.([]byte); ok {
						values[i] = append([]byte(nil), b...)
					}
				}
				result[table] = append(result[table], values)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		return result
	}
	before := snapshot(s)
	var schemaBefore int
	if err := s.db.QueryRow("PRAGMA schema_version").Scan(&schemaBefore); err != nil {
		t.Fatal(err)
	}
	got, _ := tokenIssue(t, s, tokenReset(id, at.Add(time.Hour)), at)
	if _, err := s.GetTokenByTokenDigest(ctx, got.Purpose, got.TokenDigest, at); err != nil {
		t.Fatal(err)
	}
	if err := s.consumeToken(ctx, got.Purpose, got.TokenDigest, at.Add(time.Minute), func(tx *sql.Tx, token Token) error {
		_, err := tx.ExecContext(ctx, "UPDATE accounts SET password_hash='authorized only' WHERE id=?", *token.AccountID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	tokenDenied(t, s, got.Purpose, got.TokenDigest, at.Add(2*time.Minute))
	if !reflect.DeepEqual(snapshot(s), before) {
		t.Fatal("token operations changed unrelated content, sessions, settings, throttle state, or schema")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var schemaAfter int
	if err := reopened.db.QueryRow("PRAGMA schema_version").Scan(&schemaAfter); err != nil || schemaAfter != schemaBefore || !reflect.DeepEqual(snapshot(reopened), before) {
		t.Fatal("reopen changed unrelated data or ran DDL")
	}
}

func TestTokenStartupDoesNotInstallMissingTable(t *testing.T) {
	s, path := accountSite(t)
	if _, err := s.db.Exec("DROP TABLE tokens"); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := s.db.QueryRow("PRAGMA schema_version").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	opened, err := Open(context.Background(), path)
	if err == nil {
		opened.Close()
	}
	db, err := connect(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tables, after int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='tokens'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("PRAGMA schema_version").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if tables != 0 || before != after {
		t.Fatal("ordinary startup installed or changed token schema")
	}
}
