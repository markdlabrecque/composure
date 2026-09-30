package integration_test

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Include every persisted snapshot column, preserving the JSON string's bytes.
func admin10SnapshotRow(t *testing.T, db *sql.DB, id string, seq int) string {
	t.Helper()
	return scalar[string](t, db, fmt.Sprintf(`SELECT json_array(id,item_id,seq,type_id,config_revision,source_draft_revision,title,path,fields,published_at,published_by) FROM snapshots WHERE item_id='%s' AND seq=%d`, id, seq))
}

func admin10DraftRow(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	return scalar[string](t, db, `SELECT json_array(id,type_id,title,path,fields,draft_revision,created_at,created_by,updated_at,updated_by) FROM items WHERE id='`+id+`'`)
}

func admin10Snapshot(t *testing.T, db *sql.DB, id string, seq, revision, configRevision int, title, fields string) string {
	t.Helper()
	var sid, item, typ, gotTitle, path, gotFields, at, actor string
	var gotSeq, source, cfg int
	err := db.QueryRow(`SELECT id,item_id,seq,type_id,config_revision,source_draft_revision,title,path,fields,published_at,published_by FROM snapshots WHERE item_id=? AND seq=?`, id, seq).Scan(&sid, &item, &gotSeq, &typ, &cfg, &source, &gotTitle, &path, &gotFields, &at, &actor)
	if err != nil {
		t.Fatalf("missing republish snapshot seq %d: %v", seq, err)
	}
	if !uuid7.MatchString(sid) || sid == id || item != id || typ != "page" || gotSeq != seq || source != revision || cfg != configRevision || actor != "local-prototype" || !timestamp.MatchString(at) {
		t.Errorf("wrong snapshot metadata: %s", admin10SnapshotRow(t, db, id, seq))
	}
	if _, err := time.Parse("2006-01-02T15:04:05.000Z", at); err != nil {
		t.Error(err)
	}
	if gotTitle != title || path != "/republish" || gotFields != fields {
		t.Errorf("snapshot values=%q/%q/%q, want %q /republish %q", gotTitle, path, gotFields, title, fields)
	}
	if scalar[string](t, db, "SELECT published_snapshot_id FROM items WHERE id='"+id+"'") != sid {
		t.Error("pointer does not select newly committed snapshot")
	}
	if scalar[int](t, db, "SELECT count(*) FROM snapshots WHERE item_id='"+id+"'") != seq {
		t.Error("wrong per-item snapshot count")
	}
	return sid
}

