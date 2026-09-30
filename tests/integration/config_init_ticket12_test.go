package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func init12Command(t *testing.T, want int, class, path string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, append([]string{"init"}, args...)...)
	var out, diagnostic bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	err := cmd.Run()
	code := 0
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			code = e.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	if ctx.Err() != nil {
		t.Fatal("init timed out")
	}
	if code != want {
		t.Fatalf("init %v: exit %d want %d; stdout=%q stderr=%q", args, code, want, out.String(), diagnostic.String())
	}
	if want == 0 {
		if diagnostic.Len() != 0 {
			t.Errorf("success stderr=%q", diagnostic.String())
		}
	} else {
		if out.Len() != 0 {
			t.Errorf("invalid init printed plan/success: %q", out.String())
		}
		line := strings.TrimSuffix(diagnostic.String(), "\n")
		prefix := "composure init: "
		if class != "" {
			prefix += class + " at " + path + ": "
		}
		if !strings.HasPrefix(line, prefix) || strings.ContainsAny(line, "\r\n") {
			t.Errorf("want one diagnostic with %q: %q", prefix, diagnostic.String())
		}
	}
	return out.String()
}

// Deliberately reversed field-array order and integers above float64 precision.
const init12Definition = `{"format_version":1,"content_types":[{"id":"page","label":"Dispatch","fields":[{"id":"body","kind":"long_text","label":"Story <script>label</script>","help_text":"Body & help","required":true,"order":9007199254740993},{"id":"strapline","kind":"short_text","label":"Intro <b>plain</b>","help_text":"Help <script>help</script>","required":false,"order":9007199254740992}]}]}`

func init12File(t *testing.T, document string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "definition.json")
	config11Write(t, file, []byte(document))
	return file
}
func init12Form(t *testing.T, page string) {
	t.Helper()
	body := admin6Control(t, page, "body", "textarea", true)
	strap := admin6Control(t, page, "strapline", "input", false)
	if body != "" && strap != "" && strings.Index(page, strap) >= strings.Index(page, body) {
		t.Error("configured controls are not ordered by exact order")
	}
	for _, text := range []string{"Story &lt;script&gt;label&lt;/script&gt;", "Body &amp; help", "Intro &lt;b&gt;plain&lt;/b&gt;", "Help &lt;script&gt;help&lt;/script&gt;"} {
		if !strings.Contains(page, text) {
			t.Errorf("form missing configured label/help %q", text)
		}
	}
	if strings.Contains(page, "<script>") || strings.Contains(page, "<b>plain</b>") {
		t.Error("configuration became executable markup")
	}
}
func init12Rendered(t *testing.T, page, strap, body string) {
	t.Helper()
	main := admin8Content(t, page)
	for _, value := range []string{strap, body} {
		if !strings.Contains(main, html.EscapeString(value)) {
			t.Errorf("rendered content missing escaped configured value %q: %s", value, main)
		}
	}
	if strings.Index(main, html.EscapeString(strap)) >= strings.Index(main, html.EscapeString(body)) {
		t.Error("rendered fields are not in configured order")
	}
	if strings.Contains(main, "<script>") || strings.Contains(main, "<b>") {
		t.Error("field value became markup")
	}
}
func init12Fields(t *testing.T, got, strap, body string) {
	t.Helper()
	want, err := json.Marshal(map[string]string{"strapline": strap, "body": body})
	if err != nil {
		t.Fatal(err)
	}
	equalJSON(t, got, want)
}

