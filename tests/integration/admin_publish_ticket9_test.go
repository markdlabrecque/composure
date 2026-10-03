package integration_test

import (
	"database/sql"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func admin9Publish(t *testing.T, base, id string, revision int, want int) (http.Header, string) {
	t.Helper()
	return admin6HTTP(t, base, "POST", "/admin/pages/"+id+"/publish", url.Values{"draft_revision": {fmt.Sprint(revision)}}.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, want)
}

func admin9Code(t *testing.T, page, code string) {
	t.Helper()
	if !strings.Contains(page, `data-error-code="`+code+`"`) {
		t.Errorf("missing error code %s in %.600s", code, page)
	}
}

func admin9Snapshot(t *testing.T, db *sql.DB, id, title, path, fields string, revision, configRevision int, expectedActors ...string) string {
	t.Helper()
	expectedActor := "local-prototype"
	if len(expectedActors) == 1 {
		expectedActor = expectedActors[0]
	}
	var sid, typ, gotTitle, gotPath, gotFields, at, actor, claimed, owner, pointer string
	var seq, source, cfg int
	if err := db.QueryRow(`SELECT s.id,s.type_id,s.title,s.path,s.fields,s.seq,s.source_draft_revision,s.config_revision,s.published_at,s.published_by,r.claimed_at,r.item_id,i.published_snapshot_id FROM snapshots s JOIN items i ON i.id=s.item_id JOIN routes r ON r.item_id=i.id WHERE i.id=?`, id).Scan(&sid, &typ, &gotTitle, &gotPath, &gotFields, &seq, &source, &cfg, &at, &actor, &claimed, &owner, &pointer); err != nil {
		t.Fatalf("publication did not atomically persist snapshot/route/pointer: %v", err)
	}
	if !uuid7.MatchString(sid) || sid == id || typ != "page" || seq != 1 || source != revision || cfg != configRevision || actor != expectedActor || !timestamp.MatchString(at) || claimed != at || owner != id || pointer != sid {
		t.Errorf("wrong publication metadata: snapshot=%s type=%s seq=%d source=%d config=%d at=%s actor=%s claimed=%s owner=%s pointer=%s", sid, typ, seq, source, cfg, at, actor, claimed, owner, pointer)
	}
	if _, err := time.Parse("2006-01-02T15:04:05.000Z", at); err != nil {
		t.Errorf("invalid UTC millisecond publication time: %v", err)
	}
	if gotTitle != title || gotPath != path {
		t.Errorf("snapshot title/path=%q/%q, want %q/%q", gotTitle, gotPath, title, path)
	}
	equalJSON(t, gotFields, []byte(fields))
	if original := scalar[string](t, db, "SELECT fields FROM items WHERE id='"+id+"'"); gotFields != original {
		t.Error("snapshot fields are not an exact copy of stored draft JSON")
	}
	if n := scalar[int](t, db, "SELECT count(*) FROM snapshots WHERE item_id='"+id+"'"); n != 1 {
		t.Errorf("snapshot count=%d, want 1", n)
	}
	return sid
}

func TestPhase1PublishPage(t *testing.T) {
	site := initSite(t, false)
	db := openDB(t, site)
	base, stop := serve(t, site)
	accountID := adminFixtureAccountID(t, base)
	title := `Published <script>title & "quote"</script>`
	body := "\nPublished <script>body</script>\nSecond & line"
	id := admin7Create(t, base, "Initial", " About-Us/ ")
	v := url.Values{"title": {title}, "path": {"/about-us"}, "body": {body}, "draft_revision": {"1"}}
	admin7Post(t, base, id, v, 303)
	_, preview := admin6HTTP(t, base, "GET", "/admin/pages/"+id+"/preview", "", nil, 200)
	if _, err := db.Exec("UPDATE active_config SET revision=7"); err != nil {
		t.Fatal(err)
	}
	before := admin6State(t, db)
	draftQuery := "SELECT json_array(type_id,title,path,fields,draft_revision,created_at,created_by,updated_at,updated_by) FROM items WHERE id='" + id + "'"
	draftBefore := scalar[string](t, db, draftQuery)
	_, edit := admin6HTTP(t, base, "GET", "/admin/pages/"+id+"/edit", "", nil, 200)
	found := false
	for _, tag := range admin6TagRE.FindAllString(edit, -1) {
		a := admin6Attrs(tag)
		if strings.HasPrefix(strings.ToLower(tag), "<form") && a["action"] == "/admin/pages/"+id+"/publish" && strings.EqualFold(a["method"], "post") {
			found = true
		}
	}
	if !found {
		t.Error("edit lacks publish POST form for saved draft")
	}
	// Unsaved input and system metadata must never replace the stored draft.
	spoof := url.Values{"draft_revision": {"2"}, "title": {"UNSAVED"}, "body": {"UNSAVED"}, "path": {"/stolen"}, "published_by": {"attacker"}, "published_at": {"attacker"}, "id": {"attacker"}, "config_revision": {"999"}}
	h, _ := admin6HTTP(t, base, "POST", "/admin/pages/"+id+"/publish", spoof.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, 303)
	location := h.Get("Location")
	if !strings.HasPrefix(location, "/admin/pages/"+id+"/edit") {
		t.Errorf("publish redirect=%q", location)
	}
	_, notice := admin6HTTP(t, base, "GET", location, "", nil, 200)
	if !strings.Contains(strings.ToLower(admin8Text(notice)), "published") {
		t.Error("successful publication has no edit notice")
	}
	sid := admin9Snapshot(t, db, id, title, "/about-us", `{"body":"\nPublished <script>body</script>\nSecond & line"}`, 2, 7, accountID)
	after := admin6State(t, db)
	if scalar[string](t, db, draftQuery) != draftBefore {
		t.Error("publication changed saved draft or its metadata")
	}
	for _, table := range []string{"site", "active_config", "sqlite_master"} {
		if after[table] != before[table] {
			t.Errorf("publish changed %s", table)
		}
	}
	if scalar[int](t, db, "SELECT draft_revision FROM items WHERE id='"+id+"'") != 2 {
		t.Error("publish changed draft revision")
	}
	_, public := admin6HTTP(t, base, "GET", "/about-us", "", nil, 200)
	if admin8Content(t, public) != admin8Content(t, preview) {
		t.Error("published content differs from exact preview content region")
	}
	if strings.Contains(public, "<script>") || !strings.Contains(public, html.EscapeString(title)) || !strings.Contains(public, "&lt;script&gt;body&lt;/script&gt;") {
		t.Error("public title/body escaping changed")
	}
	for _, p := range []string{"/About-Us", "/about-us/", "/stolen", "/about%2dus"} {
		admin6HTTP(t, base, "GET", p, "", nil, 404)
	}
	for _, q := range []string{"UPDATE snapshots SET title='mutated' WHERE id=?", "DELETE FROM snapshots WHERE id=?"} {
		if _, err := db.Exec(q, sid); err == nil {
			t.Error("published snapshot immutability trigger missing")
		}
	}
	admin6Unchanged(t, db, after)
	stop()
	base, _ = serve(t, site)
	_, again := admin6HTTP(t, base, "GET", "/about-us", "", nil, 200)
	if again != public {
		t.Error("published content changed after restart")
	}
	// Subsequent draft saves and previews remain isolated from the first snapshot.
	v.Set("title", "Private new title")
	v.Set("body", "Private new body")
	v.Set("draft_revision", "2")
	admin7Post(t, base, id, v, 303)
	_, newPreview := admin6HTTP(t, base, "GET", "/admin/pages/"+id+"/preview", "", nil, 200)
	if !strings.Contains(newPreview, "Private new body") {
		t.Error("preview did not use subsequently saved draft")
	}
	_, still := admin6HTTP(t, base, "GET", "/about-us", "", nil, 200)
	if still != public || admin6State(t, db)["snapshots"] != after["snapshots"] || admin6State(t, db)["routes"] != after["routes"] {
		t.Error("draft save changed published snapshot or route")
	}
}

func TestPublishTicket9StoredDraftRevalidation(t *testing.T) {
	for _, tc := range []struct{ name, sql, value, code string }{
		{"empty_title", "UPDATE items SET title=? WHERE id=?", "", "required"},
		{"invalid_title", "UPDATE items SET title=? WHERE id=?", "bad\x01title", "invalid_text"},
		{"reserved", "UPDATE items SET path=? WHERE id=?", " ADMIN/child/ ", "path_reserved"},
		{"invalid_path", "UPDATE items SET path=? WHERE id=?", "/bad//path", "path_invalid"},
		{"too_long_body", "UPDATE items SET fields=json_object('body',?) WHERE id=?", strings.Repeat("x", 100001), "too_long"},
		{"unknown_field", "UPDATE items SET fields=? WHERE id=?", `{"body":"saved","unconfigured":"must not be dropped"}`, ""},
		{"nonstring_field", "UPDATE items SET fields=? WHERE id=?", `{"body":42}`, ""},
		{"null_field", "UPDATE items SET fields=? WHERE id=?", `{"body":null}`, ""},
		{"fields_not_object", "UPDATE items SET fields=? WHERE id=?", `[]`, ""},
		{"fields_null", "UPDATE items SET fields=? WHERE id=?", `null`, ""},
		{"unknown_type", "UPDATE items SET type_id=? WHERE id=?", "other", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			site := initSite(t, true)
			db := openDB(t, site)
			base, _ := serve(t, site)
			id := admin7Create(t, base, "Saved", "/private")
			if _, err := db.Exec(tc.sql, tc.value, id); err != nil {
				t.Fatal(err)
			}
			before := admin6State(t, db)
			_, old := admin6HTTP(t, base, "GET", "/example", "", nil, 200)
			_, page := admin9Publish(t, base, id, 1, 422)
			if tc.code != "" {
				admin9Code(t, page, tc.code)
			}
			admin6Unchanged(t, db, before)
			_, public := admin6HTTP(t, base, "GET", "/example", "", nil, 200)
			if public != old {
				t.Error("invalid publication changed previous owner")
			}
			admin6HTTP(t, base, "GET", "/private", "", nil, 404)
		})
	}
	t.Run("current_config_required", func(t *testing.T) {
		site := initSite(t, false)
		db := openDB(t, site)
		base, _ := serve(t, site)
		id := admin7Create(t, base, "Saved", "/private")
		if _, err := db.Exec("UPDATE items SET fields='{\"body\":\"\"}' WHERE id=?", id); err != nil {
			t.Fatal(err)
		}
		admin6Custom(t, db)
		before := admin6State(t, db)
		_, page := admin9Publish(t, base, id, 1, 422)
		admin9Code(t, page, "required")
		admin6Unchanged(t, db, before)
	})
	t.Run("published_path_change", func(t *testing.T) {
		site := initSite(t, true)
		db := openDB(t, site)
		base, _ := serve(t, site)
		id := scalar[string](t, db, "SELECT id FROM items")
		if _, err := db.Exec("UPDATE items SET path='/changed' WHERE id=?", id); err != nil {
			t.Fatal(err)
		}
		before := admin6State(t, db)
		_, page := admin9Publish(t, base, id, 1, 422)
		admin9Code(t, page, "path_change_unsupported")
		admin6Unchanged(t, db, before)
		admin6HTTP(t, base, "GET", "/example", "", nil, 200)
		admin6HTTP(t, base, "GET", "/changed", "", nil, 404)
	})
}

