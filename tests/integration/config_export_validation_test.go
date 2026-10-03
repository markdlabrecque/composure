package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// These acceptance tests exercise the release CLI, not a parallel test codec.
func config11Command(t *testing.T, cwd string, want int, class, path string, args ...string) (string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, append([]string{"config"}, args...)...)
	cmd.Dir = cwd
	var out, diagnostic bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	err := cmd.Run()
	code := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	if ctx.Err() != nil {
		t.Fatal("config command timed out")
	}
	if code != want {
		t.Errorf("config %v: exit %d want %d; stdout=%q stderr=%q", args, code, want, out.String(), diagnostic.String())
	}
	if want == 0 {
		if diagnostic.Len() != 0 {
			t.Errorf("success stderr=%q", diagnostic.String())
		}
	} else {
		if out.Len() != 0 {
			t.Errorf("failure stdout=%q", out.String())
		}
		line := strings.TrimSuffix(diagnostic.String(), "\n")
		prefix := "composure config " + args[0] + ": "
		if !strings.HasPrefix(line, prefix) || strings.ContainsAny(line, "\r\n") {
			t.Errorf("expected one diagnostic line with %q: %q", prefix, diagnostic.String())
		}
		if class != "" && !strings.HasPrefix(line, prefix+class+" at "+path+": ") {
			t.Errorf("want %s at %s: %q", class, path, line)
		}
	}
	return out.String(), diagnostic.String()
}

func config11Write(t *testing.T, file string, data []byte) {
	t.Helper()
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func config11Validate(t *testing.T, data []byte, want int, class, path string) string {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "input.json")
	config11Write(t, file, data)
	before := entries(t, dir)
	out, diagnostic := config11Command(t, dir, want, class, path, "validate", "--file", file)
	if want == 0 && out != "Valid Page configuration format v1.\n" {
		t.Errorf("validation stdout=%q", out)
	}
	if !bytes.Equal(readFile(t, file), data) || !reflect.DeepEqual(entries(t, dir), before) {
		t.Error("standalone validation changed input or created files")
	}
	return diagnostic
}

func config11Definition(fields string) string {
	return `{"format_version":1,"content_types":[{"id":"page","label":"Page","fields":[` + fields + `]}]}`
}
func config11Field(id, order string) string {
	return `{"id":"` + id + `","kind":"long_text","label":"Body","help_text":"","required":false,"order":` + order + `}`
}

func TestConfig11ValidateExamplesAndFixtureMatrix(t *testing.T) {
	for _, name := range []string{"page-config-default.json", "page-config-custom.json"} {
		t.Run(name, func(t *testing.T) {
			config11Validate(t, readFile(t, filepath.Join(root, "docs/phase1/examples", name)), 0, "", "")
		})
	}
	field := "$.content_types[0].fields[0]."
	for _, tc := range []struct {
		name, class, path string
		exit              int
	}{
		{"duplicate-field-id", "duplicate_id", "$.content_types[0].fields[1].id", 3},
		{"duplicate-field-order", "duplicate_order", "$.content_types[0].fields[1].order", 3},
		{"reserved-field-id", "reserved_id", field + "id", 3},
		{"unsupported-version-old", "unsupported_version", "$.format_version", 4},
		{"unsupported-version-new", "unsupported_version", "$.format_version", 4},
		{"unknown-property", "unknown_property", field + "extra", 3},
		{"unsupported-field-kind", "invalid_value", field + "kind", 3},
		{"missing-required-property", "missing_property", field + "required", 3},
		{"invalid-order-zero", "invalid_value", field + "order", 3},
		{"invalid-required-type", "invalid_type", field + "required", 3},
		{"invalid-order-boolean", "invalid_type", field + "order", 3},
		{"invalid-order-fraction", "invalid_type", field + "order", 3},
		{"invalid-field-id", "invalid_id", field + "id", 3},
		{"duplicate-json-property", "duplicate_property", field + "id", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config11Validate(t, readFile(t, filepath.Join(root, "docs/phase1/examples/config-invalid", tc.name+".json")), tc.exit, tc.class, tc.path)
		})
	}
}

