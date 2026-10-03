package integration_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// These acceptance tests use the release binary and disposable real SQLite sites.
func admin6HTTP(t *testing.T, base, method, path, body string, headers map[string]string, want int) (http.Header, string) {
	t.Helper()
	req, err := http.NewRequest(method, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range headers {
		if key == "Host" {
			req.Host = value
		} else {
			req.Header.Set(key, value)
		}
	}
	if strings.HasPrefix(path, "/admin") && path != "/admin/sign-in" && path != "/admin/static/admin.css" {
		req.Header.Set("Cookie", adminFixtureCookie(t, base))
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != want {
		t.Errorf("%s %s: status %d, want %d; response %.400s", method, path, resp.StatusCode, want, data)
	}
	if strings.HasPrefix(path, "/admin") && resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("%s: Cache-Control=%q, want no-store", path, resp.Header.Get("Cache-Control"))
	}
	return resp.Header, string(data)
}
func admin6Post(t *testing.T, base string, values url.Values, want int) (http.Header, string) {
	t.Helper()
	return admin6HTTP(t, base, "POST", "/admin/pages", values.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, want)
}
func admin6Values() url.Values {
	return url.Values{"title": {"<script>New & draft</script>"}, "path": {" About-Us/ "}, "body": {"first\r\n<script>body</script>\rlast"}}
}

// Logical snapshots include every committed column, configuration and schema.
// WAL bookkeeping may change during reads; committed content must not.
func admin6State(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	state := map[string]string{}
	for _, table := range []string{"site", "active_config", "items", "snapshots", "routes", "sqlite_master"} {
		rows, err := db.Query("SELECT * FROM " + table + " ORDER BY 1")
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		all := [][]any{}
		for rows.Next() {
			values := make([]any, len(columns))
			ptrs := make([]any, len(values))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			all = append(all, values)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		data, err := json.Marshal(all)
		if err != nil {
			t.Fatal(err)
		}
		state[table] = string(data)
	}
	return state
}
func admin6Unchanged(t *testing.T, db *sql.DB, before map[string]string) {
	t.Helper()
	if got := admin6State(t, db); !reflect.DeepEqual(got, before) {
		t.Errorf("request changed committed state: before=%v after=%v", before, got)
	}
}

var admin6TagRE = regexp.MustCompile(`(?is)<(?:input|textarea|label|form|a|button)\b[^>]*>`)
var admin6AttrRE = regexp.MustCompile(`(?i)([a-z][a-z0-9_-]*)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+)))?`)

func admin6Attrs(tag string) map[string]string {
	a := map[string]string{}
	for _, m := range admin6AttrRE.FindAllStringSubmatch(tag, -1) {
		value := m[2]
		if m[3] != "" {
			value = m[3]
		}
		if m[4] != "" {
			value = m[4]
		}
		a[strings.ToLower(m[1])] = html.UnescapeString(value)
	}
	return a
}
func admin6Control(t *testing.T, page, name, kind string, required bool) string {
	t.Helper()
	for _, tag := range admin6TagRE.FindAllString(page, -1) {
		a := admin6Attrs(tag)
		if a["name"] != name {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(tag), "<"+kind) {
			t.Errorf("%s widget=%s, want %s", name, tag, kind)
		}
		if kind == "input" && a["type"] != "text" {
			t.Errorf("%s must be text input: %s", name, tag)
		}
		_, hasRequired := a["required"]
		if hasRequired != required {
			t.Errorf("%s required=%v, want %v", name, hasRequired, required)
		}
		if a["id"] == "" {
			t.Errorf("%s lacks label target", name)
		}
		labelled := false
		for _, label := range admin6TagRE.FindAllString(page, -1) {
			if strings.HasPrefix(strings.ToLower(label), "<label") && admin6Attrs(label)["for"] == a["id"] {
				labelled = true
			}
		}
		if !labelled {
			t.Errorf("%s lacks associated label", name)
		}
		return tag
	}
	t.Errorf("missing configured control %q", name)
	return ""
}
func admin6Link(t *testing.T, page, target string) {
	t.Helper()
	for _, tag := range admin6TagRE.FindAllString(page, -1) {
		if strings.HasPrefix(strings.ToLower(tag), "<a") && admin6Attrs(tag)["href"] == target {
			return
		}
	}
	t.Errorf("missing clickable link to %s", target)
}
func admin6Newlines(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
}

func admin6Retains(t *testing.T, page string, values url.Values) {
	t.Helper()
	for _, name := range []string{"title", "path", "body", "strapline"} {
		if _, ok := values[name]; !ok {
			continue
		}
		found := false
		for _, tag := range admin6TagRE.FindAllString(page, -1) {
			a := admin6Attrs(tag)
			if a["name"] != name {
				continue
			}
			if strings.HasPrefix(strings.ToLower(tag), "<input") {
				found = a["value"] == values.Get(name)
			} else if strings.HasPrefix(strings.ToLower(tag), "<textarea") {
				start := strings.Index(page, tag) + len(tag)
				tail := page[start:]
				end := strings.Index(strings.ToLower(tail), "</textarea>")
				if end >= 0 {
					// HTML normalizes source line endings and ignores the first LF
					// immediately after a textarea start tag. Raw source equality
					// would miss the user's lost leading newline on redisplay.
					got := html.UnescapeString(admin6Newlines(tail[:end]))
					got = admin6Newlines(strings.TrimPrefix(got, "\n"))
					found = got == admin6Newlines(values.Get(name))
				}
			}
		}
		if !found {
			t.Errorf("error form failed to retain %s value of %d characters", name, len(values.Get(name)))
		}
	}
}
func admin6Custom(t *testing.T, db *sql.DB) {
	t.Helper()
	const document = `{"format_version":1,"content_types":[{"id":"page","label":"Page","fields":[{"id":"body","kind":"long_text","label":"Story <script>label</script>","help_text":"Body & help","required":true,"order":9007199254740993},{"id":"strapline","kind":"short_text","label":"Intro <b>plain</b>","help_text":"Help <script>help</script>","required":false,"order":9007199254740992}]}]}`
	if _, err := db.Exec("UPDATE active_config SET document=?", document); err != nil {
		t.Fatal(err)
	}
}

func TestAdminCreateTicket6NavigationAndDefaultForm(t *testing.T) {
	site := initSite(t, true)
	db := openDB(t, site)
	before := admin6State(t, db)
	base, _ := serve(t, site)
	h, _ := admin6HTTP(t, base, "GET", "/admin", "", nil, 303)
	if h.Get("Location") != "/admin/pages" {
		t.Errorf("admin redirect=%q", h.Get("Location"))
	}
	_, list := admin6HTTP(t, base, "GET", "/admin/pages", "", nil, 200)
	admin6Link(t, list, "/admin/pages/new")
	id := scalar[string](t, db, "SELECT id FROM items")
	admin6Link(t, list, "/admin/pages/"+id+"/edit")
	for _, s := range []string{"Example Page", "/example", "Published", scalar[string](t, db, "SELECT updated_at FROM items")} {
		if !strings.Contains(strings.ToLower(list), strings.ToLower(s)) {
			t.Errorf("list lacks %q", s)
		}
	}
	_, form := admin6HTTP(t, base, "GET", "/admin/pages/new", "", nil, 200)
	admin6Control(t, form, "title", "input", true)
	admin6Control(t, form, "path", "input", true)
	admin6Control(t, form, "body", "textarea", false)
	validForm := false
	for _, tag := range admin6TagRE.FindAllString(form, -1) {
		a := admin6Attrs(tag)
		if strings.HasPrefix(strings.ToLower(tag), "<form") && strings.EqualFold(a["method"], "post") && a["action"] == "/admin/pages" {
			validForm = true
		}
	}
	if !validForm {
		t.Error("create lacks real POST form")
	}
	admin6HTTP(t, base, "GET", "/admin/pages/019f0000-0000-7000-8000-000000000099/edit", "", nil, 404)
	admin6HTTP(t, base, "HEAD", "/admin/pages/new", "", nil, 200)
	admin6Unchanged(t, db, before)
}

func TestAdminCreateTicket6DraftPersistenceAndMetadata(t *testing.T) {
	site := initSite(t, true)
	db := openDB(t, site)
	before := admin6State(t, db)
	base, stop := serve(t, site)
	accountID := adminFixtureAccountID(t, base)
	values := admin6Values()
	for _, key := range []string{"id", "type_id", "draft_revision", "created_at", "updated_at", "created_by", "updated_by", "published_snapshot_id", "config_revision", "config", "publish", "unknown"} {
		values.Set(key, "attacker")
	}
	h, _ := admin6Post(t, base, values, 303)
	location := h.Get("Location")
	var id, typ, title, path, fields, created, updated, creator, updater string
	var rev int
	var published sql.NullString
	if err := db.QueryRow(`SELECT id,type_id,title,path,fields,draft_revision,created_at,updated_at,created_by,updated_by,published_snapshot_id FROM items WHERE path='/about-us'`).Scan(&id, &typ, &title, &path, &fields, &rev, &created, &updated, &creator, &updater, &published); err != nil {
		t.Fatalf("valid create did not persist draft: %v", err)
	}
	if !uuid7.MatchString(id) || typ != "page" || rev != 1 || published.Valid || creator != accountID || updater != accountID || created != updated || !timestamp.MatchString(created) {
		t.Errorf("invalid server metadata: id=%s type=%s rev=%d published=%v actors=%s/%s times=%s/%s", id, typ, rev, published, creator, updater, created, updated)
	}
	if title != values.Get("title") || path != "/about-us" {
		t.Errorf("draft title/path=%q/%q", title, path)
	}
	equalJSON(t, fields, []byte(`{"body":"first\n<script>body</script>\nlast"}`))
	if location != "/admin/pages/"+id+"/edit" {
		t.Errorf("saved redirect=%q", location)
	}
	for _, table := range []string{"site", "active_config", "snapshots", "routes", "sqlite_master"} {
		if admin6State(t, db)[table] != before[table] {
			t.Errorf("create changed %s", table)
		}
	}
	_, saved := admin6HTTP(t, base, "GET", location, "", nil, 200)
	_, list := admin6HTTP(t, base, "GET", "/admin/pages", "", nil, 200)
	admin6Link(t, list, location)
	for _, page := range []string{saved, list} {
		if !strings.Contains(page, "&lt;script&gt;New &amp; draft&lt;/script&gt;") || strings.Contains(page, "<script>") {
			t.Errorf("unsafe/missing stored title %.500s", page)
		}
	}
	if !strings.Contains(saved, "&lt;script&gt;body&lt;/script&gt;") {
		t.Error("saved route lacks escaped stored body")
	}
	// Ticket 7 replaces the intermediate read-only screen with the real edit form.
	admin7Form(t, saved, id)
	admin7Revision(t, saved, "1")
	admin6HTTP(t, base, "GET", "/about-us", "", nil, 404)
	stop()
	base, _ = serve(t, site)
	_, again := admin6HTTP(t, base, "GET", location, "", nil, 200)
	if again != saved {
		t.Error("saved HTML changed across restart")
	}
	admin6HTTP(t, base, "GET", "/admin/pages", "", nil, 200)
	// No route reservation: another never-published draft can propose the same URL.
	values.Set("title", "Second draft")
	admin6Post(t, base, values, 303)
	if got := scalar[int](t, db, "SELECT count(*) FROM items WHERE path='/about-us'"); got != 2 {
		t.Errorf("draft-only duplicate count=%d", got)
	}
}

func TestAdminCreateTicket6ConfiguredFields(t *testing.T) {
	site := initSite(t, false)
	db := openDB(t, site)
	admin6Custom(t, db)
	base, _ := serve(t, site)
	_, form := admin6HTTP(t, base, "GET", "/admin/pages/new", "", nil, 200)
	body := admin6Control(t, form, "body", "textarea", true)
	intro := admin6Control(t, form, "strapline", "input", false)
	if body != "" && intro != "" && strings.Index(form, intro) >= strings.Index(form, body) {
		t.Error("fields are not in exact numeric order")
	}
	for _, text := range []string{"Story &lt;script&gt;label&lt;/script&gt;", "Body &amp; help", "Intro &lt;b&gt;plain&lt;/b&gt;", "Help &lt;script&gt;help&lt;/script&gt;"} {
		if !strings.Contains(form, text) {
			t.Errorf("configured form lacks escaped %q", text)
		}
	}
	if strings.Contains(form, "<script>") || strings.Contains(form, "<b>plain</b>") {
		t.Error("config text rendered as markup")
	}
	v := url.Values{"title": {"Unicode fields"}, "path": {"/configured"}, "body": {strings.Repeat("界", 100000)}, "strapline": {strings.Repeat("é", 255)}}
	admin6Post(t, base, v, 303)
	fields := scalar[string](t, db, "SELECT fields FROM items")
	want, _ := json.Marshal(map[string]string{"body": v.Get("body"), "strapline": v.Get("strapline")})
	equalJSON(t, fields, want)
}

func TestAdminCreateTicket6ValidationPreservesCommittedState(t *testing.T) {
	cases := []struct{ name, key, value, code string }{
		{"blank_title", "title", "  ", "required"}, {"blank_path", "path", "  ", "required"}, {"invalid_path", "path", "/a//b", "path_invalid"}, {"encoded_path", "path", "/a%20b", "path_invalid"}, {"dot_path", "path", "/../x", "path_invalid"}, {"deep_path", "path", "/a/b/c/d/e/f/g/h/i", "path_invalid"}, {"long_path", "path", "/" + strings.Repeat("a", 200), "path_invalid"},
		{"title_limit", "title", strings.Repeat("界", 201), "too_long"}, {"title_control", "title", "bad\ttext", "invalid_text"}, {"body_required", "body", "", "required"}, {"body_limit", "body", strings.Repeat("界", 100001), "too_long"}, {"short_limit", "strapline", strings.Repeat("é", 256), "too_long"},
	}
	for _, segment := range []string{"admin", "static", "media", "files", "healthz", "api"} {
		cases = append(cases, struct{ name, key, value, code string }{"reserved_" + segment, "path", "/" + segment + "/x", "path_reserved"})
	}
	site := initSite(t, true)
	db := openDB(t, site)
	admin6Custom(t, db)
	base, _ := serve(t, site)
	before := admin6State(t, db)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := url.Values{"title": {"Keep <script>title</script>"}, "path": {" /kept/ "}, "body": {"Keep <script>body</script>"}, "strapline": {"Keep & intro"}}
			v.Set(tc.key, tc.value)
			_, page := admin6Post(t, base, v, 422)
			if !strings.Contains(page, `data-error-code="`+tc.code+`"`) && !strings.Contains(page, `data-error-code='`+tc.code+`'`) {
				t.Errorf("missing error code %s", tc.code)
			}
			admin6Retains(t, page, v)
			if strings.Contains(page, "<script>") {
				t.Error("submitted text rendered as markup")
			}
			admin6Unchanged(t, db, before)
		})
	}
}