func TestPublishTicket9StaleUnknownAndConflict(t *testing.T) {
	site := initSite(t, false)
	db := openDB(t, site)
	base, _ := serve(t, site)
	title := `Owner <script> & "title"</script>`
	owner := admin7Create(t, base, title, "About-Us/")
	loser := admin7Create(t, base, "Losing saved draft", "/about-us/")
	before := admin6State(t, db)
	admin9Publish(t, base, "019f0000-0000-7000-8000-000000000099", 1, 404)
	admin6Unchanged(t, db, before)
	admin7Post(t, base, loser, url.Values{"title": {"Losing <em>draft</em>"}, "path": {"/about-us"}, "body": {"saved <script>body</script>"}, "draft_revision": {"1"}}, 303)
	before = admin6State(t, db)
	_, page := admin9Publish(t, base, loser, 1, 409)
	admin9Code(t, page, "stale_draft")
	admin6Unchanged(t, db, before)
	admin9Publish(t, base, owner, 1, 303)
	// Simulate a legacy stored alias. Ownership must check its normalized path.
	if _, err := db.Exec("UPDATE items SET path=' About-Us/ ' WHERE id=?", loser); err != nil {
		t.Fatal(err)
	}
	before = admin6State(t, db)
	_, page = admin9Publish(t, base, loser, 2, 409)
	admin9Code(t, page, "path_taken")
	admin6Link(t, page, "/admin/pages/"+owner+"/edit")
	if !strings.Contains(page, html.EscapeString(title)) || strings.Contains(page, "<script>") || !strings.Contains(page, "&lt;em&gt;draft&lt;/em&gt;") || !strings.Contains(page, "&lt;script&gt;body&lt;/script&gt;") {
		t.Error("conflict omitted/failed to escape owner title or saved draft")
	}
	admin6Unchanged(t, db, before)
	admin6HTTP(t, base, "GET", "/about-us", "", nil, 200)
}

