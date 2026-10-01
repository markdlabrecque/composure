package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func accountSite(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "composure.db")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(context.Background(), path, "account-test-site", time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC), nil); err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func TestAccountSchemaInitialized(t *testing.T) {
	s, _ := accountSite(t)
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM accounts").Scan(&n); err != nil {
		t.Fatalf("initialized database must contain accounts table: %v", err)
	}
	if n != 0 {
		t.Fatalf("new accounts count = %d, want 0", n)
	}
	var sessions int
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='sessions'").Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatal("account initialization unexpectedly created sessions")
	}
}

func TestAccountSchemaConstraints(t *testing.T) {
	s, _ := accountSite(t)
	insert := func(id string, email, hash, admin, editor, state, created, updated any) error {
		_, err := s.db.Exec("INSERT INTO accounts VALUES(?,?,?,?,?,?,?,?)", id, email, hash, admin, editor, state, created, updated)
		return err
	}
	if err := insert("valid", "valid@example.test", "opaque hash", 1, 1, "deactivated", "2026-09-30T12:00:00.000Z", "2026-09-30T12:00:00.000Z"); err != nil {
		t.Fatalf("valid account insert: %v", err)
	}
	cases := []struct {
		name                                                string
		email, hash, admin, editor, state, created, updated any
	}{
		{"uppercase", "UPPER@example.test", "hash", 1, 0, "active", "time", "time"},
		{"spaces", " user@example.test ", "hash", 1, 0, "active", "time", "time"},
		{"duplicate deactivated", "valid@example.test", "hash", 1, 0, "active", "time", "time"},
		{"no roles", "none@example.test", "hash", 0, 0, "active", "time", "time"},
		{"admin boolean", "admin@example.test", "hash", 2, 1, "active", "time", "time"},
		{"editor boolean", "editor@example.test", "hash", 1, -1, "active", "time", "time"},
		{"state", "state@example.test", "hash", 1, 0, "pending", "time", "time"},
		{"null email", nil, "hash", 1, 0, "active", "time", "time"},
		{"null hash", "hash@example.test", nil, 1, 0, "active", "time", "time"},
		{"null admin", "a@example.test", "hash", nil, 1, "active", "time", "time"},
		{"null editor", "e@example.test", "hash", 1, nil, "active", "time", "time"},
		{"null state", "s@example.test", "hash", 1, 0, nil, "time", "time"},
		{"null created", "c@example.test", "hash", 1, 0, "active", nil, "time"},
		{"null updated", "u@example.test", "hash", 1, 0, "active", "time", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := insert(tc.name, tc.email, tc.hash, tc.admin, tc.editor, tc.state, tc.created, tc.updated); err == nil {
				t.Fatal("invalid account accepted")
			}
		})
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM accounts").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("failed inserts changed rows: %d", n)
	}
}

func TestAccountInitializationRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "composure.db")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := connect(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TABLE accounts (sentinel TEXT); INSERT INTO accounts VALUES('keep')"); err != nil {
		t.Fatal(err)
	}
	if err = Initialize(context.Background(), path, "site", time.Now(), nil); err == nil {
		t.Fatal("initialization must fail on conflicting accounts table")
	}
	var n int
	if err = db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name IN ('site','items','snapshots','routes','active_config')").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("failed initialization left %d Page tables", n)
	}
	var sentinel string
	if err = db.QueryRow("SELECT sentinel FROM accounts").Scan(&sentinel); err != nil || sentinel != "keep" {
		t.Fatalf("existing table changed: %q %v", sentinel, err)
	}
}