func TestPhase1RepublishIsolation(t *testing.T) {
	site := initSite(t, false)
	db := openDB(t, site)
	base, stop := serve(t, site)
	id := admin7Create(t, base, "A <script>title</script>", "/republish")
	other := admin7Create(t, base, "Other published Page", "/other")
	admin9Publish(t, base, id, 1, 303)
	admin9Publish(t, base, other, 1, 303)
	a := admin10SnapshotRow(t, db, id, 1)
	original := admin6State(t, db)
	otherRow := admin7Row(t, db, other)
	otherSnapshot := admin10SnapshotRow(t, db, other, 1)
	_, publicA := admin6HTTP(t, base, "GET", "/republish", "", nil, 200)
	_, publicOther := admin6HTTP(t, base, "GET", "/other", "", nil, 200)
	_, previewA := admin6HTTP(t, base, "GET", "/admin/pages/"+id+"/preview", "", nil, 200)
	admin8Parity(t, previewA, "A <script>title</script>", "Original body")
	if admin8Content(t, publicA) != admin8Content(t, previewA) {
		t.Error("first publication differs from saved A")
	}

	titleB := `B <em>private & title</em>`
	bodyB := "B <script>private</script>\nNext & line"
	for i, body := range []string{"Intermediate private body", bodyB} {
		before := admin6State(t, db)
		admin7Post(t, base, id, url.Values{"title": {titleB}, "path": {"/republish"}, "body": {body}, "draft_revision": {fmt.Sprint(i + 1)}}, 303)
		admin7NoHistory(t, db, before)
		if scalar[int](t, db, "SELECT draft_revision FROM items WHERE id='"+id+"'") != i+2 {
			t.Error("changing draft save did not advance revision")
		}
		_, public := admin6HTTP(t, base, "GET", "/republish", "", nil, 200)
		if public != publicA {
			t.Error("draft save exposed private values")
		}
	}
	// A same-value save must leave the complete draft row and history unchanged.
	before := admin6State(t, db)
	admin7Post(t, base, id, url.Values{"title": {titleB}, "path": {"/republish"}, "body": {bodyB}, "draft_revision": {"3"}}, 303)
	admin6Unchanged(t, db, before)
	const rawB = `{ "body" : "B <script>private</script>\nNext & line" }`
	if _, err := db.Exec("UPDATE items SET fields=? WHERE id=?", rawB, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE active_config SET revision=7"); err != nil {
		t.Fatal(err)
	}
	draftB := admin10DraftRow(t, db, id)
	check := func(wantPublic string) {
		t.Helper()
		_, preview := admin6HTTP(t, base, "GET", "/admin/pages/"+id+"/preview", "", nil, 200)
		admin8Parity(t, preview, titleB, bodyB)
		_, public := admin6HTTP(t, base, "GET", "/republish", "", nil, 200)
		if public != wantPublic {
			t.Error("public content changed outside successful publication")
		}
		_, otherPublic := admin6HTTP(t, base, "GET", "/other", "", nil, 200)
		if otherPublic != publicOther || admin7Row(t, db, other) != otherRow || admin10SnapshotRow(t, db, other, 1) != otherSnapshot {
			t.Error("operation changed second item")
		}
		if admin10SnapshotRow(t, db, id, 1) != a {
			t.Error("snapshot A was mutated")
		}
		if admin6State(t, db)["routes"] != original["routes"] {
			t.Error("owned routes changed")
		}
		if admin10DraftRow(t, db, id) != draftB {
			t.Error("publication changed draft revision or metadata")
		}
	}
	stop()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openDB(t, site)
	base, stop = serve(t, site)
	check(publicA)

	// Fail only after a new snapshot was inserted, before changing this pointer.
	trigger := fmt.Sprintf(`CREATE TRIGGER ticket10_fail_pointer BEFORE UPDATE OF published_snapshot_id ON items WHEN NEW.id='%s' AND NEW.published_snapshot_id<>OLD.published_snapshot_id AND EXISTS(SELECT 1 FROM snapshots WHERE item_id=NEW.id AND seq=2) BEGIN SELECT RAISE(ABORT,'ticket10 failure after snapshot insert'); END`, id)
	if _, err := db.Exec(trigger); err != nil {
		t.Fatal(err)
	}
	before = admin6State(t, db)
	admin9Publish(t, base, id, 3, 500)
	admin6Unchanged(t, db, before)
	check(publicA)
	if _, err := db.Exec("DROP TRIGGER ticket10_fail_pointer"); err != nil {
		t.Fatal(err)
	}
	// A stale form must not publish B or alter the retained A pointer.
	before = admin6State(t, db)
	_, stale := admin9Publish(t, base, id, 2, 409)
	admin9Code(t, stale, "stale_draft")
	admin6Unchanged(t, db, before)

	admin9Publish(t, base, id, 3, 303)
	sidB := admin10Snapshot(t, db, id, 2, 3, 7, titleB, rawB)
	b := admin10SnapshotRow(t, db, id, 2)
	_, publicB := admin6HTTP(t, base, "GET", "/republish", "", nil, 200)
	_, previewB := admin6HTTP(t, base, "GET", "/admin/pages/"+id+"/preview", "", nil, 200)
	if publicB == publicA || admin8Content(t, publicB) != admin8Content(t, previewB) {
		t.Error("republish did not expose saved B")
	}
	check(publicB)
	before = admin6State(t, db)
	stop()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openDB(t, site)
	base, _ = serve(t, site)
	admin6Unchanged(t, db, before)
	check(publicB)
	if admin10SnapshotRow(t, db, id, 2) != b {
		t.Error("restart changed snapshot B")
	}
	admin9Publish(t, base, id, 3, 303)
	sidC := admin10Snapshot(t, db, id, 3, 3, 7, titleB, rawB)
	if sidC == sidB {
		t.Error("unchanged publication reused snapshot ID")
	}
	if admin10SnapshotRow(t, db, id, 2) != b {
		t.Error("unchanged publication changed snapshot B")
	}
	check(publicB)
	// Existing immutable-history protection applies to later snapshots too.
	before = admin6State(t, db)
	for _, q := range []string{"UPDATE snapshots SET fields='{}' WHERE id=?", "DELETE FROM snapshots WHERE id=?"} {
		if _, err := db.Exec(q, sidB); err == nil {
			t.Error("republished snapshot can be mutated")
		}
	}
	admin6Unchanged(t, db, before)
	entered := url.Values{"title": {"Entered <script>title</script>"}, "path": {"/changed"}, "body": {"Entered <em>body</em>\nkeep it"}, "draft_revision": {"3"}}
	_, page := admin7Post(t, base, id, entered, 422)
	admin9Code(t, page, "path_change_unsupported")
	admin6Retains(t, page, entered)
	admin7Revision(t, page, "3")
	text := strings.ToLower(admin8Text(page))
	if !strings.Contains(text, "redirect") || !strings.Contains(text, "phase 5") {
		t.Error("published-path rejection must explain later redirect support")
	}
	admin6Unchanged(t, db, before)
	admin6HTTP(t, base, "GET", "/changed", "", nil, 404)
	check(publicB)
}