func TestAdminCreateTicket6ConflictNamesActualOwner(t *testing.T) {
	for _, unpublished := range []bool{false, true} {
		t.Run(fmt.Sprint("unpublished_", unpublished), func(t *testing.T) {
			site := initSite(t, true)
			db := openDB(t, site)
			publishedFixture(t, db, "/owned", "<script>Actual & owner</script>", "owned body")
			if unpublished {
				if _, err := db.Exec("UPDATE items SET published_snapshot_id=NULL WHERE path='/owned'"); err != nil {
					t.Fatal(err)
				}
			}
			before := admin6State(t, db)
			base, _ := serve(t, site)
			v := admin6Values()
			v.Set("path", " Owned/ ")
			_, page := admin6Post(t, base, v, 409)
			if (!strings.Contains(page, `data-error-code="path_taken"`) && !strings.Contains(page, `data-error-code='path_taken'`)) || !strings.Contains(page, "&lt;script&gt;Actual &amp; owner&lt;/script&gt;") || strings.Contains(page, "<script>") {
				t.Error("conflict lacks escaped actual owner/error code")
			}
			admin6Link(t, page, "/admin/pages/019f0000-0000-7000-8000-000000000001/edit")
			admin6Retains(t, page, v)
			admin6Unchanged(t, db, before)
		})
	}
}

