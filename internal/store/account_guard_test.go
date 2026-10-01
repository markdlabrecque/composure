package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
)

// Proposed store API, following accounts.go and sessions.go:
// DeactivateAccount(ctx, id, at) error
// SetAccountRoles(ctx, id, isAdministrator, isEditor, at) error
// ErrLastActiveAdministrator is a store sentinel usable with errors.Is.
// Missing accounts return content.ErrNotFound. Each actual change and its
// target-account session revocations must commit in the same transaction.

var accountGuardAt = time.Date(2026, 9, 30, 12, 0, 0, 123000000, time.UTC)
var accountGuardChangeAt = time.Date(2026, 9, 30, 9, 5, 6, 456789123, time.FixedZone("offset", -4*3600))

const accountGuardChangeTimestamp = "2026-09-30T13:05:06.456Z"

type accountGuardMutation struct {
	name          string
	deactivate    bool
	administrator bool
	editor        bool
}

func (m accountGuardMutation) apply(ctx context.Context, s *Store, id string, at time.Time) error {
	if m.deactivate {
		return s.DeactivateAccount(ctx, id, at)
	}
	return s.SetAccountRoles(ctx, id, m.administrator, m.editor, at)
}

func accountGuardCreate(t *testing.T, s *Store, email string, administrator, editor bool, state string) string {
	t.Helper()
	id, err := s.CreateAccount(context.Background(), AccountDraft{
		Email: email, PasswordHash: "opaque fixture hash", IsAdministrator: administrator, IsEditor: editor, State: state,
	}, accountGuardAt)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func accountGuardGet(t *testing.T, s *Store, id string) Account {
	t.Helper()
	account, err := s.GetAccount(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return account
}

// Capture complete rows, not just counts, to check failed mutations and
// unrelated content, attribution, configuration and schema stay unchanged.
func accountGuardRows(t *testing.T, s *Store, tables ...string) map[string][][]any {
	t.Helper()
	result := make(map[string][][]any)
	for _, table := range tables {
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

func accountGuardAllRows(t *testing.T, s *Store) map[string][][]any {
	t.Helper()
	return accountGuardRows(t, s, "accounts", "sessions", "items", "snapshots", "routes", "site", "active_config", "sqlite_schema")
}

func accountGuardPage(t *testing.T, s *Store, actor string) {
	t.Helper()
	ctx := context.Background()
	id, err := s.CreateItem(ctx, content.ItemDraft{Title: "Keep", Path: "/keep", Fields: map[string]string{"body": "published"}}, accountGuardAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish(ctx, id, 1, accountGuardAt, actor, content.ValidateStoredPageDraft); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDraft(ctx, id, 1, content.ItemDraft{Title: "Private", Path: "/keep", Fields: map[string]string{"body": "draft"}}, accountGuardAt.Add(time.Minute), actor); err != nil {
		t.Fatal(err)
	}
}

func TestAccountGuardRejectsLastActiveAdministratorWithoutWrites(t *testing.T) {
	for _, editor := range []bool{false, true} {
		for _, extra := range []string{"none", "active editor", "deactivated administrator"} {
			for _, mutation := range []accountGuardMutation{{name: "deactivate", deactivate: true}, {name: "remove administrator", editor: true}} {
				t.Run(fmt.Sprintf("editor=%t/%s/%s", editor, extra, mutation.name), func(t *testing.T) {
					s, _ := accountSite(t)
					id := accountGuardCreate(t, s, "last@example.test", true, editor, "active")
					if extra != "none" {
						state := "active"
						if extra == "deactivated administrator" {
							state = "deactivated"
						}
						accountGuardCreate(t, s, "other@example.test", extra == "deactivated administrator", true, state)
					}
					accountGuardPage(t, s, id)
					for i := 0; i < 2; i++ {
						createSessionForTest(t, s, id, sessionDigest(fmt.Sprintf("last %d", i)), accountGuardAt)
					}
					before := accountGuardAllRows(t, s)
					if err := mutation.apply(context.Background(), s, id, accountGuardChangeAt); !errors.Is(err, ErrLastActiveAdministrator) {
						t.Fatalf("last active administrator error = %v, want ErrLastActiveAdministrator", err)
					}
					if !reflect.DeepEqual(before, accountGuardAllRows(t, s)) {
						t.Fatal("rejected last-administrator change altered persisted data or schema")
					}
				})
			}
		}
	}
}

func TestAccountGuardAllowedChangesRevokeOnlyTargetAndPreserveAttribution(t *testing.T) {
	cases := []struct {
		name                  string
		administrator, editor bool
		mutation              accountGuardMutation
	}{
		{"deactivate dual role", true, true, accountGuardMutation{deactivate: true}},
		{"deactivate administrator only", true, false, accountGuardMutation{deactivate: true}},
		{"deactivate editor only", false, true, accountGuardMutation{deactivate: true}},
		{"remove administrator from dual role", true, true, accountGuardMutation{editor: true}},
		{"replace administrator with editor", true, false, accountGuardMutation{editor: true}},
		{"grant administrator", false, true, accountGuardMutation{administrator: true, editor: true}},
		{"grant editor", true, false, accountGuardMutation{administrator: true, editor: true}},
		{"remove editor", true, true, accountGuardMutation{administrator: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, path := accountSite(t)
			ctx := context.Background()
			id := accountGuardCreate(t, s, "target@example.test", tc.administrator, tc.editor, "active")
			otherID := accountGuardCreate(t, s, "actor@example.test", true, false, "active")
			beforeAccount, beforeOther := accountGuardGet(t, s, id), accountGuardGet(t, s, otherID)
			accountGuardPage(t, s, id)
			unrelated := accountGuardRows(t, s, "items", "snapshots", "routes", "site", "active_config", "sqlite_schema")
			ids := make([]string, 4)
			digests := make([][]byte, 4)
			for i := range ids {
				digests[i] = sessionDigest(fmt.Sprintf("target %d", i))
				ids[i] = createSessionForTest(t, s, id, digests[i], accountGuardAt)
			}
			oldRevocation := accountGuardAt.Add(time.Minute)
			if err := s.RevokeSession(ctx, ids[0], oldRevocation); err != nil {
				t.Fatal(err)
			}
			// Required revocation includes expired rows as well as usable rows.
			if _, err := s.db.Exec("UPDATE sessions SET expires_at=? WHERE id=?", accountGuardAt.Add(time.Minute).Format("2006-01-02T15:04:05.000Z"), ids[1]); err != nil {
				t.Fatal(err)
			}
			otherDigest := sessionDigest("unaffected actor")
			otherSession := createSessionForTest(t, s, otherID, otherDigest, accountGuardAt)
			beforeSessions := accountGuardRows(t, s, "sessions")
			if err := tc.mutation.apply(ctx, s, id, accountGuardChangeAt); err != nil {
				t.Fatal(err)
			}
			want := beforeAccount
			want.UpdatedAt = accountGuardChangeTimestamp
			if tc.mutation.deactivate {
				want.State = "deactivated"
			} else {
				want.IsAdministrator, want.IsEditor = tc.mutation.administrator, tc.mutation.editor
			}
			// Only revoked_at may change on retained target sessions.
			for _, row := range beforeSessions["sessions"] {
				if row[1] == id && row[5] == nil {
					row[5] = accountGuardChangeTimestamp
				}
			}
			verify := func(s *Store) {
				t.Helper()
				if accountGuardGet(t, s, id) != want || accountGuardGet(t, s, otherID) != beforeOther {
					t.Fatal("account change lost identity, independent roles, metadata or affected another account")
				}
				if !reflect.DeepEqual(beforeSessions, accountGuardRows(t, s, "sessions")) {
					t.Fatal("revocation changed session data beyond target unrevoked rows")
				}
				for _, digest := range digests {
					assertSessionDenied(t, s, digest, accountGuardChangeAt)
				}
				assertSessionUsable(t, s, otherSession, otherID, otherDigest, accountGuardChangeAt)
				if !reflect.DeepEqual(unrelated, accountGuardRows(t, s, "items", "snapshots", "routes", "site", "active_config", "sqlite_schema")) {
					t.Fatal("account mutation changed historical attribution, content, configuration or schema")
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
		})
	}
}

func TestAccountGuardLastAdministratorCanChangeEditorFlag(t *testing.T) {
	s, _ := accountSite(t)
	id := accountGuardCreate(t, s, "last@example.test", true, false, "active")
	for i, editor := range []bool{true, false} {
		digest := sessionDigest(fmt.Sprintf("editor change %d", i))
		createSessionForTest(t, s, id, digest, accountGuardAt)
		if err := s.SetAccountRoles(context.Background(), id, true, editor, accountGuardChangeAt.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
		got := accountGuardGet(t, s, id)
		if !got.IsAdministrator || got.IsEditor != editor || got.State != "active" {
			t.Fatal("changing editor role lost the last active administrator")
		}
		assertSessionDenied(t, s, digest, accountGuardChangeAt)
	}
}

func TestAccountGuardNoOpAndMissingAccountDoNotWrite(t *testing.T) {
	s, _ := accountSite(t)
	active := accountGuardCreate(t, s, "last@example.test", true, true, "active")
	inactive := accountGuardCreate(t, s, "inactive@example.test", true, false, "deactivated")
	createSessionForTest(t, s, active, sessionDigest("no-op active"), accountGuardAt)
	createSessionForTest(t, s, inactive, sessionDigest("no-op inactive"), accountGuardAt)
	cases := []struct {
		name, id string
		mutation accountGuardMutation
		missing  bool
	}{
		{"same active roles", active, accountGuardMutation{administrator: true, editor: true}, false},
		{"same inactive roles", inactive, accountGuardMutation{administrator: true}, false},
		{"already deactivated", inactive, accountGuardMutation{deactivate: true}, false},
		{"missing deactivation", "missing", accountGuardMutation{deactivate: true}, true},
		{"missing roles", "missing", accountGuardMutation{editor: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := accountGuardAllRows(t, s)
			err := tc.mutation.apply(context.Background(), s, tc.id, accountGuardChangeAt)
			if tc.missing {
				if !errors.Is(err, content.ErrNotFound) {
					t.Fatalf("missing account error = %v, want ErrNotFound", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, accountGuardAllRows(t, s)) {
				t.Fatal("no-op or missing-account mutation wrote data")
			}
		})
	}
}

func TestAccountGuardPersistenceFailureRollsBackAccountAndRevocation(t *testing.T) {
	for _, mutation := range []accountGuardMutation{{name: "deactivate", deactivate: true}, {name: "remove administrator", editor: true}} {
		for _, table := range []string{"accounts", "sessions"} {
			t.Run(mutation.name+"/fail "+table, func(t *testing.T) {
				s, _ := accountSite(t)
				id := accountGuardCreate(t, s, "target@example.test", true, true, "active")
				accountGuardCreate(t, s, "other@example.test", true, false, "active")
				for i := 0; i < 2; i++ {
					createSessionForTest(t, s, id, sessionDigest(fmt.Sprintf("rollback %d", i)), accountGuardAt)
				}
				// AFTER UPDATE fails after SQLite has attempted the write. Testing
				// both tables detects partial commits in either mutation order.
				if _, err := s.db.Exec("CREATE TRIGGER account_guard_fail AFTER UPDATE ON " + table + " BEGIN SELECT RAISE(ABORT, 'account guard fixture failure'); END"); err != nil {
					t.Fatal(err)
				}
				before := accountGuardAllRows(t, s)
				if err := mutation.apply(context.Background(), s, id, accountGuardChangeAt); err == nil {
					t.Fatal("persistence failure must not report success")
				}
				if !reflect.DeepEqual(before, accountGuardAllRows(t, s)) {
					t.Fatal("failed mutation left partial account or session changes")
				}
				if _, err := s.db.Exec("DROP TRIGGER account_guard_fail"); err != nil {
					t.Fatal(err)
				}
				if err := mutation.apply(context.Background(), s, id, accountGuardChangeAt); err != nil {
					t.Fatalf("valid mutation failed after removing injected SQL failure: %v", err)
				}
			})
		}
	}
}

func TestAccountGuardConcurrentRemovalsAcrossStoresPreserveAdministrator(t *testing.T) {
	demote := accountGuardMutation{name: "demote", editor: true}
	deactivate := accountGuardMutation{name: "deactivate", deactivate: true}
	for _, pair := range [][2]accountGuardMutation{{demote, demote}, {deactivate, deactivate}, {demote, deactivate}} {
		for round := 0; round < 4; round++ {
			t.Run(fmt.Sprintf("%s/%s/%d", pair[0].name, pair[1].name, round), func(t *testing.T) {
				s, path := accountSite(t)
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				other, err := Open(ctx, path)
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
				stores := [2]*Store{s, other}
				ids := [2]string{accountGuardCreate(t, s, "a@example.test", true, true, "active"), accountGuardCreate(t, s, "b@example.test", true, true, "active")}
				before := [2]Account{accountGuardGet(t, s, ids[0]), accountGuardGet(t, s, ids[1])}
				for i, id := range ids {
					createSessionForTest(t, s, id, sessionDigest(fmt.Sprintf("concurrent %d", i)), accountGuardAt)
				}
				// Both callers reach the barrier before release; no sleeps or
				// assumptions about which connection wins the SQLite write lock.
				start := make(chan struct{})
				var ready, done sync.WaitGroup
				ready.Add(2)
				done.Add(2)
				var errs [2]error
				for i := range ids {
					go func(i int) {
						defer done.Done()
						ready.Done()
						<-start
						errs[i] = pair[i].apply(ctx, stores[i], ids[i], accountGuardChangeAt)
					}(i)
				}
				ready.Wait()
				close(start)
				done.Wait()
				if errs[0] == nil && errs[1] == nil {
					t.Fatal("both competing administrator removals reported success")
				}
				if errs[0] != nil && errs[1] != nil {
					t.Fatal("neither valid competing mutation succeeded on an otherwise idle database")
				}
				var activeAdministrators int
				if err := s.db.QueryRow("SELECT count(*) FROM accounts WHERE state='active' AND is_administrator=1").Scan(&activeAdministrators); err != nil || activeAdministrators != 1 {
					t.Fatalf("active administrators = %d, want 1; %v", activeAdministrators, err)
				}
				for i, id := range ids {
					got, want := accountGuardGet(t, s, id), before[i]
					var revoked any
					if err := s.db.QueryRow("SELECT revoked_at FROM sessions WHERE account_id=?", id).Scan(&revoked); err != nil {
						t.Fatal(err)
					}
					if errs[i] == nil {
						want.UpdatedAt = accountGuardChangeTimestamp
						if pair[i].deactivate {
							want.State = "deactivated"
						} else {
							want.IsAdministrator = false
						}
						if revoked != accountGuardChangeTimestamp {
							t.Fatal("successful concurrent change did not revoke its target session")
						}
					} else {
						if revoked != nil {
							t.Fatal("failed concurrent change revoked the remaining administrator session")
						}
						// A transient lock error is allowed, but retry after contention
						// must observe the committed invariant and leave no writes.
						beforeRetry := accountGuardAllRows(t, s)
						if err := pair[i].apply(ctx, stores[i], id, accountGuardChangeAt); !errors.Is(err, ErrLastActiveAdministrator) {
							t.Fatalf("surviving administrator retry error = %v", err)
						}
						if !reflect.DeepEqual(beforeRetry, accountGuardAllRows(t, s)) {
							t.Fatal("rejected concurrent-removal retry changed account or session data")
						}
					}
					if got != want || accountGuardGet(t, s, id) != want {
						t.Fatal("concurrent mutation or retry left incorrect account data")
					}
				}
			})
		}
	}
}