func TestPhase1ConfigRoundTrip(t *testing.T) {
	a := initSite(t, true)
	adb := openDB(t, a)
	startupExec(t, adb, "UPDATE active_config SET document=?,revision=7", init12Definition)
	originalA := admin6State(t, adb)
	abase, _ := serve(t, a)
	_, aPublic := admin6HTTP(t, abase, "GET", "/example", "", nil, 200)
	exported := filepath.Join(t.TempDir(), "export.json")
	config11Command(t, root, 0, "", "", "export", "--site", a, "--out", exported)
	config11Command(t, root, 0, "", "", "validate", "--file", exported)
	exportedBytes := readFile(t, exported)
	b := filepath.Join(t.TempDir(), "b")
	plan := init12Command(t, 0, "", "", "--site", b, "--config", exported)
	noSite(t, b)
	for _, text := range []string{"page", "Dispatch", "strapline", "body", "revision 1", "No changes made"} {
		if !strings.Contains(plan, text) {
			t.Errorf("plan does not describe selected definition: missing %q in %q", text, plan)
		}
	}
	if strings.Contains(plan, "built-in default") || strings.Index(plan, "strapline") >= strings.Index(plan, "body") {
		t.Error("plan describes default/unordered fields")
	}
	init12Command(t, 0, "", "", "--site", b, "--config", exported, "--apply")
	bdb := openDB(t, b)
	if scalar[int](t, bdb, "SELECT revision FROM active_config") != 1 {
		t.Error("destination active revision is not 1")
	}
	// Export gives a canonical semantic comparison, including exact field order/settings.
	bExport := filepath.Join(t.TempDir(), "b-export.json")
	config11Command(t, root, 0, "", "", "export", "--site", b, "--out", bExport)
	if !bytes.Equal(readFile(t, bExport), exportedBytes) {
		t.Error("definition IDs/settings changed across initialization")
	}
	for _, table := range []string{"items", "snapshots", "routes"} {
		if scalar[int](t, bdb, "SELECT count(*) FROM "+table) != 0 {
			t.Errorf("destination copied source %s", table)
		}
	}
	if scalar[string](t, bdb, "SELECT site_id FROM site") == scalar[string](t, adb, "SELECT site_id FROM site") {
		t.Error("destination copied source identity")
	}
	bbase, stop := serve(t, b)
	_, form := admin6HTTP(t, bbase, "GET", "/admin/pages/new", "", nil, 200)
	init12Form(t, form)
	bad := url.Values{"title": {"B page"}, "path": {"/republish"}, "strapline": {"Keep <script>intro</script>"}, "body": {""}}
	before := admin6State(t, bdb)
	_, rejected := admin6Post(t, bbase, bad, 422)
	admin9Code(t, rejected, "required")
	admin6Retains(t, rejected, bad)
	admin6Unchanged(t, bdb, before)
	v := url.Values{"title": {"B page"}, "path": {"/republish"}, "strapline": {"First <script>intro</script>"}, "body": {"First <b>story</b>\r\nNext & line"}}
	h, _ := admin6Post(t, bbase, v, 303)
	id := scalar[string](t, bdb, "SELECT id FROM items")
	if id == scalar[string](t, adb, "SELECT id FROM items") || !uuid7.MatchString(id) {
		t.Error("destination did not mint its own item ID")
	}
	if h.Get("Location") != "/admin/pages/"+id+"/edit" {
		t.Error("create redirect does not identify saved item")
	}
	normalized := admin6Newlines(v.Get("body"))
	init12Fields(t, scalar[string](t, bdb, "SELECT fields FROM items"), v.Get("strapline"), normalized)
	_, edit := admin6HTTP(t, bbase, "GET", h.Get("Location"), "", nil, 200)
	init12Form(t, edit)
	admin6Retains(t, edit, v)
	admin6HTTP(t, bbase, "GET", "/republish", "", nil, 404)
	previewHeaders, preview := admin6HTTP(t, bbase, "GET", "/admin/pages/"+id+"/preview", "", nil, 200)
	admin8Headers(t, previewHeaders)
	init12Rendered(t, preview, v.Get("strapline"), normalized)
	admin9Publish(t, bbase, id, 1, 303)
	first := admin10SnapshotRow(t, bdb, id, 1)
	_, public := admin6HTTP(t, bbase, "GET", "/republish", "", nil, 200)
	init12Rendered(t, public, v.Get("strapline"), normalized)
	if admin8Content(t, preview) != admin8Content(t, public) {
		t.Error("preview/public content differ for same fields")
	}
	init12Fields(t, scalar[string](t, bdb, "SELECT fields FROM snapshots"), v.Get("strapline"), normalized)
	if scalar[int](t, bdb, "SELECT config_revision FROM snapshots") != 1 {
		t.Error("snapshot did not record destination active revision")
	}
	if scalar[string](t, bdb, "SELECT id FROM snapshots") == scalar[string](t, adb, "SELECT id FROM snapshots") {
		t.Error("snapshot identity copied from source")
	}
	v.Set("draft_revision", "1")
	v.Set("strapline", "Second <script>private</script>")
	v.Set("body", "Second <b>private</b>\rLast")
	admin7Post(t, bbase, id, v, 303)
	normalized = admin6Newlines(v.Get("body"))
	init12Fields(t, scalar[string](t, bdb, "SELECT fields FROM items"), v.Get("strapline"), normalized)
	_, preview = admin6HTTP(t, bbase, "GET", "/admin/pages/"+id+"/preview", "", nil, 200)
	init12Rendered(t, preview, v.Get("strapline"), normalized)
	_, stillPublic := admin6HTTP(t, bbase, "GET", "/republish", "", nil, 200)
	if stillPublic != public {
		t.Error("draft leaked through public snapshot")
	}
	if admin10SnapshotRow(t, bdb, id, 1) != first {
		t.Error("draft edit mutated historical snapshot")
	}
	admin9Publish(t, bbase, id, 2, 303)
	_, public = admin6HTTP(t, bbase, "GET", "/republish", "", nil, 200)
	init12Rendered(t, public, v.Get("strapline"), normalized)
	init12Fields(t, scalar[string](t, bdb, "SELECT fields FROM snapshots WHERE seq=2"), v.Get("strapline"), normalized)
	if admin10SnapshotRow(t, bdb, id, 1) != first || scalar[int](t, bdb, "SELECT count(*) FROM snapshots") != 2 {
		t.Error("republish did not retain immutable history")
	}
	beforeRestart := admin6State(t, bdb)
	stop()
	config11Write(t, exported, []byte(`{"format_version":99}`))
	config11Write(t, filepath.Join(b, "page-config.json"), []byte(`not configuration`))
	bbase, _ = serve(t, b)
	_, edit = admin6HTTP(t, bbase, "GET", "/admin/pages/"+id+"/edit", "", nil, 200)
	init12Form(t, edit)
	admin6Retains(t, edit, v)
	_, restarted := admin6HTTP(t, bbase, "GET", "/republish", "", nil, 200)
	if restarted != public {
		t.Error("restart changed published content after source-file edit")
	}
	admin6Unchanged(t, bdb, beforeRestart)
	admin6Unchanged(t, adb, originalA)
	_, aAgain := admin6HTTP(t, abase, "GET", "/example", "", nil, 200)
	if aAgain != aPublic {
		t.Error("site B changed source public content")
	}
	admin6HTTP(t, abase, "GET", "/republish", "", nil, 404)
	admin6HTTP(t, bbase, "GET", "/example", "", nil, 404)
}

