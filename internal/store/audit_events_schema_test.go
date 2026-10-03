package store

import (
	"context"
	"testing"
)

func TestAuditEventsSchemaInitializedWithoutChangingVersion(t *testing.T) {
	s, _ := accountSite(t)
	var tables, version, existingTables int
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='audit_events'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 1 {
		t.Fatalf("initialized audit_events table count = %d, want 1", tables)
	}
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("schema marker = %d, want %d", version, SchemaVersion)
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table'
		AND name IN ('site','active_config','accounts','sessions','items','snapshots','routes')`).Scan(&existingTables); err != nil {
		t.Fatal(err)
	}
	if existingTables != 7 {
		t.Fatalf("audit schema initialization retained %d existing tables, want 7", existingTables)
	}
}

func TestOpenDoesNotInstallMissingAuditEventsSchema(t *testing.T) {
	s, path := accountSite(t)
	if _, err := s.db.Exec("DROP TABLE audit_events"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var tables int
	if err := reopened.db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='audit_events'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatal("ordinary startup installed missing audit_events schema")
	}
}
