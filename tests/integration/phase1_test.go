package integration_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This is the first vertical slice: the CLI itself creates the seed, then the
// real HTTP process renders it. No fixture supplies initialization behavior.
func TestPhase1InitializeAndServe(t *testing.T) {
	site := initSite(t, true)
	if got := strings.Join(entries(t, site), ","); got != "composure.db" {
		t.Fatalf("closed atomic init left files %q", got)
	}
	db := openDB(t, site)
	ids := verifySeed(t, db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	url, stop := serve(t, site)
	header, first := request(t, url, "GET", "/example", "", 200)
	if !strings.HasPrefix(header.Get("Content-Type"), "text/html") {
		t.Errorf("Content-Type=%q", header.Get("Content-Type"))
	}
	var seed struct {
		Title, Path string
		Fields      map[string]string
	}
	if err := json.Unmarshal(readFile(t, filepath.Join(root, "docs/phase1/examples/seed-page.json")), &seed); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, seed.Title) || !strings.Contains(first, seed.Fields["body"]) {
		t.Fatalf("public HTML does not contain stored seed: %q", first)
	}
	request(t, url, "GET", "/unknown", "", 404)
	stop()
	db = openDB(t, site)
	if got := verifySeed(t, db); got != ids {
		t.Errorf("IDs changed after restart: %v -> %v", ids, got)
	}
	db.Close()
	url, stop = serve(t, site)
	_, second := request(t, url, "GET", "/example", "", 200)
	if first != second {
		t.Error("published HTML changed after restart")
	}
	stop()
}

func verifySeed(t *testing.T, db *sql.DB) [3]string {
	t.Helper()
	verifySchema(t, db)
	var siteID, created string
	var format int
	if err := db.QueryRow(`SELECT site_id,created_at,config_format_version FROM site WHERE singleton=1`).Scan(&siteID, &created, &format); err != nil {
		t.Fatal(err)
	}
	if format != 1 {
		t.Errorf("config_format_version=%d", format)
	}
	var document, applied, actor string
	var revision int
	if err := db.QueryRow(`SELECT revision,document,applied_at,applied_by FROM active_config WHERE singleton=1`).Scan(&revision, &document, &applied, &actor); err != nil {
		t.Fatal(err)
	}
	if revision != 1 || actor != "local-prototype" {
		t.Errorf("config revision=%d actor=%q", revision, actor)
	}
	equalJSON(t, document, readFile(t, filepath.Join(root, "docs/phase1/examples/page-config-default.json")))
	for _, table := range []string{"site", "active_config", "items", "snapshots", "routes"} {
		if n := scalar[int](t, db, "SELECT count(*) FROM "+table); n != 1 {
			t.Errorf("%s has %d rows, want 1", table, n)
		}
	}
	var itemID, snapshotID, title, path, fields, itemCreated, itemUpdated, creator, updater, typ string
	var draft int
	if err := db.QueryRow(`SELECT id,published_snapshot_id,type_id,title,path,fields,draft_revision,created_at,created_by,updated_at,updated_by FROM items`).Scan(&itemID, &snapshotID, &typ, &title, &path, &fields, &draft, &itemCreated, &creator, &itemUpdated, &updater); err != nil {
		t.Fatal(err)
	}
	var seed struct {
		Title, Path string
		Fields      map[string]string
	}
	if err := json.Unmarshal(readFile(t, filepath.Join(root, "docs/phase1/examples/seed-page.json")), &seed); err != nil {
		t.Fatal(err)
	}
	wantFields, _ := json.Marshal(seed.Fields)
	equalJSON(t, fields, wantFields)
	if title != seed.Title || path != seed.Path || typ != "page" || draft != 1 || creator != "local-prototype" || updater != creator {
		t.Errorf("unexpected seed draft: %q %q %q revision=%d actors=%q/%q", title, path, typ, draft, creator, updater)
	}
	var linkedItem, snapshotType, snapshotTitle, snapshotPath, snapshotFields, published, publisher string
	var seq, configRev, sourceRev int
	if err := db.QueryRow(`SELECT item_id,type_id,seq,config_revision,source_draft_revision,title,path,fields,published_at,published_by FROM snapshots WHERE id=?`, snapshotID).Scan(&linkedItem, &snapshotType, &seq, &configRev, &sourceRev, &snapshotTitle, &snapshotPath, &snapshotFields, &published, &publisher); err != nil {
		t.Fatal(err)
	}
	if linkedItem != itemID || snapshotType != "page" || seq != 1 || configRev != 1 || sourceRev != 1 || snapshotTitle != seed.Title || snapshotPath != seed.Path || publisher != "local-prototype" {
		t.Errorf("snapshot does not match seeded publication")
	}
	equalJSON(t, snapshotFields, wantFields)
	var routePath, kind, routeItem, claimed string
	if err := db.QueryRow(`SELECT path,kind,item_id,claimed_at FROM routes`).Scan(&routePath, &kind, &routeItem, &claimed); err != nil {
		t.Fatal(err)
	}
	if routePath != seed.Path || kind != "item" || routeItem != itemID {
		t.Error("route does not resolve seed item")
	}
	for _, id := range []string{siteID, itemID, snapshotID} {
		if !uuid7.MatchString(id) {
			t.Errorf("not UUIDv7: %q", id)
		}
	}
	if siteID == itemID || itemID == snapshotID || siteID == snapshotID {
		t.Error("entity IDs must be distinct")
	}
	if !timestamp.MatchString(created) {
		t.Errorf("not UTC millisecond timestamp: %q", created)
	}
	for _, at := range []string{applied, itemCreated, itemUpdated, published, claimed} {
		if at != created {
			t.Errorf("init timestamp %q differs from %q", at, created)
		}
	}
	return [3]string{siteID, itemID, snapshotID}
}

func verifySchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for query, want := range map[string]int{"PRAGMA application_id": 0x434D5053, "PRAGMA user_version": 1} {
		if got := scalar[int](t, db, query); got != want {
			t.Errorf("%s=%d, want %d", query, got, want)
		}
	}
	if got := scalar[string](t, db, "PRAGMA journal_mode"); got != "wal" {
		t.Errorf("journal mode=%q", got)
	}
	if got := scalar[string](t, db, "PRAGMA quick_check"); got != "ok" {
		t.Errorf("integrity=%q", got)
	}
	for _, table := range []string{"site", "active_config", "items", "snapshots", "routes"} {
		if got := scalar[int](t, db, "SELECT strict FROM pragma_table_list WHERE name='"+table+"'"); got != 1 {
			t.Errorf("%s is not STRICT", table)
		}
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name IN ('snapshots_no_update','snapshots_no_delete')`); n != 2 {
		t.Errorf("immutable snapshot triggers=%d", n)
	}
	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Error("foreign key violations")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestInitPlanChangesNothing(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "empty"}[existing], func(t *testing.T) {
			site := filepath.Join(t.TempDir(), "site")
			if existing {
				if err := os.Mkdir(site, 0755); err != nil {
					t.Fatal(err)
				}
			}
			out := cli(t, 0, "init", "--site", site, "--example")
			for _, fragment := range []string{"Plan: initialize a Composure site", site, filepath.Join(site, "composure.db"), "schema v1", "config format v1", "revision 1", "/example", "No changes made"} {
				if !strings.Contains(out, fragment) {
					t.Errorf("plan missing %q: %q", fragment, out)
				}
			}
			if existing {
				if len(entries(t, site)) != 0 {
					t.Error("plan mutated empty directory")
				}
			} else {
				noSite(t, site)
			}
		})
	}
}

func TestInitWithoutExample(t *testing.T) {
	site := initSite(t, false)
	db := openDB(t, site)
	verifySchema(t, db)
	if scalar[int](t, db, "SELECT count(*) FROM site") != 1 || scalar[int](t, db, "SELECT revision FROM active_config") != 1 {
		t.Error("ordinary init missing site/config")
	}
	equalJSON(t, scalar[string](t, db, "SELECT document FROM active_config"), readFile(t, filepath.Join(root, "docs/phase1/examples/page-config-default.json")))
	for _, table := range []string{"items", "snapshots", "routes"} {
		if n := scalar[int](t, db, "SELECT count(*) FROM "+table); n != 0 {
			t.Errorf("ordinary init seeded %s with %d rows", table, n)
		}
	}
	db.Close()
	url, _ := serve(t, site)
	request(t, url, "GET", "/example", "", 404)
}

func TestInitRefusesNonEmptySite(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(map[bool]string{false: "plan", true: "apply"}[apply], func(t *testing.T) {
			site := initSite(t, true)
			before := readFile(t, filepath.Join(site, "composure.db"))
			args := []string{"init", "--site", site, "--example"}
			if apply {
				args = append(args, "--apply")
			}
			cli(t, 3, args...)
			if !bytes.Equal(before, readFile(t, filepath.Join(site, "composure.db"))) {
				t.Error("reinit overwrote committed DB")
			}
			if got := strings.Join(entries(t, site), ","); got != "composure.db" {
				t.Errorf("rejected init left files %q", got)
			}
		})
	}
	t.Run("unrelated_file", func(t *testing.T) {
		site := t.TempDir()
		path := filepath.Join(site, "keep.txt")
		if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
		for _, apply := range []bool{false, true} {
			args := []string{"init", "--site", site}
			if apply {
				args = append(args, "--apply")
			}
			cli(t, 3, args...)
		}
		if string(readFile(t, path)) != "keep" || strings.Join(entries(t, site), ",") != "keep.txt" {
			t.Error("nonempty directory changed")
		}
	})
}

func TestSeparateSitesKeepIndependentContent(t *testing.T) {
	a, b := initSite(t, true), initSite(t, true)
	da, db := openDB(t, a), openDB(t, b)
	ia, ib := verifySeed(t, da), verifySeed(t, db)
	for i := range ia {
		if ia[i] == ib[i] {
			t.Errorf("separate sites share ID %q", ia[i])
		}
	}
	if _, err := da.Exec(`UPDATE items SET title='private draft A',fields='{"body":"private body A"}',draft_revision=2`); err != nil {
		t.Fatal(err)
	}
	da.Close()
	db.Close()
	ua, sa := serve(t, a)
	ub, sb := serve(t, b)
	_, ba := request(t, ua, "GET", "/example", "", 200)
	_, bb := request(t, ub, "GET", "/example", "", 200)
	if strings.Contains(ba, "private draft A") || strings.Contains(ba, "private body A") || ba != bb {
		t.Error("draft values leaked or site B changed")
	}
	sa()
	sb()
	// Restart after a draft edit must still resolve the immutable original snapshot.
	ua, _ = serve(t, a)
	_, again := request(t, ua, "GET", "/example", "", 200)
	if again != ba {
		t.Error("draft edit changed public snapshot on restart")
	}
}

func TestPublicBoundary(t *testing.T) {
	site := initSite(t, true)
	url, _ := serve(t, site)
	h, b := request(t, url, "GET", "/healthz", "", 200)
	if strings.TrimSpace(b) != "ok" || !strings.HasPrefix(h.Get("Content-Type"), "text/plain") {
		t.Errorf("healthz content %q %q", h.Get("Content-Type"), b)
	}
	_, b = request(t, url, "GET", "/example", "attacker.example", 400)
	if strings.Contains(b, "Example Page") {
		t.Error("unexpected Host rendered page")
	}
	for _, path := range []string{"/unknown", "/Example", "/example/"} {
		request(t, url, "GET", path, "", 404)
	}
	for _, path := range []string{"/example", "/healthz"} {
		h, _ = request(t, url, "POST", path, "", 405)
		if !strings.Contains(h.Get("Allow"), "GET") || !strings.Contains(h.Get("Allow"), "HEAD") {
			t.Errorf("Allow=%q", h.Get("Allow"))
		}
		_, b = request(t, url, "HEAD", path, "", 200)
		if b != "" {
			t.Error("HEAD sent body")
		}
	}
}

func TestServeRejectsMissingSiteAndNonLoopback(t *testing.T) {
	site := filepath.Join(t.TempDir(), "missing")
	cli(t, 3, "serve", "--site", site, "--addr", "127.0.0.1:0")
	noSite(t, site)
	empty := t.TempDir()
	cli(t, 3, "serve", "--site", empty, "--addr", "127.0.0.1:0")
	if len(entries(t, empty)) != 0 {
		t.Error("serve created missing database")
	}
	for _, addr := range []string{"0.0.0.0:0", "[::]:0", "192.0.2.1:0", "invalid"} {
		cli(t, 2, "serve", "--site", site, "--addr", addr)
		noSite(t, site)
	}
}

func TestServeRejectsRepresentativeIncompatibleAndForeignDatabase(t *testing.T) {
	for _, tc := range []struct {
		name, mutation string
		code           int
	}{{"newer_schema", "PRAGMA user_version=2", 4}, {"foreign_identity", "PRAGMA application_id=0", 3}} {
		t.Run(tc.name, func(t *testing.T) {
			site := initSite(t, true)
			db := openDB(t, site)
			if _, err := db.Exec(tc.mutation); err != nil {
				t.Fatal(err)
			}
			db.Close()
			before := readFile(t, filepath.Join(site, "composure.db"))
			cli(t, tc.code, "serve", "--site", site, "--addr", "127.0.0.1:0")
			if !bytes.Equal(before, readFile(t, filepath.Join(site, "composure.db"))) {
				t.Error("rejected startup mutated database")
			}
		})
	}
}

func TestInitIOFailureLeavesParentUntouched(t *testing.T) {
	parent := t.TempDir()
	file := filepath.Join(parent, "file")
	if err := os.WriteFile(file, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	cli(t, 1, "init", "--site", filepath.Join(file, "site"), "--example", "--apply")
	if string(readFile(t, file)) != "unchanged" || strings.Join(entries(t, parent), ",") != "file" {
		t.Error("failed init changed parent or left temporary files")
	}
}

func TestCIFailureProbe(t *testing.T) {
	if os.Getenv("COMPOSURE_CI_FAILURE_PROBE") == "1" {
		t.Fatal("deliberate Composure CI failure probe: real Go suite must fail")
	}
}

func TestInitAppliesToExistingEmptyDirectory(t *testing.T) {
	site := t.TempDir()
	cli(t, 0, "init", "--site", site, "--example", "--apply")
	if got := strings.Join(entries(t, site), ","); got != "composure.db" {
		t.Fatalf("applied empty directory left %q", got)
	}
	db := openDB(t, site)
	verifySeed(t, db)
}

func TestInitializedSnapshotsAreImmutable(t *testing.T) {
	site := initSite(t, true)
	db := openDB(t, site)
	before := scalar[string](t, db, "SELECT fields FROM snapshots")
	for _, query := range []string{`UPDATE snapshots SET title='changed'`, `DELETE FROM snapshots`} {
		if _, err := db.Exec(query); err == nil {
			t.Errorf("schema accepted mutation: %s", query)
		}
	}
	if got := scalar[string](t, db, "SELECT fields FROM snapshots"); got != before || scalar[int](t, db, "SELECT count(*) FROM snapshots") != 1 {
		t.Error("immutable snapshot changed")
	}
}