func TestPublishTicket9FailureRollsBackAndRetries(t *testing.T) {
	site := initSite(t, true)
	db := openDB(t, site)
	base, _ := serve(t, site)
	accountID := adminFixtureAccountID(t, base)
	id := admin7Create(t, base, "Saved", "/rollback")
	// This real SQLite trigger fails route insertion only after a snapshot exists
	// inside the transaction. The error must unwind that snapshot and the pointer.
	if _, err := db.Exec(`CREATE TRIGGER ticket9_fail_route BEFORE INSERT ON routes WHEN NEW.path='/rollback' AND EXISTS(SELECT 1 FROM snapshots WHERE item_id=NEW.item_id) BEGIN SELECT RAISE(ABORT,'ticket9 failure after snapshot insert'); END`); err != nil {
		t.Fatal(err)
	}
	before := admin6State(t, db)
	_, old := admin6HTTP(t, base, "GET", "/example", "", nil, 200)
	admin9Publish(t, base, id, 1, 500)
	admin6Unchanged(t, db, before)
	if n := scalar[int](t, db, "SELECT count(*) FROM snapshots WHERE item_id='"+id+"'"); n != 0 {
		t.Errorf("failed publication left %d snapshots", n)
	}
	admin6HTTP(t, base, "GET", "/rollback", "", nil, 404)
	_, still := admin6HTTP(t, base, "GET", "/example", "", nil, 200)
	if old != still {
		t.Error("failed publication changed prior public content")
	}
	if _, err := db.Exec("DROP TRIGGER ticket9_fail_route"); err != nil {
		t.Fatal(err)
	}
	admin9Publish(t, base, id, 1, 303)
	admin9Snapshot(t, db, id, "Saved", "/rollback", `{"body":"Original body"}`, 1, 1, accountID)
	admin6HTTP(t, base, "GET", "/rollback", "", nil, 200)
}

