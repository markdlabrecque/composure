package integration_test

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

// A separate fixture adds published content using the actual schema. It never
// substitutes for the CLI-driven seed assertion in the initialization tracer.
func publishedFixture(t *testing.T, db *sql.DB, path, title, body string) {
	t.Helper()
	const item = "019f0000-0000-7000-8000-000000000001"
	const snapshot = "019f0000-0000-7000-8000-000000000002"
	const at = "2026-09-29T12:00:00.000Z"
	fields, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO items(id,type_id,title,path,fields,draft_revision,created_at,created_by,updated_at,updated_by) VALUES(?,'page',?,?,?,1,?,'local-prototype',?,'local-prototype')`, []any{item, title, path, string(fields), at, at}},
		{`INSERT INTO snapshots(id,item_id,seq,type_id,config_revision,source_draft_revision,title,path,fields,published_at,published_by) VALUES(?,?,1,'page',1,1,?,?,?,?,'local-prototype')`, []any{snapshot, item, title, path, string(fields), at}},
		{`UPDATE items SET published_snapshot_id=? WHERE id=?`, []any{snapshot, item}},
		{`INSERT INTO routes(path,kind,item_id,claimed_at) VALUES(?,'item',?,?)`, []any{path, item, at}},
	}
	for _, s := range statements {
		if _, err := tx.Exec(s.sql, s.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestStoredPublishedTextIsEscaped(t *testing.T) {
	site := initSite(t, true)
	db := openDB(t, site)
	title := "<script>stored title</script>"
	body := "<script>stored body</script>\nSecond line & plain text"
	publishedFixture(t, db, "/escaped", title, body)
	db.Close()
	url, stop := serve(t, site)
	_, html := request(t, url, "GET", "/escaped", "", 200)
	for _, want := range []string{"&lt;script&gt;stored title&lt;/script&gt;", "&lt;script&gt;stored body&lt;/script&gt;\nSecond line &amp; plain text"} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered HTML lacks escaped stored value %q: %q", want, html)
		}
	}
	if strings.Contains(html, "<script>") {
		t.Error("stored plain text became executable markup")
	}
	stop()
	url, _ = serve(t, site)
	_, again := request(t, url, "GET", "/escaped", "", 200)
	if again != html {
		t.Error("persisted fixture HTML changed on restart")
	}
}

func TestPublishedFixtureDoesNotCrossSites(t *testing.T) {
	a, b := initSite(t, true), initSite(t, true)
	db := openDB(t, a)
	publishedFixture(t, db, "/only-a", "Only site A", "Independent persisted body")
	db.Close()
	ua, _ := serve(t, a)
	ub, _ := serve(t, b)
	_, html := request(t, ua, "GET", "/only-a", "", 200)
	if !strings.Contains(html, "Independent persisted body") {
		t.Error("site A did not render its stored body")
	}
	request(t, ub, "GET", "/only-a", "", 404)
}
