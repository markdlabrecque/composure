package integration_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func admin7Post(t *testing.T, base, id string, values url.Values, want int) (http.Header, string) {
	t.Helper()
	return admin6HTTP(t, base, "POST", "/admin/pages/"+id, values.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, want)
}
func admin7Values(revision int) url.Values {
	return url.Values{"title": {"Keep <script>title & text</script>"}, "path": {" /kept/ "}, "body": {"\nKeep <script>body</script>\r\nlast"}, "strapline": {"Keep & intro"}, "draft_revision": {fmt.Sprint(revision)}}
}
func admin7Revision(t *testing.T, page, want string) {
	t.Helper()
	for _, tag := range admin6TagRE.FindAllString(page, -1) {
		a := admin6Attrs(tag)
		if a["name"] == "draft_revision" {
			if a["type"] != "hidden" || a["value"] != want {
				t.Errorf("expected submitted revision %q in hidden control, got %s", want, tag)
			}
			return
		}
	}
	t.Error("missing draft_revision hidden control")
}
func admin7Form(t *testing.T, page, id string) {
	t.Helper()
	for _, tag := range admin6TagRE.FindAllString(page, -1) {
		a := admin6Attrs(tag)
		if strings.HasPrefix(strings.ToLower(tag), "<form") && strings.EqualFold(a["method"], "post") && a["action"] == "/admin/pages/"+id {
			return
		}
	}
	t.Error("missing edit POST form selecting the URL item")
}
func admin7Create(t *testing.T, base, title, path string) string {
	t.Helper()
	h, _ := admin6Post(t, base, url.Values{"title": {title}, "path": {path}, "body": {"Original body"}, "strapline": {"Original intro"}}, 303)
	location := h.Get("Location")
	if !strings.HasPrefix(location, "/admin/pages/") || !strings.HasSuffix(location, "/edit") {
		t.Fatalf("create redirect=%q", location)
	}
	return strings.TrimSuffix(strings.TrimPrefix(location, "/admin/pages/"), "/edit")
}

// Capture every item column, including actor and publication metadata absent from Item.
func admin7Row(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	rows, err := db.Query("SELECT * FROM items WHERE id=?", id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal("missing fixture item")
	}
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func admin7NoHistory(t *testing.T, db *sql.DB, before map[string]string) {
	t.Helper()
	after := admin6State(t, db)
	for _, table := range []string{"site", "active_config", "snapshots", "routes", "sqlite_master"} {
		if after[table] != before[table] {
			t.Errorf("draft save changed %s", table)
		}
	}
	// No audit schema is introduced. Existing tables, if any, must remain empty
	// in this fresh-site fixture, which performs only draft operations.
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name LIKE '%audit%'")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	for _, name := range names {
		if scalar[int](t, db, `SELECT count(*) FROM "`+strings.ReplaceAll(name, `"`, `""`)+`"`) != 0 {
			t.Errorf("draft operation added audit entries in %s", name)
		}
	}
}

