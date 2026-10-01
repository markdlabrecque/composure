package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
)

// Proposed API follows accounts.go: CreateSession(ctx, SessionDraft, at) returns
// an ID; GetSessionByTokenDigest(ctx, digest, now) returns a usable Session;
// RevokeSession(ctx, sessionID, at) and RevokeAllSessions(ctx, accountID, at)
// return errors. SessionDraft takes AccountID and TokenDigest []byte, never a
// raw credential or caller-selected expiry. Session timestamps are strings,
// with RevokedAt *string for the nullable timestamp. Missing/unusable lookups
// return content.ErrNotFound, as account lookups do.

func sessionAccount(t *testing.T, s *Store, email string, at time.Time) string {
	t.Helper()
	id, err := s.CreateAccount(context.Background(), AccountDraft{
		Email: email, PasswordHash: "opaque hash", IsEditor: true, State: "active",
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func sessionDigest(label string) []byte {
	// These are deterministic digest fixtures, not browser credentials.
	digest := sha256.Sum256([]byte(label))
	return digest[:]
}

func createSessionForTest(t *testing.T, s *Store, accountID string, digest []byte, at time.Time) string {
	t.Helper()
	id, err := s.CreateSession(context.Background(), SessionDraft{AccountID: accountID, TokenDigest: digest}, at)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func assertSessionDenied(t *testing.T, s *Store, digest []byte, now time.Time) {
	t.Helper()
	got, err := s.GetSessionByTokenDigest(context.Background(), digest, now)
	if !errors.Is(err, content.ErrNotFound) {
		t.Fatalf("unusable session error = %v, want ErrNotFound", err)
	}
	if got.ID != "" || got.AccountID != "" {
		t.Fatal("unusable lookup returned a session identity")
	}
}

func assertSessionUsable(t *testing.T, s *Store, id, accountID string, digest []byte, now time.Time) {
	t.Helper()
	got, err := s.GetSessionByTokenDigest(context.Background(), digest, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != id || got.AccountID != accountID || !bytes.Equal(got.TokenDigest, digest) || got.RevokedAt != nil {
		t.Fatal("usable session lookup did not return the expected unrevoked row")
	}
}

func TestSessionRoundTripAndRestart(t *testing.T) {
	s, path := accountSite(t)
	ctx := context.Background()
	at := time.Date(2026, 9, 30, 23, 34, 56, 789123456, time.FixedZone("offset", -7*3600))
	accountID := sessionAccount(t, s, "session@example.test", at)
	digest := sessionDigest("round trip")
	id := createSessionForTest(t, s, accountID, digest, at)
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id) {
		t.Fatalf("not UUIDv7: %q", id)
	}
	ms, err := strconv.ParseInt(strings.ReplaceAll(id[:13], "-", ""), 16, 64)
	if err != nil || ms != at.UnixMilli() {
		t.Fatalf("UUID clock = %d, want %d; %v", ms, at.UnixMilli(), err)
	}
	want := Session{
		ID: id, AccountID: accountID, TokenDigest: digest,
		CreatedAt: "2026-10-01T06:34:56.789Z", ExpiresAt: "2026-10-01T14:34:56.789Z",
	}
	verify := func(s *Store) {
		t.Helper()
		got, err := s.GetSessionByTokenDigest(ctx, digest, at.Add(7*time.Hour))
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("session round trip differs: %v", err)
		}
		var storedDigest []byte
		var kind, created, expires string
		var revoked sql.NullString
		if err := s.db.QueryRow(`SELECT token_digest,typeof(token_digest),created_at,expires_at,revoked_at FROM sessions WHERE id=?`, id).
			Scan(&storedDigest, &kind, &created, &expires, &revoked); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(storedDigest, digest) || len(storedDigest) != sha256.Size || kind != "blob" || created != want.CreatedAt || expires != want.ExpiresAt || revoked.Valid {
			t.Fatal("stored digest, timestamp format, or initial revocation state differs")
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

func TestSessionAbsoluteExpiryBoundary(t *testing.T) {
	s, _ := accountSite(t)
	at := time.Date(2026, 9, 30, 20, 0, 0, 123000000, time.UTC)
	accountID := sessionAccount(t, s, "expiry@example.test", at)
	digest := sessionDigest("expiry")
	id := createSessionForTest(t, s, accountID, digest, at)
	expires := at.Add(8 * time.Hour)
	for _, elapsed := range []time.Duration{0, time.Hour, 7*time.Hour + 59*time.Minute, 8*time.Hour - time.Nanosecond} {
		assertSessionUsable(t, s, id, accountID, digest, at.Add(elapsed))
	}
	assertSessionDenied(t, s, digest, expires)
	assertSessionDenied(t, s, digest, expires.Add(time.Nanosecond))
	assertSessionDenied(t, s, digest, expires.Add(24*time.Hour))
	var storedExpiry string
	var revoked sql.NullString
	if err := s.db.QueryRow("SELECT expires_at,revoked_at FROM sessions WHERE id=?", id).Scan(&storedExpiry, &revoked); err != nil {
		t.Fatal(err)
	}
	if storedExpiry != "2026-10-01T04:00:00.123Z" || revoked.Valid {
		t.Fatal("lookup must not extend absolute expiry or change revocation state")
	}
}

func TestSessionUnknownDigestAndDeactivatedAccount(t *testing.T) {
	s, _ := accountSite(t)
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	accountID := sessionAccount(t, s, "deactivated@example.test", at)
	digest := sessionDigest("deactivated")
	id := createSessionForTest(t, s, accountID, digest, at)
	assertSessionDenied(t, s, sessionDigest("unknown"), at)
	assertSessionUsable(t, s, id, accountID, digest, at)
	// Account mutation APIs are later tickets. Change the persisted state here
	// to prove each lookup checks it, rather than trusting session creation.
	if _, err := s.db.Exec("UPDATE accounts SET state='deactivated' WHERE id=?", accountID); err != nil {
		t.Fatal(err)
	}
	assertSessionDenied(t, s, digest, at.Add(time.Minute))
	var revoked sql.NullString
	if err := s.db.QueryRow("SELECT revoked_at FROM sessions WHERE id=?", id).Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	if revoked.Valid {
		t.Fatal("deactivated-account denial must work even without explicit session revocation")
	}
	if account, err := s.GetAccount(context.Background(), accountID); err != nil || account.State != "deactivated" {
		t.Fatalf("deactivation must retain the account: %v", err)
	}
}

func TestSessionRevokeOneAndAllPersistAndAreAccountScoped(t *testing.T) {
	s, path := accountSite(t)
	ctx := context.Background()
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	accountA := sessionAccount(t, s, "a@example.test", at)
	accountB := sessionAccount(t, s, "b@example.test", at)
	digests := [][]byte{sessionDigest("a one"), sessionDigest("a two"), sessionDigest("a three"), sessionDigest("b one"), sessionDigest("b two")}
	accounts := []string{accountA, accountA, accountA, accountB, accountB}
	ids := make([]string, len(digests))
	seen := map[string]bool{}
	for i, digest := range digests {
		ids[i] = createSessionForTest(t, s, accounts[i], digest, at)
		if seen[ids[i]] {
			t.Fatal("generated duplicate session ID")
		}
		seen[ids[i]] = true
	}
	revokedAt := time.Date(2026, 9, 30, 9, 5, 6, 456789123, time.FixedZone("offset", -4*3600))
	if err := s.RevokeSession(ctx, ids[0], revokedAt); err != nil {
		t.Fatal(err)
	}
	assertSessionDenied(t, s, digests[0], revokedAt)
	for i := 1; i < len(ids); i++ {
		assertSessionUsable(t, s, ids[i], accounts[i], digests[i], revokedAt)
	}
	var oneTimestamp string
	if err := s.db.QueryRow("SELECT revoked_at FROM sessions WHERE id=?", ids[0]).Scan(&oneTimestamp); err != nil {
		t.Fatal(err)
	}
	if oneTimestamp != "2026-09-30T13:05:06.456Z" {
		t.Fatalf("single revocation timestamp = %q", oneTimestamp)
	}
	// A real reopen must retain both the revoked row and still-usable rows.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertSessionDenied(t, reopened, digests[0], revokedAt)
	for i := 1; i < len(ids); i++ {
		assertSessionUsable(t, reopened, ids[i], accounts[i], digests[i], revokedAt)
	}
	allAt := revokedAt.Add(time.Hour)
	if err := reopened.RevokeAllSessions(ctx, accountA, allAt); err != nil {
		t.Fatal(err)
	}
	verifyAll := func(s *Store) {
		t.Helper()
		for i := 0; i < 3; i++ {
			assertSessionDenied(t, s, digests[i], allAt)
		}
		for i := 3; i < len(ids); i++ {
			assertSessionUsable(t, s, ids[i], accountB, digests[i], allAt)
		}
		for _, id := range ids[1:3] {
			var timestamp string
			if err := s.db.QueryRow("SELECT revoked_at FROM sessions WHERE id=?", id).Scan(&timestamp); err != nil {
				t.Fatal(err)
			}
			if timestamp != "2026-09-30T14:05:06.456Z" {
				t.Fatalf("all-session revocation timestamp = %q", timestamp)
			}
		}
	}
	verifyAll(reopened)
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	verifyAll(again)
}

func TestSessionCreateRejectsInvalidDigestDuplicateAndMissingAccount(t *testing.T) {
	s, _ := accountSite(t)
	ctx := context.Background()
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	accountA := sessionAccount(t, s, "constraints-a@example.test", at)
	accountB := sessionAccount(t, s, "constraints-b@example.test", at)
	digest := sessionDigest("unique")
	id := createSessionForTest(t, s, accountA, digest, at)
	cases := []struct {
		name, accountID string
		digest          []byte
	}{
		{"duplicate same account", accountA, digest},
		{"duplicate other account", accountB, digest},
		{"missing account", "missing", sessionDigest("missing account")},
		{"null digest", accountA, nil},
		{"empty digest", accountA, []byte{}},
		{"short digest", accountA, make([]byte, 31)},
		{"long digest", accountA, make([]byte, 33)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotID, err := s.CreateSession(ctx, SessionDraft{AccountID: tc.accountID, TokenDigest: tc.digest}, at)
			if err == nil || gotID != "" {
				t.Fatal("invalid session create must fail without an ID")
			}
		})
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM sessions").Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed creates changed session count: %d; %v", count, err)
	}
	assertSessionUsable(t, s, id, accountA, digest, at)
}

func TestSessionSchemaConstraints(t *testing.T) {
	s, _ := accountSite(t)
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	accountID := sessionAccount(t, s, "schema@example.test", at)
	insert := func(id string, account, digest, created, expires, revoked any) error {
		_, err := s.db.Exec(`INSERT INTO sessions(id,account_id,token_digest,created_at,expires_at,revoked_at) VALUES(?,?,?,?,?,?)`, id, account, digest, created, expires, revoked)
		return err
	}
	created, expires := "2026-09-30T12:00:00.000Z", "2026-09-30T20:00:00.000Z"
	digest := sessionDigest("schema valid")
	if err := insert("valid", accountID, digest, created, expires, nil); err != nil {
		t.Fatalf("initialized schema must accept a valid session: %v", err)
	}
	cases := []struct {
		name                                       string
		account, digest, created, expires, revoked any
	}{
		{"duplicate digest", accountID, digest, created, expires, nil},
		{"null account", nil, sessionDigest("null account"), created, expires, nil},
		{"foreign key", "missing", sessionDigest("foreign key"), created, expires, nil},
		{"null digest", accountID, nil, created, expires, nil},
		{"short digest", accountID, make([]byte, 31), created, expires, nil},
		{"long digest", accountID, make([]byte, 33), created, expires, nil},
		{"text digest", accountID, strings.Repeat("x", 32), created, expires, nil},
		{"null created", accountID, sessionDigest("null created"), nil, expires, nil},
		{"null expires", accountID, sessionDigest("null expires"), created, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := insert(tc.name, tc.account, tc.digest, tc.created, tc.expires, tc.revoked); err == nil {
				t.Fatal("schema accepted an invalid session")
			}
		})
	}
	if err := insert("valid", accountID, sessionDigest("duplicate id"), created, expires, nil); err == nil {
		t.Fatal("schema accepted a duplicate session ID")
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM sessions").Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed inserts changed row count: %d; %v", count, err)
	}
	var strict int
	if err := s.db.QueryRow("SELECT strict FROM pragma_table_list WHERE name='sessions'").Scan(&strict); err != nil || strict != 1 {
		t.Fatalf("sessions must be STRICT: %d; %v", strict, err)
	}
	var indexed int
	if err := s.db.QueryRow(`SELECT count(*) FROM pragma_index_list('sessions') AS l JOIN pragma_index_info(l.name) AS i WHERE i.seqno=0 AND i.name='account_id'`).Scan(&indexed); err != nil || indexed < 1 {
		t.Fatalf("sessions must index account_id for account-scoped revocation: %d; %v", indexed, err)
	}
}

func TestSessionStartupDoesNotInstallMissingTable(t *testing.T) {
	s, path := accountSite(t)
	ctx := context.Background()
	// Simulate a development database without the new table. Ordinary Open
	// may reject it or open it, but must never become an upgrade mechanism.
	if _, err := s.db.Exec("DROP TABLE sessions"); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := s.db.QueryRow("PRAGMA schema_version").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	opened, err := Open(ctx, path)
	if err == nil {
		if err := opened.Close(); err != nil {
			t.Fatal(err)
		}
	}
	db, err := connect(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tables, after int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='sessions'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("PRAGMA schema_version").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if tables != 0 || after != before {
		t.Fatal("startup installed or changed schema; only fresh development initialization may create sessions")
	}
}

func TestSessionOperationsPreservePagesAccountsAndStartupSchema(t *testing.T) {
	s, path := accountSite(t)
	ctx := context.Background()
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	accountID := sessionAccount(t, s, "preserve@example.test", at)
	pageID, err := s.CreateItem(ctx, content.ItemDraft{Title: "Keep", Path: "/keep", Fields: map[string]string{"body": "published body"}}, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish(ctx, pageID, 1, at, "local-prototype", content.ValidateStoredPageDraft); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDraft(ctx, pageID, 1, content.ItemDraft{Title: "Keep draft", Path: "/keep", Fields: map[string]string{"body": "private body"}}, at.Add(time.Minute), "local-prototype"); err != nil {
		t.Fatal(err)
	}
	// Snapshot every value in the unrelated tables, including attribution,
	// history and routes not returned by the public repository API.
	snapshot := func(s *Store) map[string][][]any {
		t.Helper()
		result := map[string][][]any{}
		for _, table := range []string{"site", "active_config", "accounts", "items", "snapshots", "routes", "sqlite_schema"} {
			rows, err := s.db.Query(fmt.Sprintf("SELECT * FROM %s ORDER BY 1", table))
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
	var schemaVersion int
	if err := s.db.QueryRow("PRAGMA schema_version").Scan(&schemaVersion); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		digest := sessionDigest(fmt.Sprintf("preserve %d", i))
		id := createSessionForTest(t, s, accountID, digest, at)
		assertSessionUsable(t, s, id, accountID, digest, at)
		if i == 0 {
			if err := s.RevokeSession(ctx, id, at.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.RevokeAllSessions(ctx, accountID, at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(s); !reflect.DeepEqual(before, after) {
		t.Fatal("session operations changed Page/account/configuration data or schema")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if after := snapshot(reopened); !reflect.DeepEqual(before, after) {
		t.Fatal("startup changed Page/account/configuration data or schema")
	}
	var afterVersion int
	if err := reopened.db.QueryRow("PRAGMA schema_version").Scan(&afterVersion); err != nil || afterVersion != schemaVersion {
		t.Fatalf("startup or session operations ran DDL: schema version %d -> %d; %v", schemaVersion, afterVersion, err)
	}
	published, err := reopened.PublishedByPath(ctx, "/keep")
	if err != nil || published.Title != "Keep" || published.Fields["body"] != "published body" {
		t.Fatalf("published Page not preserved: %v", err)
	}
	draft, err := reopened.GetItem(ctx, pageID)
	if err != nil || draft.Title != "Keep draft" || draft.Fields["body"] != "private body" || draft.Revision != 2 {
		t.Fatalf("private draft not preserved: %v", err)
	}
}