func TestConfigInitTicket12RejectsDefinitionsBeforeWrites(t *testing.T) {
	cases := []struct {
		name, document, class, path string
		code                        int
	}{
		{"malformed", `{"format_version":1,"content_types":[`, "json_syntax", "$", 3},
	}
	for _, tc := range []struct {
		name, class, path string
		code              int
	}{
		{"unsupported-version-old", "unsupported_version", "$.format_version", 4},
		{"unsupported-version-new", "unsupported_version", "$.format_version", 4},
		{"duplicate-field-id", "duplicate_id", "$.content_types[0].fields[1].id", 3},
		{"duplicate-field-order", "duplicate_order", "$.content_types[0].fields[1].order", 3},
		{"duplicate-json-property", "duplicate_property", "$.content_types[0].fields[0].id", 3},
		{"unsupported-field-kind", "invalid_value", "$.content_types[0].fields[0].kind", 3},
	} {
		cases = append(cases, struct {
			name, document, class, path string
			code                        int
		}{tc.name, string(readFile(t, filepath.Join(root, "docs/phase1/examples/config-invalid", tc.name+".json"))), tc.class, tc.path, tc.code})
	}
	for _, tc := range cases {
		for _, apply := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/apply_%v", tc.name, apply), func(t *testing.T) {
				file := init12File(t, tc.document)
				for _, existing := range []bool{false, true} {
					dest := filepath.Join(t.TempDir(), "site")
					if existing {
						if err := os.Mkdir(dest, 0755); err != nil {
							t.Fatal(err)
						}
					}
					args := []string{"--site", dest, "--config", file}
					if apply {
						args = append(args, "--apply")
					}
					init12Command(t, tc.code, tc.class, tc.path, args...)
					if existing {
						if len(entries(t, dest)) != 0 {
							t.Error("invalid config changed existing empty directory")
						}
					} else {
						noSite(t, dest)
					}
					if string(readFile(t, file)) != tc.document {
						t.Error("init changed configuration source")
					}
				}
			})
		}
	}
}