func TestConfig11ValidationShapeAndPrecedence(t *testing.T) {
	base := config11Definition(config11Field("body", "10"))
	field := "$.content_types[0].fields[0]."
	cases := []struct {
		name, data, class, path string
		exit                    int
	}{
		{"root_null", `null`, "invalid_type", "$", 3}, {"root_array", `[]`, "invalid_type", "$", 3},
		{"missing_version", `{"extra":true}`, "missing_property", "$.format_version", 3},
		{"null_version", `{"format_version":null}`, "invalid_type", "$.format_version", 3},
		{"exponent_version", `{"format_version":1e0}`, "invalid_type", "$.format_version", 3},
		{"unsupported_before_shape", `{"format_version":2,"extra":true}`, "unsupported_version", "$.format_version", 4},
		{"duplicate_before_version", `{"format_version":2,"extra":1,"\u0065xtra":2}`, "duplicate_property", "$.extra", 3},
		{"escaped_duplicate_id", strings.Replace(base, `"id":"body"`, `"id":"body","\u0069d":"other"`, 1), "duplicate_property", field + "id", 3},
		{"missing_before_unknown", `{"format_version":1,"extra":true}`, "missing_property", "$.content_types", 3},
		{"unknown_before_values", strings.Replace(base, `"id":"body"`, `"id":false,"extra":true`, 1), "unknown_property", field + "extra", 3},
		{"values_schema_order", strings.Replace(strings.Replace(base, `"id":"body"`, `"id":false`, 1), `"order":10`, `"order":true`, 1), "invalid_type", field + "id", 3},
		{"null_required", strings.Replace(base, `"required":false`, `"required":null`, 1), "invalid_type", field + "required", 3},
		{"null_fields", `{"format_version":1,"content_types":[{"id":"page","label":"Page","fields":null}]}`, "invalid_type", "$.content_types[0].fields", 3},
		{"wrong_content_types", `{"format_version":1,"content_types":{}}`, "invalid_type", "$.content_types", 3},
		{"empty_types", `{"format_version":1,"content_types":[]}`, "unsupported_content_type", "$.content_types", 3},
		{"additional_type", strings.Replace(config11Definition(""), `}]}`, `},{"id":"page","label":"Second","fields":[]}]}`, 1), "unsupported_content_type", "$.content_types[1]", 3},
		{"wrong_type_id", strings.Replace(base, `"id":"page"`, `"id":"article"`, 1), "unsupported_content_type", "$.content_types[0].id", 3},
		{"exponent_order", config11Definition(config11Field("body", "1e1")), "invalid_type", field + "order", 3},
		{"negative_order", config11Definition(config11Field("body", "-1")), "invalid_value", field + "order", 3},
		{"reserved_path", config11Definition(config11Field("path", "10")), "reserved_id", field + "id", 3},
		{"non_ascii_id", config11Definition(config11Field("café", "10")), "invalid_id", field + "id", 3},
		{"long_id", config11Definition(config11Field(strings.Repeat("a", 65), "10")), "invalid_id", field + "id", 3},
		{"unknown_input_order", strings.Replace(base, `"format_version":1`, `"format_version":1,"z":true,"a":true`, 1), "unknown_property", "$.z", 3},
		{"control_property", strings.Replace(base, `"format_version":1`, `"format_version":1,"bad.key\nname":true`, 1), "unknown_property", `$["bad.key\nname"]`, 3},
	}

	for _, member := range []string{"id", "kind", "label", "help_text", "required", "order"} {
		for _, mutation := range []string{"missing", "null", "wrong_type"} {
			t.Run(member+"_"+mutation, func(t *testing.T) {
				var value any
				if mutation == "wrong_type" {
					value = []any{}
				}
				var doc map[string]any
				if err := json.Unmarshal([]byte(base), &doc); err != nil {
					t.Fatal(err)
				}
				f := doc["content_types"].([]any)[0].(map[string]any)["fields"].([]any)[0].(map[string]any)
				if mutation == "missing" {
					delete(f, member)
				} else {
					f[member] = value
				}
				data, err := json.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				class := "invalid_type"
				if mutation == "missing" {
					class = "missing_property"
				}
				config11Validate(t, data, 3, class, field+member)
			})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { config11Validate(t, []byte(tc.data), tc.exit, tc.class, tc.path) })
	}
	for name, data := range map[string]string{"empty_fields": config11Definition(""), "empty_labels": strings.ReplaceAll(base, `"Body"`, `""`), "max_id": config11Definition(config11Field(strings.Repeat("a", 64), strings.Repeat("9", 400))), "valid_pair": strings.Replace(base, `"Body"`, `"\uD83D\uDE00"`, 1)} {
		t.Run(name, func(t *testing.T) { config11Validate(t, []byte(data), 0, "", "") })
	}
}

