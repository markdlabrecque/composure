package integration_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/store"
	_ "modernc.org/sqlite"
)

var binary, root string

var adminFixtureMu sync.Mutex
var adminFixtureSites = map[string]string{}
var adminFixtureCookies = map[string]string{}

// TestMain builds the shipped CLI once, independently of the test binary's race/CGO mode.
func TestMain(m *testing.M) {
	var err error
	root, err = filepath.Abs("../..")
	if err != nil {
		panic(err)
	}
	dir, err := os.MkdirTemp("", "composure-process-")
	if err != nil {
		panic(err)
	}
	binary = filepath.Join(dir, "composure")
	build := exec.Command("go", "build", "-o", binary, "./cmd/composure")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "release build: %v\n%s", err, output)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func cli(t *testing.T, want int, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("CLI timed out: %v", args)
	}
	code := 0
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			code = e.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	if code != want {
		t.Fatalf("%v: exit %d, want %d; stdout=%q stderr=%q", args, code, want, out.String(), stderr.String())
	}
	if want != 0 {
		line := strings.TrimSuffix(stderr.String(), "\n")
		if !strings.HasPrefix(line, "composure "+args[0]+": ") || strings.Contains(line, "\n") {
			t.Errorf("error must be one prefixed stderr line: %q", stderr.String())
		}
		if strings.Contains(out.String(), "listening on") {
			t.Error("rejected command announced a listener")
		}
	}
	return out.String()
}

func initSite(t *testing.T, example bool) string {
	t.Helper()
	site := filepath.Join(t.TempDir(), "site")
	args := []string{"init", "--site", site, "--apply"}
	if example {
		args = append(args, "--example")
	}
	cli(t, 0, args...)
	if _, err := os.Stat(filepath.Join(site, "composure.db")); err != nil {
		t.Fatalf("applied init did not create database: %v", err)
	}
	return site
}

func openDB(t *testing.T, site string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(site, "composure.db"))+"?mode=rw&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func scalar[T any](t *testing.T, db *sql.DB, query string) T {
	t.Helper()
	var value T
	if err := db.QueryRow(query).Scan(&value); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return value
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func noSite(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected absent site %s, got %v", path, err)
	}
}

func entries(t *testing.T, site string) []string {
	t.Helper()
	files, err := os.ReadDir(site)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Name())
	}
	return names
}

// The stdout announcement is the readiness address; HTTP readiness has a deadline.
// Every started child is killed if needed and reaped, including failed starts.
func serve(t *testing.T, site string) (string, func()) {
	t.Helper()
	cmd := exec.Command(binary, "serve", "--site", site, "--addr", "127.0.0.1:0")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stopped := false
	stop := func() {
		if !stopped {
			stopped = true
			_ = cmd.Process.Kill()
			<-done
		}
	}
	t.Cleanup(stop)
	addresses := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "listening on http://127.0.0.1:") {
				select {
				case addresses <- strings.TrimPrefix(line, "listening on "):
				default:
				}
			}
		}
	}()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	var url string
	select {
	case url = <-addresses:
	case err := <-done:
		stopped = true
		t.Fatalf("server exited before listening: %v; stderr=%q", err, stderr.String())
	case <-deadline.C:
		t.Fatal("server did not announce ephemeral loopback address")
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	client := &http.Client{Timeout: time.Second}
	for {
		resp, err := client.Get(url + "/healthz")
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 && strings.TrimSpace(string(body)) == "ok" {
				adminFixtureMu.Lock()
				adminFixtureSites[url] = site
				adminFixtureMu.Unlock()
				return url, stop
			}
		}
		select {
		case err := <-done:
			stopped = true
			t.Fatalf("server exited before readiness: %v; stderr=%q", err, stderr.String())
		case <-deadline.C:
			t.Fatal("healthz readiness deadline exceeded")
		case <-ticker.C:
		}
	}
}

// adminFixtureCookie creates one real persisted session per disposable site.
// Integration HTTP requests use this controlled credential to keep earlier
// authenticated route acceptance tests meaningful after the admin guard lands.
func adminFixtureCookie(t *testing.T, base string) string {
	t.Helper()
	adminFixtureMu.Lock()
	defer adminFixtureMu.Unlock()
	site := adminFixtureSites[base]
	if site == "" {
		t.Fatalf("no site registered for integration server %q", base)
	}
	if credential := adminFixtureCookies[site]; credential != "" {
		return "__Host-composure_session=" + credential
	}

	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Millisecond)
	repository, err := store.Open(ctx, filepath.Join(site, "composure.db"))
	if err != nil {
		t.Fatalf("open admin fixture store: %v", err)
	}
	defer repository.Close()
	accounts, err := repository.ListAccounts(ctx)
	if err != nil {
		t.Fatalf("list admin fixture accounts: %v", err)
	}
	accountID := ""
	for _, account := range accounts {
		if account.State == "active" {
			accountID = account.ID
			break
		}
	}
	if accountID == "" {
		accountID, err = repository.CreateAccount(ctx, store.AccountDraft{
			Email: "integration-admin@example.test", PasswordHash: "unused-integration-fixture-hash",
			IsAdministrator: true, IsEditor: true, State: "active",
		}, at)
		if err != nil {
			t.Fatalf("create admin fixture account: %v", err)
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("create admin fixture credential: %v", err)
	}
	digest := sha256.Sum256(raw)
	if _, err := repository.CreateSession(ctx, store.SessionDraft{AccountID: accountID, TokenDigest: digest[:]}, at); err != nil {
		t.Fatalf("create admin fixture session: %v", err)
	}
	credential := base64.RawURLEncoding.EncodeToString(raw)
	adminFixtureCookies[site] = credential
	return "__Host-composure_session=" + credential
}

func request(t *testing.T, url, method, path, host string, status int) (http.Header, string) {
	t.Helper()
	req, err := http.NewRequest(method, url+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != status {
		t.Fatalf("%s %s Host=%q: status %d, want %d; body=%q", method, path, host, resp.StatusCode, status, body)
	}
	return resp.Header, string(body)
}

func equalJSON(t *testing.T, got string, want []byte) {
	t.Helper()
	var a, b any
	if err := json.Unmarshal([]byte(got), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &b); err != nil {
		t.Fatal(err)
	}
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if !bytes.Equal(aa, bb) {
		t.Errorf("JSON=%s, want %s", aa, bb)
	}
}

var uuid7 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var timestamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)
