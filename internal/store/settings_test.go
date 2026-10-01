package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
)

func TestSettingTableInitialized(t *testing.T) {
	s, _ := accountSite(t)
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM site_settings").Scan(&count); err != nil {
		t.Fatalf("initialized database must contain site_settings: %v", err)
	}
	if count != 0 {
		t.Fatalf("new settings count = %d, want 0", count)
	}
}

func TestSettingMissingKey(t *testing.T) {
	s, _ := accountSite(t)
	ctx := context.Background()
	if _, err := s.GetSetting(ctx, "missing"); !errors.Is(err, content.ErrNotFound) {
		t.Fatalf("missing setting error = %v, want ErrNotFound", err)
	}
	if err := s.SetSetting(ctx, "present", ""); err != nil {
		t.Fatal(err)
	}
	if value, err := s.GetSetting(ctx, "present"); err != nil || value != "" {
		t.Fatalf("stored empty value = %q, error = %v", value, err)
	}
	if _, err := s.GetSetting(ctx, "missing"); !errors.Is(err, content.ErrNotFound) {
		t.Fatalf("missing setting after another write error = %v, want ErrNotFound", err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM site_settings").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("missing lookups changed settings count to %d, want 1", count)
	}
}

func TestSettingRoundTripAndSeparateKeys(t *testing.T) {
	s, _ := accountSite(t)
	ctx := context.Background()
	settings := []struct{ key, value string }{
		{"site_name", "Example site"},
		{"Site_Name", "Different case, different key"},
		{" site_name ", "  preserve surrounding whitespace  "},
		{"custom.setting/日本語", "Café 日本語\nsecond line\r\n"},
		{"quote'; DROP TABLE site_settings; --", "O'Reilly \"quoted\"; not SQL"},
		{"optional", ""},
	}
	for _, setting := range settings {
		if err := s.SetSetting(ctx, setting.key, setting.value); err != nil {
			t.Fatalf("set %q: %v", setting.key, err)
		}
	}
	for _, setting := range settings {
		value, err := s.GetSetting(ctx, setting.key)
		if err != nil || value != setting.value {
			t.Fatalf("get %q = %q, error = %v; want %q", setting.key, value, err, setting.value)
		}
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM site_settings").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(settings) {
		t.Fatalf("settings count = %d, want %d", count, len(settings))
	}
}

func TestSettingReplacementKeepsOneRow(t *testing.T) {
	s, _ := accountSite(t)
	ctx := context.Background()
	for _, want := range []string{"original", "replacement", "replacement", ""} {
		if err := s.SetSetting(ctx, "site_name", want); err != nil {
			t.Fatal(err)
		}
		if value, err := s.GetSetting(ctx, "site_name"); err != nil || value != want {
			t.Fatalf("replacement = %q, error = %v; want %q", value, err, want)
		}
		var count int
		if err := s.db.QueryRow("SELECT count(*) FROM site_settings").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("repeated sets created %d rows, want 1", count)
		}
	}
}

func TestSettingRestartPreservesSettingsPageAndAccount(t *testing.T) {
	s, path := accountSite(t)
	ctx := context.Background()
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	pageID, err := s.CreateItem(ctx, content.ItemDraft{
		Title: "Existing Page", Path: "/existing-page", Fields: map[string]string{"body": "Existing body"},
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	accountID, err := s.CreateAccount(ctx, AccountDraft{
		Email: "admin@example.test", PasswordHash: "opaque test hash",
		IsAdministrator: true, IsEditor: true, State: "active",
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	pageBefore, err := s.GetItem(ctx, pageID)
	if err != nil {
		t.Fatal(err)
	}
	accountBefore, err := s.GetAccount(ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	configBefore, err := s.ActiveConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range []struct{ key, value string }{
		{"site_name", "Original name"}, {"custom", "Persisted value"},
		{"site_name", "Replacement name"}, {"empty", ""},
	} {
		if err := s.SetSetting(ctx, setting.key, setting.value); err != nil {
			t.Fatal(err)
		}
	}
	verify := func(s *Store) {
		t.Helper()
		for key, want := range map[string]string{
			"site_name": "Replacement name", "custom": "Persisted value", "empty": "",
		} {
			if value, err := s.GetSetting(ctx, key); err != nil || value != want {
				t.Fatalf("get %q = %q, error = %v; want %q", key, value, err, want)
			}
		}
		if page, err := s.GetItem(ctx, pageID); err != nil || !reflect.DeepEqual(page, pageBefore) {
			t.Fatalf("settings writes changed existing Page: %v", err)
		}
		if account, err := s.GetAccount(ctx, accountID); err != nil || !reflect.DeepEqual(account, accountBefore) {
			t.Fatalf("settings writes changed existing account: %v", err)
		}
		if active, err := s.ActiveConfig(ctx); err != nil || !reflect.DeepEqual(active, configBefore) {
			t.Fatalf("settings writes changed active Page configuration: %v", err)
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

func TestSettingOpenDoesNotCreateMissingTable(t *testing.T) {
	// This is a disposable fixture, not a supported database upgrade path.
	s, path := accountSite(t)
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, "DROP TABLE site_settings"); err != nil {
		t.Fatal(err)
	}
	var schemaBefore int
	if err := s.db.QueryRowContext(ctx, "PRAGMA schema_version").Scan(&schemaBefore); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Open may reject an incomplete schema, but it must never repair it.
	reopened, openErr := Open(ctx, path)
	if openErr == nil {
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	}
	db, err := connect(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count, schemaAfter int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name='site_settings'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("Open recreated the missing settings table")
	}
	if err := db.QueryRowContext(ctx, "PRAGMA schema_version").Scan(&schemaAfter); err != nil {
		t.Fatal(err)
	}
	if schemaAfter != schemaBefore {
		t.Fatalf("Open changed schema version from %d to %d", schemaBefore, schemaAfter)
	}
}
