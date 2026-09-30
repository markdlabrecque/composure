package integration_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func relativeSite(t *testing.T, absolute string) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, absolute)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.IsAbs(relative) {
		t.Fatal("fixture did not produce a relative site path")
	}
	return relative
}

func TestRelativeSiteCLI(t *testing.T) {
	t.Run("init_plan_and_apply", func(t *testing.T) {
		parent := t.TempDir()
		site := filepath.Join(parent, "site")
		relative := relativeSite(t, site)
		cli(t, 0, "init", "--site", relative, "--example")
		noSite(t, site)
		if len(entries(t, parent)) != 0 {
			t.Error("relative plan mutated parent")
		}
		cli(t, 0, "init", "--site", relative, "--example", "--apply")
		db := openDB(t, site)
		verifySeed(t, db)
		db.Close()
		if got := strings.Join(entries(t, site), ","); got != "composure.db" {
			t.Errorf("relative apply left %q", got)
		}
		url, stop := serve(t, relative)
		_, html := request(t, url, "GET", "/example", "", 200)
		if !strings.Contains(html, "Example Page") {
			t.Error("relative site did not render persisted seed")
		}
		stop()
	})
	t.Run("serve_existing", func(t *testing.T) {
		site := initSite(t, true)
		path := filepath.Join(site, "composure.db")
		before := readFile(t, path)
		t.Cleanup(func() {
			if !bytes.Equal(before, readFile(t, path)) {
				t.Error("relative serve changed committed database bytes")
			}
		})
		url, stop := serve(t, relativeSite(t, site))
		_, html := request(t, url, "GET", "/example", "", 200)
		if !strings.Contains(html, "Example Page") {
			t.Error("relative serve did not render persisted seed")
		}
		stop()
	})
}

func TestNoncanonicalRawPublicPathsReturn404(t *testing.T) {
	site := initSite(t, true)
	url, _ := serve(t, site)
	for _, path := range []string{"//example", "/a/../example", "/%65xample", "/%2fexample"} {
		t.Run(path, func(t *testing.T) {
			header, body := request(t, url, "GET", path, "", 404)
			if header.Get("Location") != "" {
				t.Errorf("noncanonical path corrected via Location=%q", header.Get("Location"))
			}
			if strings.Contains(body, "Example Page") {
				t.Error("noncanonical path rendered published page")
			}
		})
	}
}

func TestVersionWithoutSite(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "version")
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("version without --site must exit0: %v; stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("version stderr=%q", stderr.String())
	}
	output := stdout.String()
	for _, pattern := range []string{`(?im)\bcomposure\s+(?:version\s*[:=]?\s*)?[^\s]+`, `(?i)\bschema(?:\s+version)?\s*[:=]?\s*1\b`, `(?i)\bconfig(?:uration)?(?:\s+format)?(?:\s+version)?\s*[:=]?\s*1\b`} {
		if !regexp.MustCompile(pattern).MatchString(output) {
			t.Errorf("version missing binary/supported version matching %q: %q", pattern, output)
		}
	}
	if len(entries(t, dir)) != 0 {
		t.Error("version command mutated its working directory")
	}
}
