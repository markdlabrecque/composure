package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
	"github.com/markdlabrecque/composure/internal/site"
	"modernc.org/sqlite"
)

func TestPublishTicket9RepositoryClockAndValidationTransaction(t *testing.T) {
	path := initSite(t, false)
	db := openDB(t, path)
	ctx := context.Background()
	opened, err := site.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	var repository content.Repository = opened
	draft := content.ItemDraft{Title: "Clock <title>", Path: "/clock", Fields: map[string]string{"body": "saved\nbody"}}
	id, err := repository.CreateItem(ctx, draft, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	// Noncanonical JSON spacing proves the callback receives persisted bytes,
	// and the snapshot copies those bytes rather than dropping stored values.
	const rawFields = `{ "body" : "saved\nbody" }`
	if _, err := db.Exec("UPDATE items SET fields=? WHERE id=?", rawFields, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE active_config SET revision=19"); err != nil {
		t.Fatal(err)
	}
	configJSON := scalar[string](t, db, "SELECT document FROM active_config")
	if _, err := db.Exec("PRAGMA busy_timeout=0"); err != nil {
		t.Fatal(err)
	}
	before := admin6State(t, db)
	calls := 0
	check := func(active content.ActiveConfig, stored content.StoredDraft) {
		calls++
		if active.Revision != 19 || string(active.Document) != configJSON {
			t.Errorf("callback did not receive current stored config: %+v", active)
		}
		if stored.Title != draft.Title || stored.Path != draft.Path || string(stored.FieldsJSON) != rawFields {
			t.Errorf("callback did not receive raw persisted draft: %+v", stored)
		}
		// WAL reads work while the publication holds its BEGIN IMMEDIATE lock.
		if scalar[int](t, db, "SELECT count(*) FROM snapshots") != 0 || scalar[int](t, db, "SELECT count(*) FROM routes") != 0 {
			t.Error("validation ran after publication writes")
		}
		// Another connection cannot write while callback validation runs. SQLite's
		// zero busy timeout makes this deterministic, without timers or sleeps.
		_, lockErr := db.Exec("UPDATE active_config SET revision=20")
		var sqliteErr *sqlite.Error
		if !errors.As(lockErr, &sqliteErr) || sqliteErr.Code() != 5 {
			t.Errorf("validation must hold SQLite write transaction, competing write returned %v", lockErr)
		}
	}
	at := time.Date(2026, 9, 29, 23, 45, 12, 789123456, time.FixedZone("west", -7*60*60))
	_, err = repository.Publish(ctx, id, 1, at, "local-prototype", func(active content.ActiveConfig, stored content.StoredDraft) (content.ItemDraft, []content.FieldError) {
		check(active, stored)
		return content.ItemDraft{}, []content.FieldError{{Field: "body", Code: "required"}}
	})
	var invalid *content.ValidationError
	if !errors.As(err, &invalid) {
		t.Errorf("callback rejection=%v, want typed ValidationError", err)
	} else if !reflect.DeepEqual(invalid.Problems, []content.FieldError{{Field: "body", Code: "required"}}) {
		t.Errorf("validation discarded callback field errors: %+v", invalid.Problems)
	}
	if calls != 1 {
		t.Errorf("validation callback calls=%d, want 1", calls)
	}
	admin6Unchanged(t, db, before)
	calls = 0
	snapshot, err := repository.Publish(ctx, id, 1, at, "local-prototype", func(active content.ActiveConfig, stored content.StoredDraft) (content.ItemDraft, []content.FieldError) {
		check(active, stored)
		var fields map[string]string
		if err := json.Unmarshal(stored.FieldsJSON, &fields); err != nil {
			t.Fatal(err)
		}
		return content.ItemDraft{Title: stored.Title, Path: stored.Path, Fields: fields}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("successful validation callback calls=%d, want 1", calls)
	}
	sid := admin9Snapshot(t, db, id, draft.Title, draft.Path, rawFields, 1, 19)
	wantTime := at.UTC().Format("2006-01-02T15:04:05.000Z")
	if got := scalar[string](t, db, "SELECT published_at FROM snapshots WHERE id='"+sid+"'"); got != wantTime {
		t.Errorf("published_at=%s, want injected UTC millisecond %s", got, wantTime)
	}
	if got := scalar[string](t, db, "SELECT claimed_at FROM routes WHERE item_id='"+id+"'"); got != wantTime {
		t.Errorf("claimed_at=%s, want same injected time %s", got, wantTime)
	}
	if prefix := strings.ReplaceAll(sid, "-", "")[:12]; prefix != fmt.Sprintf("%012x", at.UnixMilli()) {
		t.Errorf("UUIDv7 time prefix=%s, want injected Unix milliseconds", prefix)
	}
	if snapshot.ID != sid || snapshot.ItemID != id || snapshot.Title != draft.Title || snapshot.Path != draft.Path || !reflect.DeepEqual(snapshot.Fields, draft.Fields) {
		t.Errorf("Publish returned wrong snapshot: %+v", snapshot)
	}
	if scalar[int](t, db, "SELECT revision FROM active_config") != 19 {
		t.Error("callback competing write escaped transaction lock")
	}
}