func TestPhase1EditDraft(t *testing.T) {
	site := initSite(t, true)
	db := openDB(t, site)
	admin6Custom(t, db)
	base, stop := serve(t, site)
	id := admin7Create(t, base, "First <script>draft</script>", "/first")
	otherID := admin7Create(t, base, "Other draft", "/second")
	// A fixed old fixture timestamp makes changed metadata deterministic without sleeps.
	if _, err := db.Exec("UPDATE items SET created_at='2026-01-01T00:00:00.000Z',updated_at='2026-01-01T00:00:00.000Z' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	before := admin6State(t, db)
	other := admin7Row(t, db, otherID)
	seedID := scalar[string](t, db, "SELECT id FROM items WHERE path='/example'")
	seed := admin7Row(t, db, seedID)
	_, form := admin6HTTP(t, base, "GET", "/admin/pages/"+id+"/edit", "", nil, 200)
	admin7Form(t, form, id)
	admin7Revision(t, form, "1")
	admin6Retains(t, form, url.Values{"title": {"First <script>draft</script>"}, "path": {"/first"}, "body": {"Original body"}, "strapline": {"Original intro"}})
	bodyTag := admin6Control(t, form, "body", "textarea", true)
	introTag := admin6Control(t, form, "strapline", "input", false)
	if bodyTag != "" && introTag != "" && strings.Index(form, introTag) >= strings.Index(form, bodyTag) {
		t.Error("edit fields not in configured order")
	}
	for _, text := range []string{"Story &lt;script&gt;label&lt;/script&gt;", "Body &amp; help", "Help &lt;script&gt;help&lt;/script&gt;"} {
		if !strings.Contains(form, text) {
			t.Errorf("edit form lacks configured %q", text)
		}
	}
	admin6Unchanged(t, db, before)
	v := admin7Values(1)
	// URL selects id; body identity and all other system columns cannot change it.
	for _, key := range []string{"id", "type_id", "created_at", "updated_at", "created_by", "updated_by", "published_snapshot_id", "config_revision", "publish", "unknown"} {
		v.Set(key, "attacker")
	}
	v.Set("id", otherID)
	v.Set("path", " Second/ ")
	h, _ := admin7Post(t, base, id, v, 303)
	if h.Get("Location") != "/admin/pages/"+id+"/edit" {
		t.Errorf("save redirect=%q", h.Get("Location"))
	}
	var title, path, fields, created, updated, creator, updater, typ string
	var rev int
	var published sql.NullString
	if err := db.QueryRow("SELECT title,path,fields,created_at,updated_at,created_by,updated_by,type_id,draft_revision,published_snapshot_id FROM items WHERE id=?", id).Scan(&title, &path, &fields, &created, &updated, &creator, &updater, &typ, &rev, &published); err != nil {
		t.Fatal(err)
	}
	if title != v.Get("title") || path != "/second" || rev != 2 {
		t.Errorf("saved draft=%q/%q revision=%d, want changed values and revision 2", title, path, rev)
	}
	want, _ := json.Marshal(map[string]string{"body": admin6Newlines(v.Get("body")), "strapline": v.Get("strapline")})
	equalJSON(t, fields, want)
	if created != "2026-01-01T00:00:00.000Z" || updated == created || !timestamp.MatchString(updated) || creator != "local-prototype" || updater != creator || typ != "page" || published.Valid {
		t.Errorf("save violated metadata: %s/%s actors=%s/%s type=%s published=%v", created, updated, creator, updater, typ, published)
	}
	if scalar[int](t, db, "SELECT count(*) FROM items") != 3 {
		t.Error("save changed item count")
	}
	if admin7Row(t, db, otherID) != other {
		t.Error("body ID spoof changed other Page")
	}
	if admin7Row(t, db, seedID) != seed {
		t.Error("save changed seeded published Page")
	}
	admin7NoHistory(t, db, before)
	_, saved := admin6HTTP(t, base, "GET", "/admin/pages/"+id+"/edit", "", nil, 200)
	expected := admin7Values(2)
	expected.Set("path", "/second")
	admin6Retains(t, saved, expected)
	admin7Revision(t, saved, "2")
	if strings.Contains(saved, "<script>") {
		t.Error("stored text rendered as executable markup")
	}
	admin6HTTP(t, base, "GET", "/second", "", nil, 404)
	// Block even an UPDATE that writes identical values. A no-op must issue no write.
	if _, err := db.Exec(`CREATE TRIGGER ticket7_no_update BEFORE UPDATE ON items BEGIN SELECT RAISE(ABORT,'no-op attempted write'); END`); err != nil {
		t.Fatal(err)
	}
	noOpState := admin6State(t, db)
	v.Set("draft_revision", "2")
	v.Set("title", "  "+v.Get("title")+"  ")
	admin7Post(t, base, id, v, 303)
	admin6Unchanged(t, db, noOpState)
	if _, err := db.Exec("DROP TRIGGER ticket7_no_update"); err != nil {
		t.Fatal(err)
	}
	staleState := admin6State(t, db)
	stale := admin7Values(1)
	_, failed := admin7Post(t, base, id, stale, 409)
	if !strings.Contains(failed, `data-error-code="stale_draft"`) {
		t.Error("missing stale_draft error")
	}
	admin6Retains(t, failed, stale)
	admin7Revision(t, failed, "1")
	admin7Form(t, failed, id)
	admin6Unchanged(t, db, staleState)
	// Reposting the retained stale form must not silently overwrite newer work.
	admin7Post(t, base, id, stale, 409)
	admin6Unchanged(t, db, staleState)
	stop()
	db.Close()
	db = openDB(t, site)
	admin6Unchanged(t, db, staleState)
	base, _ = serve(t, site)
	_, restarted := admin6HTTP(t, base, "GET", "/admin/pages/"+id+"/edit", "", nil, 200)
	admin6Retains(t, restarted, expected)
	admin7Revision(t, restarted, "2")
	admin6HTTP(t, base, "GET", "/second", "", nil, 404)
}

func TestPhase1EditDraftValidation(t *testing.T) {
	site := initSite(t, true)
	db := openDB(t, site)
	admin6Custom(t, db)
	base, _ := serve(t, site)
	id := admin7Create(t, base, "Valid draft", "/valid")
	before := admin6State(t, db)
	for _, tc := range []struct{ name, key, value, code string }{
		{"blank_title", "title", "  ", "required"}, {"title_long", "title", strings.Repeat("界", 201), "too_long"}, {"title_control", "title", "bad\ttext", "invalid_text"},
		{"body_required", "body", "  ", "required"}, {"body_long", "body", strings.Repeat("界", 100001), "too_long"}, {"short_long", "strapline", strings.Repeat("é", 256), "too_long"},
		{"path_required", "path", " ", "required"}, {"path_invalid", "path", "/a//b", "path_invalid"}, {"path_reserved", "path", " Admin/ ", "path_reserved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := admin7Values(1)
			v.Set(tc.key, tc.value)
			_, page := admin7Post(t, base, id, v, 422)
			if !strings.Contains(page, `data-error-code="`+tc.code+`"`) {
				t.Errorf("missing %s error", tc.code)
			}
			admin6Retains(t, page, v)
			admin7Revision(t, page, "1")
			admin7Form(t, page, id)
			if strings.Contains(page, "<script>") {
				t.Error("submitted text rendered as markup")
			}
			admin6Unchanged(t, db, before)
		})
	}
}

