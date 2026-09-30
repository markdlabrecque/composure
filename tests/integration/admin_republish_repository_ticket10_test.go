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

func TestRepublishTicket10StoredValidationLockAndClock(t *testing.T) {
	path := initSite(t, true)
	db := openDB(t, path)
	ctx := context.Background()
	opened, err := site.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	var repository content.Repository = opened
	id := scalar[string](t, db, "SELECT id FROM items WHERE path='/example'")
	a := admin10SnapshotRow(t, db, id, 1)
	const raw = `{ "body" : "stored\nrepublish & body" }`
	if _, err := db.Exec("UPDATE items SET title='Stored republish',fields=?,draft_revision=2 WHERE id=?", raw, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE active_config SET revision=19"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=0"); err != nil {
		t.Fatal(err)
	}
	configJSON := scalar[string](t, db, "SELECT document FROM active_config")
	before := admin6State(t, db)
	draft := admin10DraftRow(t, db, id)
	calls := 0
	validate := func(active content.ActiveConfig, stored content.StoredDraft) (content.ItemDraft, []content.FieldError) {
		calls++
		if active.Revision != 19 || string(active.Document) != configJSON || stored.Title != "Stored republish" || stored.Path != "/example" || string(stored.FieldsJSON) != raw {
			t.Errorf("republish validation did not receive stored draft/current config: %+v %+v", active, stored)
		}
		if scalar[int](t, db, "SELECT count(*) FROM snapshots") != 1 {
			t.Error("validation ran after snapshot writes")
		}
		_, lockErr := db.Exec("UPDATE active_config SET revision=20")
		var sqliteErr *sqlite.Error
		if !errors.As(lockErr, &sqliteErr) || sqliteErr.Code() != 5 {
			t.Errorf("republish validation lacks write lock: %v", lockErr)
		}
		var fields map[string]string
		if err := json.Unmarshal(stored.FieldsJSON, &fields); err != nil {
			t.Fatal(err)
		}
		return content.ItemDraft{Title: stored.Title, Path: stored.Path, Fields: fields}, nil
	}
	at := time.Date(2026, 9, 30, 23, 45, 12, 789123456, time.FixedZone("west", -7*60*60))
	_, err = repository.Publish(ctx, id, 2, at, "local-prototype", func(active content.ActiveConfig, stored content.StoredDraft) (content.ItemDraft, []content.FieldError) {
		validate(active, stored)
		return content.ItemDraft{}, []content.FieldError{{Field: "body", Code: "required"}}
	})
	var invalid *content.ValidationError
	if !errors.As(err, &invalid) || !reflect.DeepEqual(invalid.Problems, []content.FieldError{{Field: "body", Code: "required"}}) {
		t.Errorf("republish callback rejection=%v", err)
	}
	if calls != 1 {
		t.Errorf("rejection callback calls=%d", calls)
	}
	admin6Unchanged(t, db, before)
	calls = 0
	snapshot, err := repository.Publish(ctx, id, 2, at, "local-prototype", validate)
	if err != nil {
		t.Fatalf("valid republish rejected: %v", err)
	}
	if calls != 1 {
		t.Errorf("success callback calls=%d", calls)
	}
	var sid, fields, publishedAt, actor string
	var seq, source, cfg int
	if err := db.QueryRow("SELECT id,seq,source_draft_revision,config_revision,fields,published_at,published_by FROM snapshots WHERE item_id=? AND seq=2", id).Scan(&sid, &seq, &source, &cfg, &fields, &publishedAt, &actor); err != nil {
		t.Fatal(err)
	}
	if snapshot.ID != sid || snapshot.ItemID != id || snapshot.Title != "Stored republish" || snapshot.Path != "/example" || snapshot.Fields["body"] != "stored\nrepublish & body" || fields != raw || seq != 2 || source != 2 || cfg != 19 || actor != "local-prototype" || publishedAt != at.UTC().Format("2006-01-02T15:04:05.000Z") {
		t.Errorf("wrong republish snapshot: %+v persisted=%s", snapshot, admin10SnapshotRow(t, db, id, 2))
	}
	if !uuid7.MatchString(sid) {
		t.Fatalf("snapshot ID is not UUIDv7: %q", sid)
	}
	if prefix := strings.ReplaceAll(sid, "-", "")[:12]; prefix != fmt.Sprintf("%012x", at.UnixMilli()) {
		t.Error("republish ID ignores injected clock")
	}
	if admin10SnapshotRow(t, db, id, 1) != a || admin10DraftRow(t, db, id) != draft {
		t.Error("republish mutated history or draft metadata")
	}
	after := admin6State(t, db)
	for _, table := range []string{"routes", "active_config", "site", "sqlite_master"} {
		if after[table] != before[table] {
			t.Errorf("republish changed %s", table)
		}
	}
	if scalar[string](t, db, "SELECT published_snapshot_id FROM items WHERE id='"+id+"'") != sid {
		t.Error("republish pointer was not advanced")
	}
	// Mutating returned fields must not affect either persisted version.
	snapshot.Fields["body"] = "mutated returned map"
	admin6Unchanged(t, db, after)
}
