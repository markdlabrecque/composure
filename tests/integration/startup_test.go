package integration_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Capture every committed row and SQL definition, including markers. SQLite
// may checkpoint WAL files during an open; those bytes are not content state.
func startupState(t *testing.T, site string) string {
	t.Helper()
	db := openDB(t, site)
	defer db.Close()
	state := map[string]any{}
	for _, query := range []string{
		"PRAGMA application_id", "PRAGMA user_version", "PRAGMA journal_mode",
		"SELECT type,name,tbl_name,sql FROM sqlite_schema ORDER BY type,name",
		"SELECT * FROM site ORDER BY singleton", "SELECT * FROM active_config ORDER BY singleton",
		"SELECT * FROM items ORDER BY id", "SELECT * FROM snapshots ORDER BY id", "SELECT * FROM routes ORDER BY path",
	} {
		rows, err := db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		var values [][]any
		for rows.Next() {
			row := make([]any, len(columns))
			destinations := make([]any, len(columns))
			for i := range row {
				destinations[i] = &row[i]
			}
			if err := rows.Scan(destinations...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			values = append(values, row)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rows.Close()
		state[query] = values
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func startupExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("fixture %s: %v", query, err)
	}
}

func startupReject(t *testing.T, site, addr string, want int, fragments ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "serve", "--site", site, "--addr", addr)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
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
		t.Errorf("startup did not reject before readiness deadline; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if code != want {
		t.Errorf("exit %d, want %d; stdout=%q stderr=%q", code, want, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("rejected startup wrote stdout/readiness: %q", stdout.String())
	}
	line := strings.TrimSuffix(stderr.String(), "\n")
	if !strings.HasPrefix(line, "composure serve: ") || strings.ContainsAny(line, "\r\n") {
		t.Errorf("expected one prefixed diagnostic line: %q", stderr.String())
	}
	for _, fragment := range fragments {
		if !strings.Contains(strings.ToLower(line), strings.ToLower(fragment)) {
			t.Errorf("diagnostic missing %q: %q", fragment, line)
		}
	}
}

func TestPhase1StartupCompatibility(t *testing.T) {
	t.Run("supported_restarts_ignore_disk_config", func(t *testing.T) {
		site := initSite(t, true)
		before := startupState(t, site)
		var html string
		for i := 0; i < 3; i++ {
			// Both a valid changed definition and malformed canary must be ignored.
			canary := []byte(`{"format_version":2,"content_types":[]}`)
			if i == 2 {
				canary = []byte("not JSON and never imported")
			}
			for _, name := range []string{"config.json", "page-config.json"} {
				if err := os.WriteFile(filepath.Join(site, name), canary, 0600); err != nil {
					t.Fatal(err)
				}
			}
			url, stop := serve(t, site)
			_, body := request(t, url, "GET", "/example", "", 200)
			if i == 0 {
				html = body
			} else if body != html {
				t.Error("published render changed on restart")
			}
			stop()
			if got := startupState(t, site); got != before {
				t.Error("startup changed committed rows, IDs, config, markers or schema")
			}
			for _, name := range []string{"config.json", "page-config.json"} {
				if !bytes.Equal(readFile(t, filepath.Join(site, name)), canary) {
					t.Error("startup rewrote disk config canary")
				}
			}
		}
	})
	// An occupied port also proves the compatibility failure wins over Listen.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	addresses := []string{"127.0.0.1:0", occupied.Addr().String()}
	for _, marker := range []struct{ name, query, diagnostic string }{
		{"schema", "PRAGMA user_version=%d", "schema"},
		{"site_config", "UPDATE site SET config_format_version=%d", "configuration format"},
		{"document_config", "", "configuration format"},
	} {
		for _, version := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s_%d", marker.name, version), func(t *testing.T) {
				site := initSite(t, true)
				db := openDB(t, site)
				if marker.query != "" {
					startupExec(t, db, fmt.Sprintf(marker.query, version))
				} else {
					document := scalar[string](t, db, "SELECT document FROM active_config")
					startupExec(t, db, "UPDATE active_config SET document=?", strings.Replace(document, `"format_version": 1`, fmt.Sprintf(`"format_version": %d`, version), 1))
				}
				db.Close()
				before := startupState(t, site)
				direction, action := "older", "migration"
				if version == 2 {
					direction, action = "newer", "newer composure binary"
				}
				for _, addr := range addresses {
					startupReject(t, site, addr, 4, marker.diagnostic, fmt.Sprint(version), direction, action)
				}
				if got := startupState(t, site); got != before {
					t.Error("rejection changed logical state or schema")
				}
				if marker.name == "document_config" {
					// Recognize a syntactically valid document's version before
					// checking properties belonging to the supported format.
					db = openDB(t, site)
					startupExec(t, db, "UPDATE active_config SET document=?", fmt.Sprintf(`{"format_version":%d,"extra":true}`, version))
					db.Close()
					before = startupState(t, site)
					startupReject(t, site, occupied.Addr().String(), 4, marker.diagnostic, fmt.Sprint(version), direction, action)
					if got := startupState(t, site); got != before {
						t.Error("unsupported document rejection changed state")
					}
				}
			})
		}
	}
	t.Run("large_document_format_marker", func(t *testing.T) {
		version := strings.Repeat("9", 400)
		site := initSite(t, true)
		db := openDB(t, site)
		startupExec(t, db, "UPDATE active_config SET document=?", fmt.Sprintf(`{"format_version":%s,"content_types":[]}`, version))
		db.Close()
		before := startupState(t, site)
		startupReject(t, site, occupied.Addr().String(), 4, "configuration format", version, "newer", "newer composure binary")
		if got := startupState(t, site); got != before {
			t.Error("unsupported document rejection changed state")
		}
	})
	for _, tc := range []struct {
		name, mutation string
		diagnostic     []string
	}{
		{"wrong_identity_before_schema", "PRAGMA application_id=0; PRAGMA user_version=2", []string{"not a Composure site database"}},
		{"non_wal_before_schema", "PRAGMA journal_mode=DELETE; PRAGMA user_version=2", []string{"integrity", "journal mode", "delete"}},
		{"failed_quick_check_before_schema", "PRAGMA ignore_check_constraints=ON; UPDATE active_config SET revision=0; PRAGMA ignore_check_constraints=OFF; PRAGMA user_version=2", []string{"integrity", "active_config"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			site := initSite(t, true)
			db := openDB(t, site)
			startupExec(t, db, tc.mutation)
			db.Close()
			before := startupState(t, site)
			for _, addr := range addresses {
				startupReject(t, site, addr, 3, tc.diagnostic...)
			}
			if got := startupState(t, site); got != before {
				t.Error("rejection changed logical state or schema")
			}
		})
	}
	t.Run("schema_before_site_config_before_document", func(t *testing.T) {
		site := initSite(t, true)
		db := openDB(t, site)
		startupExec(t, db, "PRAGMA user_version=0; UPDATE site SET config_format_version=2; UPDATE active_config SET document='{}'")
		db.Close()
		before := startupState(t, site)
		startupReject(t, site, occupied.Addr().String(), 4, "schema version 0", "migration")
		if got := startupState(t, site); got != before {
			t.Error("schema rejection changed state")
		}
		db = openDB(t, site)
		startupExec(t, db, "PRAGMA user_version=1")
		db.Close()
		before = startupState(t, site)
		startupReject(t, site, occupied.Addr().String(), 4, "configuration format version 2", "newer composure binary")
		if got := startupState(t, site); got != before {
			t.Error("config marker rejection changed state")
		}
	})
	for _, kind := range []string{"missing_directory", "missing_database", "corrupt", "directory_database"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			site := filepath.Join(parent, "site")
			if kind != "missing_directory" {
				if err := os.Mkdir(site, 0700); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(site, "composure.db")
			data := []byte("not a SQLite database\x00keep these bytes")
			if kind == "corrupt" {
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "directory_database" {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(path, "keep"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			startupReject(t, site, "0.0.0.0:0", 2, "loopback")
			for _, addr := range addresses {
				diagnostic := "not a Composure site database"
				if kind == "missing_directory" {
					diagnostic = "does not exist"
				}
				if kind == "missing_database" {
					diagnostic = "composure.db missing"
				}
				startupReject(t, site, addr, 3, diagnostic)
			}
			if kind == "missing_directory" {
				noSite(t, site)
				return
			}
			want := []string{}
			if kind != "missing_database" {
				want = []string{"composure.db"}
			}
			if !reflect.DeepEqual(entries(t, site), want) && !(kind == "missing_database" && len(entries(t, site)) == 0) {
				t.Errorf("rejection replaced/created site files: %v", entries(t, site))
			}
			if kind == "corrupt" && !bytes.Equal(readFile(t, path), data) {
				t.Error("corrupt input replaced")
			}
			if kind == "directory_database" {
				if got := entries(t, path); !reflect.DeepEqual(got, []string{"keep"}) || !bytes.Equal(readFile(t, filepath.Join(path, "keep")), data) {
					t.Error("nonregular database input changed")
				}
			}
		})
	}
	t.Run("active_definitions", func(t *testing.T) {
		files, err := filepath.Glob(filepath.Join(root, "docs/phase1/examples/config-invalid/*.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 0 {
			t.Fatal("missing established invalid fixtures")
		}
		cases := map[string][]byte{}
		for _, file := range files {
			if !strings.Contains(filepath.Base(file), "unsupported-version") {
				cases[filepath.Base(file)] = readFile(t, file)
			}
		}
		defaultDoc := string(readFile(t, filepath.Join(root, "internal/config/default.json")))
		for name, replacement := range map[string]string{
			"lone_high_surrogate": `\uD800`, "lone_low_surrogate": `\uDC00`, "reversed_surrogates": `\uDC00\uD800`, "invalid_utf8": string([]byte{0xff}),
		} {
			cases[name] = []byte(strings.Replace(defaultDoc, "Body", replacement, 1))
		}
		cases["null_field"] = []byte(strings.Replace(defaultDoc, `"label": "Body"`, `"label": null`, 1))
		cases["exponent_order"] = []byte(strings.Replace(defaultDoc, `"order": 10`, `"order": 1e1`, 1))
		cases["escaped_duplicate_property"] = []byte(strings.Replace(defaultDoc, `"id": "body"`, `"id": "body", "\u0069d": "body"`, 1))
		cases["malformed"] = []byte(`{"format_version":1,`)
		for name, document := range cases {
			t.Run(name, func(t *testing.T) {
				site := initSite(t, true)
				db := openDB(t, site)
				if name == "malformed" {
					// A malformed document normally violates json_valid at quick_check.
					// Rebuild only this fixture's config table without that CHECK so the
					// decoder gate is exercised after a genuinely successful quick_check.
					startupExec(t, db, `ALTER TABLE active_config RENAME TO old_config;
					CREATE TABLE active_config(singleton INTEGER PRIMARY KEY, revision INTEGER NOT NULL, document TEXT NOT NULL, applied_at TEXT NOT NULL, applied_by TEXT NOT NULL) STRICT;
					INSERT INTO active_config SELECT * FROM old_config; DROP TABLE old_config;`)
				}
				startupExec(t, db, "UPDATE active_config SET document=?", string(document))
				if check := scalar[string](t, db, "PRAGMA quick_check"); check != "ok" {
					t.Fatalf("fixture must reach decoder, quick_check=%q", check)
				}
				db.Close()
				before := startupState(t, site)
				for _, addr := range addresses {
					startupReject(t, site, addr, 4, "active configuration", "invalid")
				}
				if got := startupState(t, site); got != before {
					t.Error("invalid definition rejection changed logical state or schema")
				}
			})
		}
	})
	t.Run("unbounded_order_starts_without_rewriting", func(t *testing.T) {
		site := initSite(t, true)
		db := openDB(t, site)
		document := scalar[string](t, db, "SELECT document FROM active_config")
		startupExec(t, db, "UPDATE active_config SET document=?", strings.Replace(document, `"order": 10`, `"order": `+strings.Repeat("9", 400), 1))
		db.Close()
		before := startupState(t, site)
		url, stop := serve(t, site)
		_, html := request(t, url, "GET", "/example", "", 200)
		stop()
		if !strings.Contains(html, "Example Page") {
			t.Error("supported large-order config did not render stored page")
		}
		if got := startupState(t, site); got != before {
			t.Error("startup rewrote exact order or committed state")
		}
	})
}