func TestConfigInitTicket12PreservesOccupiedTarget(t *testing.T) {
	file := init12File(t, init12Definition)
	for _, apply := range []bool{false, true} {
		t.Run(fmt.Sprint(apply), func(t *testing.T) {
			dest := initSite(t, true)
			db := openDB(t, dest)
			before := admin6State(t, db)
			canary := filepath.Join(dest, "asset.txt")
			config11Write(t, canary, []byte("keep concurrent/user data"))
			names := entries(t, dest)
			args := []string{"--site", dest, "--config", file}
			if apply {
				args = append(args, "--apply")
			}
			init12Command(t, 3, "", "", args...)
			admin6Unchanged(t, db, before)
			if string(readFile(t, canary)) != "keep concurrent/user data" || !reflect.DeepEqual(entries(t, dest), names) {
				t.Error("occupied target files changed")
			}
		})
	}
}

func TestConfigInitTicket12ExampleValidation(t *testing.T) {
	// Fixed seed has body only; an optional additional field is allowed.
	t.Run("valid", func(t *testing.T) {
		file := init12File(t, init12Definition)
		dest := filepath.Join(t.TempDir(), "site")
		init12Command(t, 0, "", "", "--site", dest, "--config", file, "--example")
		noSite(t, dest)
		init12Command(t, 0, "", "", "--site", dest, "--config", file, "--example", "--apply")
		db := openDB(t, dest)
		for _, table := range []string{"items", "snapshots", "routes"} {
			if scalar[int](t, db, "SELECT count(*) FROM "+table) != 1 {
				t.Errorf("missing example %s", table)
			}
		}
		seed := readFile(t, filepath.Join(root, "docs/phase1/examples/seed-page.json"))
		var page struct {
			Fields map[string]string `json:"fields"`
		}
		if err := json.Unmarshal(seed, &page); err != nil {
			t.Fatal(err)
		}
		fields, _ := json.Marshal(page.Fields)
		equalJSON(t, scalar[string](t, db, "SELECT fields FROM items"), fields)
		equalJSON(t, scalar[string](t, db, "SELECT fields FROM snapshots"), fields)
	})
	for _, tc := range []struct{ name, document, class, path string }{
		{"required_addition", strings.Replace(init12Definition, `"required":false`, `"required":true`, 1), "required_value_missing", "$.fields.strapline"},
		{"unconfigured_seed", config11Definition(""), "field_value_invalid", "$.fields.body"},
	} {
		for _, apply := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/apply_%v", tc.name, apply), func(t *testing.T) {
				file := init12File(t, tc.document)
				for _, existing := range []bool{false, true} {
					dest := filepath.Join(t.TempDir(), "site")
					if existing {
						if err := os.Mkdir(dest, 0755); err != nil {
							t.Fatal(err)
						}
					}
					args := []string{"--site", dest, "--config", file, "--example"}
					if apply {
						args = append(args, "--apply")
					}
					init12Command(t, 3, tc.class, tc.path, args...)
					if existing {
						if len(entries(t, dest)) != 0 {
							t.Error("invalid seed wrote artifacts")
						}
					} else {
						noSite(t, dest)
					}
				}
			})
		}
	}
}