func TestPublishTicket9HTTPBoundary(t *testing.T) {
	site := initSite(t, false)
	db := openDB(t, site)
	base, _ := serve(t, site)
	id := admin7Create(t, base, "Saved", "/private")
	path := "/admin/pages/" + id + "/publish"
	before := admin6State(t, db)
	for _, method := range []string{"GET", "HEAD", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		t.Run(method, func(t *testing.T) {
			h, body := admin6HTTP(t, base, method, path, "draft_revision=1", nil, 405)
			if h.Get("Allow") != "POST" {
				t.Errorf("Allow=%q, want POST", h.Get("Allow"))
			}
			if method == "HEAD" && body != "" {
				t.Error("HEAD returned body")
			}
			admin6Unchanged(t, db, before)
		})
	}
	for _, tc := range []struct {
		name, body string
		headers    map[string]string
		status     int
	}{
		{"host", "draft_revision=1", map[string]string{"Host": "attacker.example", "Content-Type": "application/x-www-form-urlencoded"}, 400},
		{"origin", "draft_revision=1", map[string]string{"Origin": "https://attacker.example", "Content-Type": "application/x-www-form-urlencoded"}, 403},
		{"fetch_site", "draft_revision=1", map[string]string{"Sec-Fetch-Site": "cross-site", "Content-Type": "application/x-www-form-urlencoded"}, 403},
		{"media", "{}", map[string]string{"Content-Type": "application/json"}, 415},
		{"oversized", "draft_revision=1&padding=" + strings.Repeat("x", 1<<20), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			admin6HTTP(t, base, "POST", path, tc.body, tc.headers, tc.status)
			admin6Unchanged(t, db, before)
		})
	}
	for _, revision := range []string{"", "0", "-1", "garbage", "999999999999999999999999"} {
		t.Run("invalid_revision_"+revision, func(t *testing.T) {
			admin6HTTP(t, base, "POST", path, url.Values{"draft_revision": {revision}}.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, 422)
			admin6Unchanged(t, db, before)
		})
	}
}