func TestPhase1EditDraftOwnedPathAndPublishedPath(t *testing.T) {
	site := initSite(t, true)
	db := openDB(t, site)
	publishedFixture(t, db, "/owned", "<script>Actual & owner</script>", "Public original")
	admin6Custom(t, db)
	base, _ := serve(t, site)
	id := admin7Create(t, base, "Private draft", "/private")
	ownerID := "019f0000-0000-7000-8000-000000000001"
	before := admin6State(t, db)
	v := admin7Values(1)
	v.Set("path", " Owned/ ")
	_, page := admin7Post(t, base, id, v, 409)
	if !strings.Contains(page, `data-error-code="path_taken"`) || !strings.Contains(page, "&lt;script&gt;Actual &amp; owner&lt;/script&gt;") || strings.Contains(page, "<script>") {
		t.Error("conflict lacks escaped actual owner/error code")
	}
	admin6Link(t, page, "/admin/pages/"+ownerID+"/edit")
	admin6Retains(t, page, v)
	admin7Revision(t, page, "1")
	admin6Unchanged(t, db, before)
	v.Set("path", "/changed-public-path")
	_, page = admin7Post(t, base, ownerID, v, 422)
	if !strings.Contains(page, `data-error-code="path_change_unsupported"`) {
		t.Error("missing published path rejection")
	}
	if !strings.Contains(html.UnescapeString(page), "Changing a published Page's path needs redirects, which arrive in a later phase. Keep /owned for now.") {
		t.Error("published path rejection must name the actual owned URL /owned")
	}
	admin6Retains(t, page, v)
	admin7Revision(t, page, "1")
	admin6Unchanged(t, db, before)
	seedID := scalar[string](t, db, "SELECT id FROM items WHERE path='/example'")
	_, page = admin7Post(t, base, seedID, v, 422)
	if !strings.Contains(html.UnescapeString(page), "Changing a published Page's path needs redirects, which arrive in a later phase. Keep /example for now.") {
		t.Error("published path rejection must name the actual owned URL /example")
	}
	admin6Retains(t, page, v)
	admin7Revision(t, page, "1")
	admin6Unchanged(t, db, before)
	// The recovery hint comes from the owned route, even if draft data differs.
	if _, err := db.Exec("UPDATE items SET path='/different-draft-path' WHERE id=?", ownerID); err != nil {
		t.Fatal(err)
	}
	before = admin6State(t, db)
	_, page = admin7Post(t, base, ownerID, v, 422)
	if !strings.Contains(html.UnescapeString(page), "Keep /owned for now.") {
		t.Error("published path recovery hint used the draft path instead of the owned route")
	}
	admin6Unchanged(t, db, before)
}