func TestConfig11SyntaxOffsets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		data   []byte
		offset int
	}{
		{"malformed", []byte(`{"format_version":1,"content_types":[`), 37},
		{"trailing", []byte(`{} x`), 3},
		{"object_trailing_comma", []byte(`{"a":1,}`), 7},
		{"array_trailing_comma", []byte(`[1,]`), 3},
		{"invalid_unicode_hex", []byte(`{"a":"x\u12X4"}`), 11},
		{"mixed_escape_first_error", []byte(`{"a":"\q","b":"\u12X4"}`), 7},
		{"mixed_unicode_first_error", []byte(`{"a":"\u12X4","b":"\q"}`), 10},
		{"utf8", []byte{'"', 0xff, '"'}, 1},
		{"high", []byte(`{"label":"\uD800"}`), 10},
		{"low", []byte(`{"label":"\uDC00"}`), 10},
		{"reversed", []byte(`{"label":"\uDC00\uD800"}`), 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diagnostic := config11Validate(t, tc.data, 3, "json_syntax", "$")
			pattern := fmt.Sprintf(`(?i)(?:byte|offset)(?: offset)?[ :]+%d\b`, tc.offset)
			if !regexp.MustCompile(pattern).MatchString(diagnostic) {
				t.Errorf("missing zero-based offset %d: %q", tc.offset, diagnostic)
			}
		})
	}
}

const config11Canonical = `{
  "format_version": 1,
  "content_types": [
    {
      "id": "page",
      "label": "Page",
      "fields": [
        {
          "id": "intro",
          "kind": "short_text",
          "label": "Intro",
          "help_text": "",
          "required": false,
          "order": 10
        },
        {
          "id": "body",
          "kind": "long_text",
          "label": "Café",
          "help_text": "A \"quote\" and \\ slash\n<&/",
          "required": false,
          "order": 20
        }
      ]
    }
  ]
}
`
const config11Reordered = `{"content_types":[{"fields":[{"order":20,"required":false,"help_text":"A \"quote\" and \\ slash\n<&/","label":"Caf\u00e9","kind":"long_text","id":"body"},{"order":10,"required":false,"help_text":"","label":"Intro","kind":"short_text","id":"intro"}],"label":"Page","id":"page"}],"format_version":1}`

func config11Active(t *testing.T, site, document string) {
	t.Helper()
	db := openDB(t, site)
	startupExec(t, db, "UPDATE active_config SET document=?", document)
	db.Close()
}

func TestConfig11ExportCanonicalSQLiteAndNoMutation(t *testing.T) {
	site := initSite(t, true)
	custom := string(readFile(t, filepath.Join(root, "docs/phase1/examples/page-config-custom.json")))
	config11Active(t, site, custom)
	canary := filepath.Join(site, "page-config.json")
	config11Write(t, canary, []byte("unrelated configuration must not be read"))
	asset := filepath.Join(site, "asset.txt")
	config11Write(t, asset, []byte("keep local asset"))
	before := startupState(t, site)
	url, stop := serve(t, site)
	_, html := request(t, url, "GET", "/example", "", 200)
	out := filepath.Join(t.TempDir(), "export.json")
	config11Command(t, root, 0, "", "", "export", "--site", site, "--out", out)
	equalJSON(t, string(readFile(t, out)), []byte(custom))
	config11Validate(t, readFile(t, out), 0, "", "")
	if got := startupState(t, site); got != before {
		t.Error("validate/export changed database rows, markers or schema")
	}
	_, after := request(t, url, "GET", "/example", "", 200)
	if after != html {
		t.Error("export changed published response")
	}
	stop()
	if string(readFile(t, canary)) != "unrelated configuration must not be read" || string(readFile(t, asset)) != "keep local asset" {
		t.Error("site assets changed")
	}
	// Canonical bytes come from the stored definition, including reordered fields.
	config11Active(t, site, config11Reordered)
	for i := 0; i < 2; i++ {
		config11Command(t, root, 0, "", "", "export", "--site", site, "--out", out)
		if got := string(readFile(t, out)); got != config11Canonical {
			t.Errorf("canonical bytes:\n%s\nwant:\n%s", got, config11Canonical)
		}
	}
	if got := entries(t, filepath.Dir(out)); !reflect.DeepEqual(got, []string{"export.json"}) {
		t.Errorf("left temporary output: %v", got)
	}
}