func TestPublishTicket9ConcurrentSamePath(t *testing.T) {
	site := initSite(t, false)
	db := openDB(t, site)
	base, _ := serve(t, site)
	ids := []string{admin7Create(t, base, "First", "Race/"), admin7Create(t, base, "Second", "/race/")}
	type result struct {
		id     string
		status int
		body   string
		err    error
	}
	out := make(chan result, 2)
	start := make(chan struct{})
	for _, id := range ids {
		go func(id string) {
			<-start
			body := url.Values{"draft_revision": {"1"}, "csrf_token": {adminFixtureCSRFToken(t, base)}}.Encode()
			req, err := http.NewRequest("POST", base+"/admin/pages/"+id+"/publish", strings.NewReader(body))
			if err != nil {
				out <- result{id: id, err: err}
				return
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Cookie", adminFixtureCookie(t, base))
			c := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			r, err := c.Do(req)
			if err != nil {
				out <- result{id: id, err: err}
				return
			}
			b, err := io.ReadAll(r.Body)
			r.Body.Close()
			out <- result{id: id, status: r.StatusCode, body: string(b), err: err}
		}(id)
	}
	close(start)
	results := []result{<-out, <-out}
	var winner, loser string
	for _, r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		switch r.status {
		case 303:
			winner = r.id
		case 409:
			loser = r.id
			admin9Code(t, r.body, "path_taken")
		default:
			t.Errorf("concurrent publication %s returned %d, want 303 or 409", r.id, r.status)
		}
	}
	if winner == "" || loser == "" {
		t.Fatalf("expected one winner and one conflict, got %+v", results)
	}
	if scalar[int](t, db, "SELECT count(*) FROM snapshots") != 1 || scalar[int](t, db, "SELECT count(*) FROM routes") != 1 || scalar[string](t, db, "SELECT item_id FROM routes WHERE path='/race'") != winner || scalar[int](t, db, "SELECT count(*) FROM items WHERE id='"+loser+"' AND published_snapshot_id IS NULL AND draft_revision=1") != 1 {
		t.Error("concurrent publication left partial or wrong ownership")
	}
	for _, r := range results {
		if r.id == loser {
			admin6Link(t, r.body, "/admin/pages/"+winner+"/edit")
		}
	}
	admin6HTTP(t, base, "GET", "/race", "", nil, 200)
}