func TestPhase1EditDraftHTTPBoundary(t *testing.T) {
	site := initSite(t, true)
	db := openDB(t, site)
	base, _ := serve(t, site)
	id := admin7Create(t, base, "Draft", "/draft")
	before := admin6State(t, db)
	missing := "019f0000-0000-7000-8000-000000000099"
	admin6HTTP(t, base, "GET", "/admin/pages/"+missing+"/edit", "", nil, 404)
	admin7Post(t, base, missing, admin7Values(1), 404)
	for _, tc := range []struct {
		name, method, path, body string
		headers                  map[string]string
		status                   int
		allow                    string
	}{
		{"edit_wrong_method", "POST", "/admin/pages/" + id + "/edit", "", nil, 405, "GET"},
		{"save_wrong_method", "PUT", "/admin/pages/" + id, "", nil, 405, "POST"},
		{"save_delete", "DELETE", "/admin/pages/" + id, "", nil, 405, "POST"},
		{"save_head", "HEAD", "/admin/pages/" + id, "", nil, 405, "POST"},
		{"save_get", "GET", "/admin/pages/" + id, "", nil, 405, "POST"},
		{"json", "POST", "/admin/pages/" + id, `{"title":"x"}`, map[string]string{"Content-Type": "application/json"}, 415, ""},
		{"missing_type", "POST", "/admin/pages/" + id, "title=x", nil, 415, ""},
		{"oversize", "POST", "/admin/pages/" + id, "body=" + strings.Repeat("x", 1<<20), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, 413, ""},
		{"bad_host", "POST", "/admin/pages/" + id, admin7Values(1).Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Host": "attacker.example"}, 400, ""},
		{"origin", "POST", "/admin/pages/" + id, admin7Values(1).Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Origin": "https://attacker.example"}, 403, ""},
		{"fetch_metadata", "POST", "/admin/pages/" + id, admin7Values(1).Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Sec-Fetch-Site": "cross-site"}, 403, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := admin6HTTP(t, base, tc.method, tc.path, tc.body, tc.headers, tc.status)
			if tc.allow == "POST" && h.Get("Allow") != "POST" {
				t.Errorf("save Allow=%q, want exactly POST", h.Get("Allow"))
			}
			if tc.allow != "" && !strings.Contains(h.Get("Allow"), tc.allow) {
				t.Errorf("Allow=%q, want %s", h.Get("Allow"), tc.allow)
			}
			admin6Unchanged(t, db, before)
		})
	}
	admin6Unchanged(t, db, before)
}

func TestPhase1EditDraftConcurrentRevision(t *testing.T) {
	site := initSite(t, true)
	db := openDB(t, site)
	base, _ := serve(t, site)
	id := admin7Create(t, base, "Draft", "/draft")
	before := admin6State(t, db)
	start := make(chan struct{})
	statuses := make(chan int, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, title := range []string{"Concurrent A", "Concurrent B"} {
		wg.Add(1)
		go func(title string) {
			defer wg.Done()
			<-start
			v := url.Values{"title": {title}, "path": {"/draft"}, "body": {title}, "draft_revision": {"1"}}
			req, err := http.NewRequest("POST", base+"/admin/pages/"+id, strings.NewReader(v.Encode()))
			if err != nil {
				errs <- err
				return
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Cookie", adminFixtureCookie(t, base))
			client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			resp, err := client.Do(req)
			if err != nil {
				errs <- err
				return
			}
			resp.Body.Close()
			statuses <- resp.StatusCode
		}(title)
	}
	close(start)
	wg.Wait()
	close(statuses)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if !reflect.DeepEqual(counts, map[int]int{303: 1, 409: 1}) {
		t.Errorf("same revision concurrent saves=%v, want one save and one stale conflict", counts)
	}
	var rev int
	var title, fields string
	if err := db.QueryRow("SELECT draft_revision,title,fields FROM items WHERE id=?", id).Scan(&rev, &title, &fields); err != nil {
		t.Fatal(err)
	}
	if rev != 2 || (title != "Concurrent A" && title != "Concurrent B") {
		t.Errorf("concurrent stored title=%q revision=%d", title, rev)
	}
	want, _ := json.Marshal(map[string]string{"body": title})
	equalJSON(t, fields, want)
	admin7NoHistory(t, db, before)
}