// Separately reaches the rendering defect on the base, which lacks --config.
func TestConfigInitTicket12ConfiguredRendering(t *testing.T) {
	site := initSite(t, false)
	db := openDB(t, site)
	definition := strings.Replace(init12Definition, `"fields":[`, `"fields":[{"id":"afterword","kind":"long_text","label":"Afterword","help_text":"More text","required":false,"order":9007199254740994},`, 1)
	config11Active(t, site, definition)
	base, _ := serve(t, site)
	v := url.Values{"afterword": {"Final <script>long text</script>"}, "title": {"Render configured fields"}, "path": {"/republish"}, "body": {"Body <b>text</b>"}, "strapline": {"Intro <script>text</script>"}}
	admin6Post(t, base, v, 303)
	id := scalar[string](t, db, "SELECT id FROM items")
	_, preview := admin6HTTP(t, base, "GET", "/admin/pages/"+id+"/preview", "", nil, 200)
	init12Rendered(t, preview, v.Get("strapline"), v.Get("body"))
	checkAfterword := func(page string) {
		t.Helper()
		main := admin8Content(t, page)
		afterword := html.EscapeString(v.Get("afterword"))
		if !strings.Contains(main, afterword) || strings.Index(main, afterword) <= strings.Index(main, html.EscapeString(v.Get("body"))) {
			t.Errorf("additional long_text missing, unescaped, or out of configured order: %s", main)
		}
	}
	checkAfterword(preview)
	admin9Publish(t, base, id, 1, 303)
	_, public := admin6HTTP(t, base, "GET", "/republish", "", nil, 200)
	init12Rendered(t, public, v.Get("strapline"), v.Get("body"))
	checkAfterword(public)
}

func TestConfigInitTicket12UnicodeLimits(t *testing.T) {
	site := initSite(t, false)
	db := openDB(t, site)
	config11Active(t, site, init12Definition)
	base, _ := serve(t, site)
	v := url.Values{"title": {"Boundary"}, "path": {"/boundary"}, "body": {strings.Repeat("界", 100000)}, "strapline": {strings.Repeat("é", 255)}}
	admin6Post(t, base, v, 303)
	init12Fields(t, scalar[string](t, db, "SELECT fields FROM items"), v.Get("strapline"), v.Get("body"))
	before := admin6State(t, db)
	for _, key := range []string{"strapline", "body"} {
		t.Run(key, func(t *testing.T) {
			bad := url.Values{"title": {"Keep"}, "path": {"/rejected"}, "body": {"Keep\r\nbody"}, "strapline": {"Keep intro"}}
			if key == "strapline" {
				bad.Set(key, strings.Repeat("é", 256))
			} else {
				bad.Set(key, strings.Repeat("界", 100001))
			}
			_, page := admin6Post(t, base, bad, 422)
			admin9Code(t, page, "too_long")
			admin6Retains(t, page, bad)
			admin6Unchanged(t, db, before)
		})
	}
}

func TestConfigInitTicket12DefaultPlan(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "site")
	plan := init12Command(t, 0, "", "", "--site", dest)
	noSite(t, dest)
	for _, line := range []string{
		"  configuration:   built-in default, revision 1",
		`  content types:   page "Page" (fields: body)`,
	} {
		if !strings.Contains(plan, line+"\n") {
			t.Errorf("default plan missing contracted line %q: %q", line, plan)
		}
	}
}