func TestAdminCreateTicket6HTTPBoundary(t *testing.T) {
	site := initSite(t, true)
	db := openDB(t, site)
	before := admin6State(t, db)
	base, _ := serve(t, site)
	cases := []struct {
		name, method, body string
		headers            map[string]string
		status             int
	}{
		{"json", "POST", `{"title":"x"}`, map[string]string{"Content-Type": "application/json"}, 415},
		{"missing_content_type", "POST", "title=x", nil, 415},
		{"multipart", "POST", "anything", map[string]string{"Content-Type": "multipart/form-data; boundary=x"}, 415},
		{"oversize", "POST", "title=x&path=/x&body=" + strings.Repeat("a", 1<<20), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, 413},
		{"cross_origin", "POST", admin6Values().Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Origin": "https://attacker.example"}, 403},
		{"fetch_metadata", "POST", admin6Values().Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Sec-Fetch-Site": "cross-site"}, 403},
		{"bad_host", "POST", admin6Values().Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Host": "attacker.example"}, 400},
		{"wrong_method", "PUT", "", nil, 405},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := admin6HTTP(t, base, tc.method, "/admin/pages", tc.body, tc.headers, tc.status)
			if tc.status == 405 && (!strings.Contains(h.Get("Allow"), "GET") || !strings.Contains(h.Get("Allow"), "POST")) {
				t.Errorf("wrong-method Allow=%q", h.Get("Allow"))
			}
			admin6Unchanged(t, db, before)
		})
	}
}

func TestAdminCreateTicket6ValidBoundaries(t *testing.T) {
	site := initSite(t, false)
	db := openDB(t, site)
	base, _ := serve(t, site)
	v := url.Values{"title": {"  " + strings.Repeat("界", 200) + "  "}, "path": {" / "}, "body": {""}}
	admin6Post(t, base, v, 303)
	var title, path, fields string
	if err := db.QueryRow("SELECT title,path,fields FROM items").Scan(&title, &path, &fields); err != nil {
		t.Fatalf("valid root/optional-body boundary did not persist: %v", err)
	}
	if title != strings.Repeat("界", 200) || path != "/" {
		t.Errorf("trimmed title/root path=%q/%q", title, path)
	}
	equalJSON(t, fields, []byte(`{"body":""}`))
	admin6HTTP(t, base, "GET", "/", "", nil, 404)
}

func TestAdminCreateTicket6DefinitionWithoutBody(t *testing.T) {
	site := initSite(t, false)
	db := openDB(t, site)
	const document = `{"format_version":1,"content_types":[{"id":"page","label":"Page","fields":[{"id":"summary","kind":"long_text","label":"Summary","help_text":"Configured summary","required":true,"order":1}]}]}`
	if _, err := db.Exec("UPDATE active_config SET document=?", document); err != nil {
		t.Fatal(err)
	}
	base, _ := serve(t, site)
	_, form := admin6HTTP(t, base, "GET", "/admin/pages/new", "", nil, 200)
	admin6Control(t, form, "summary", "textarea", true)
	for _, tag := range admin6TagRE.FindAllString(form, -1) {
		if admin6Attrs(tag)["name"] == "body" {
			t.Error("form invented unconfigured body field")
		}
	}
	v := url.Values{"title": {"Configured identity"}, "path": {"/summary"}, "summary": {"Persist this field"}, "body": {"Ignore unconfigured body"}}
	admin6Post(t, base, v, 303)
	if scalar[int](t, db, "SELECT count(*) FROM items") != 1 {
		t.Fatal("configured create did not persist")
	}
	equalJSON(t, scalar[string](t, db, "SELECT fields FROM items"), []byte(`{"summary":"Persist this field"}`))
}

// Invalid creation must redisplay the browser-visible textarea value, including
// its first newline, without relying on a create write while storage is pending.
func TestAdminCreateTicket6ValidationRetainsLeadingTextareaNewline(t *testing.T) {
	site := initSite(t, true)
	db := openDB(t, site)
	before := admin6State(t, db)
	base, _ := serve(t, site)
	for _, tc := range []struct{ name, body string }{
		{"leading_lf", "\nLeading line"},
		{"leading_crlf", "\r\nLeading line\r\nSecond line"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := url.Values{"title": {"Keep title"}, "path": {"/admin/reserved"}, "body": {tc.body}}
			_, page := admin6Post(t, base, values, 422)
			if !strings.Contains(page, `data-error-code="path_reserved"`) && !strings.Contains(page, `data-error-code='path_reserved'`) {
				t.Error("missing path_reserved validation error")
			}
			admin6Retains(t, page, values)
			admin6Unchanged(t, db, before)
		})
	}
}