func TestConfigExportOmitsSMTPEnvironment(t *testing.T) {
	sentinels := map[string]string{
		"COMPOSURE_SMTP_HOST":     "smtp-export-canary.invalid",
		"COMPOSURE_SMTP_PORT":     "2525",
		"COMPOSURE_SMTP_FROM":     "export-canary@example.invalid",
		"COMPOSURE_SMTP_USERNAME": "smtp-export-user-canary",
		"COMPOSURE_SMTP_PASSWORD": "smtp-export-password-canary",
		"COMPOSURE_SMTP_TLS":      "implicit",
	}
	for name, value := range sentinels {
		t.Setenv(name, value)
	}

	site := initSite(t, false)
	out := filepath.Join(t.TempDir(), "export.json")
	config11Command(t, root, 0, "", "", "export", "--site", site, "--out", out)
	exported := readFile(t, out)

	var document map[string]json.RawMessage
	if err := json.Unmarshal(exported, &document); err != nil {
		t.Fatalf("decode exported configuration: %v", err)
	}
	if got := len(document); got != 2 || document["format_version"] == nil || document["content_types"] == nil {
		t.Errorf("exported root keys = %v, want only format_version and content_types", reflect.ValueOf(document).MapKeys())
	}
	text := string(exported)
	if strings.Contains(strings.ToLower(text), "smtp") {
		t.Error("export contains an SMTP key")
	}
	for name, sentinel := range sentinels {
		if strings.Contains(text, sentinel) {
			t.Errorf("export contains the value of %s", name)
		}
	}
}

func TestConfig11ExportIntegerUnicodeAndEmptyArray(t *testing.T) {
	site := initSite(t, false)
	out := filepath.Join(t.TempDir(), "out.json")
	bigOrder := strings.Repeat("9", 400)
	document := strings.Replace(config11Reordered, `"order":20`, `"order":`+bigOrder, 1)
	document = strings.Replace(document, `"order":10`, `"order":9007199254740993`, 1)
	document = strings.Replace(document, `Caf\u00e9`, `\uD83D\uDE00e\u0301\b\f\r\t\u0001\u2028\u2029`, 1)
	want := strings.Replace(config11Canonical, `"order": 20`, `"order": `+bigOrder, 1)
	want = strings.Replace(want, `"order": 10`, `"order": 9007199254740993`, 1)
	want = strings.Replace(want, "Café", `😀é\b\f\r\t\u0001\u2028\u2029`, 1)
	config11Active(t, site, document)
	config11Command(t, root, 0, "", "", "export", "--site", site, "--out", out)
	if got := string(readFile(t, out)); got != want {
		t.Errorf("integer/Unicode bytes=%q want=%q", got, want)
	}
	config11Active(t, site, config11Definition(""))
	config11Command(t, root, 0, "", "", "export", "--site", site, "--out", out)
	empty := "{\n  \"format_version\": 1,\n  \"content_types\": [\n    {\n      \"id\": \"page\",\n      \"label\": \"Page\",\n      \"fields\": []\n    }\n  ]\n}\n"
	if got := string(readFile(t, out)); got != empty {
		t.Errorf("empty-array bytes=%q want=%q", got, empty)
	}
}

func TestConfig11ExportFailurePreservesOutput(t *testing.T) {
	for _, tc := range []struct {
		name, document, class, path string
		exit                        int
	}{
		{"invalid_active", `{"format_version":1,"content_types":[]}`, "unsupported_content_type", "$.content_types", 3},
		{"unsupported_active", `{"format_version":2,"extra":true}`, "unsupported_version", "$.format_version", 4},
		{"missing_property_active", `{"format_version":1}`, "missing_property", "$.content_types", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			site := initSite(t, true)
			config11Active(t, site, tc.document)
			state := startupState(t, site)
			dir := t.TempDir()
			out := filepath.Join(dir, "out.json")
			config11Write(t, out, []byte("previous output"))
			config11Command(t, root, tc.exit, tc.class, tc.path, "export", "--site", site, "--out", out)
			if string(readFile(t, out)) != "previous output" || !reflect.DeepEqual(entries(t, dir), []string{"out.json"}) {
				t.Error("failure replaced output or left temporary files")
			}
			if startupState(t, site) != state {
				t.Error("failure changed database")
			}
		})
	}
	t.Run("missing_parent", func(t *testing.T) {
		site := initSite(t, false)
		parent := filepath.Join(t.TempDir(), "absent")
		config11Command(t, root, 1, "", "", "export", "--site", site, "--out", filepath.Join(parent, "out.json"))
		noSite(t, parent)
	})
	t.Run("rename_failure", func(t *testing.T) {
		site := initSite(t, false)
		parent := t.TempDir()
		out := filepath.Join(parent, "out.json")
		if err := os.Mkdir(out, 0700); err != nil {
			t.Fatal(err)
		}
		config11Write(t, filepath.Join(out, "keep"), []byte("keep"))
		config11Command(t, root, 1, "", "", "export", "--site", site, "--out", out)
		if string(readFile(t, filepath.Join(out, "keep"))) != "keep" || !reflect.DeepEqual(entries(t, parent), []string{"out.json"}) {
			t.Error("failed rename changed target or leaked temporary file")
		}
	})
	t.Run("invalid_site", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "out.json")
		config11Write(t, out, []byte("old"))
		site := filepath.Join(dir, "absent")
		config11Command(t, root, 3, "", "", "export", "--site", site, "--out", out)
		if string(readFile(t, out)) != "old" {
			t.Error("invalid site replaced output")
		}
		noSite(t, site)
	})
}

func TestConfig11ProtectedDatabaseTargets(t *testing.T) {
	for _, name := range []string{"composure.db", "composure.db-wal", "composure.db-shm", "relative", "symlink", "hardlink", "parent_symlink"} {
		t.Run(name, func(t *testing.T) {
			site := initSite(t, true)
			state := startupState(t, site)
			db := filepath.Join(site, "composure.db")
			target := filepath.Join(site, name)
			switch name {
			case "relative":
				target = filepath.Join(site, "..", filepath.Base(site), "composure.db")
			case "symlink":
				if err := os.Symlink(db, target); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(db, target); err != nil {
					t.Fatal(err)
				}
			case "parent_symlink":
				alias := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(site, alias); err != nil {
					t.Fatal(err)
				}
				target = filepath.Join(alias, "composure.db")
			}
			config11Command(t, root, 1, "", "", "export", "--site", site, "--out", target)
			if startupState(t, site) != state {
				t.Error("protected target damaged database state")
			}
		})
	}
}

func TestConfig11UsageAndInputIO(t *testing.T) {
	for _, args := range [][]string{{"validate"}, {"validate", "--file"}, {"validate", "--file", "x", "--site", "x"}, {"export"}, {"export", "--site", "x"}, {"export", "--out", "x"}, {"validate", "--unknown"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) { config11Command(t, root, 2, "", "", args...) })
	}
	dir := t.TempDir()
	config11Command(t, root, 1, "", "", "validate", "--file", filepath.Join(dir, "missing"))
	config11Command(t, root, 1, "", "", "validate", "--file", dir)
}

// Unknown verbs are untrusted argument text and must not inject physical lines.
func TestConfig11UnknownVerbDiagnosticSingleLine(t *testing.T) {
	for _, tc := range []struct{ name, verb string }{
		{"line_feed", "unknown\nforged diagnostic"},
		{"carriage_return", "unknown\rforged diagnostic"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "config", tc.verb)
			var out, diagnostic bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &diagnostic
			err := cmd.Run()
			if ctx.Err() != nil {
				t.Fatal("unknown command timed out")
			}
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 2 {
				t.Fatalf("unknown verb exit=%v want 2; stderr=%q", err, diagnostic.String())
			}
			if out.Len() != 0 {
				t.Errorf("unknown verb stdout=%q", out.String())
			}
			line := strings.TrimSuffix(diagnostic.String(), "\n")
			if !strings.HasPrefix(line, "composure config") || strings.ContainsAny(line, "\r\n") || strings.Contains(line, tc.verb) {
				t.Errorf("unknown verb must produce one prefixed stderr line without raw argument controls: %q", diagnostic.String())
			}
		})
	}
}
