package integration_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/auth"
)

// Additive regression after ticket 72's round-1 review. The original account
// and missing/empty-password writer tests remain unchanged.
const admin72EmailPassword = " \tfixture-email72-password-é!\r\n "
const admin72EmailSecret = "harmless-email72-secret-sentinel"

// Run the real CLI with only harmless fixture environment values. Keep output
// out of diagnostics even if a broken CLI prints a credential or environment.
func admin72EmailProcess(t *testing.T, executable, coverageDir string, args ...string) (int, string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, append([]string{"init"}, args...)...)
	cmd.WaitDelay = time.Second
	cmd.Env = []string{
		"COMPOSURE_ADMIN_PASSWORD=" + admin72EmailPassword,
		"COMPOSURE_TEST_SECRET=" + admin72EmailSecret,
	}
	if coverageDir != "" {
		cmd.Env = append(cmd.Env, "GOCOVERDIR="+coverageDir)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	for _, output := range []string{stdout.String(), stderr.String()} {
		for _, secret := range []string{admin72EmailPassword, strings.TrimSpace(admin72EmailPassword), admin72EmailSecret, "$argon2"} {
			if strings.Contains(output, secret) {
				t.Fatal("init exposed a fixture password, secret, or hash; output withheld")
			}
		}
	}
	if ctx.Err() != nil {
		t.Fatal("init exceeded the process deadline")
	}
	code := 0
	if err != nil {
		exited, ok := err.(*exec.ExitError)
		if !ok || exited.ExitCode() < 0 {
			t.Fatal("init did not exit normally")
		}
		code = exited.ExitCode()
	}
	return code, stdout.String(), stderr.String()
}

func admin72EmailRefusal(t *testing.T, code int, stdout, stderr string) {
	t.Helper()
	if code != 3 {
		t.Errorf("canonical-empty administrator email: exit %d, want invalid-input refusal 3", code)
	}
	if stdout != "" {
		t.Error("canonical-empty administrator email emitted stdout; validation must precede planning output")
	}
	line := strings.TrimSuffix(stderr, "\n")
	if !strings.HasPrefix(line, "composure init: ") || strings.ContainsAny(line, "\r\n") || !strings.Contains(line, "email") {
		t.Error("canonical-empty email must report one prefixed email-validation error; output withheld")
	}
	if strings.Contains(stderr, "flag provided but not defined") {
		t.Error("required admin-email flag is missing; email validation was not exercised")
	}
}

// Snapshot both the destination and its parent. No database means no seeded
// account/schema/config/example. Identity and mtime also catch replacement or
// temporary file creation followed by cleanup in an already-empty directory.
func admin72EmailDestination(t *testing.T, empty bool) (string, func()) {
	t.Helper()
	parent := t.TempDir()
	dir := filepath.Join(parent, "site")
	if empty {
		if err := os.Mkdir(dir, 0750); err != nil {
			t.Fatal(err)
		}
	}
	parentBefore, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	var before os.FileInfo
	if empty {
		before, err = os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
	}
	return dir, func() {
		t.Helper()
		parentAfter, err := os.Stat(parent)
		if err != nil {
			t.Fatal("init removed the destination parent")
		}
		if !os.SameFile(parentBefore, parentAfter) || parentBefore.Mode() != parentAfter.Mode() || !parentBefore.ModTime().Equal(parentAfter.ModTime()) {
			t.Error("refused init changed the destination parent")
		}
		if !empty {
			if _, err := os.Lstat(dir); !os.IsNotExist(err) {
				t.Error("canonical-empty email created a previously absent site")
			}
			if len(entries(t, parent)) != 0 {
				t.Error("canonical-empty email left site storage or temporary files in the parent")
			}
			return
		}
		after, err := os.Stat(dir)
		if err != nil {
			t.Fatal("init removed the already-empty site directory")
		}
		if !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
			t.Error("refused init changed the already-empty site directory")
		}
		if strings.Join(entries(t, parent), ",") != "site" || len(entries(t, dir)) != 0 {
			t.Error("canonical-empty email seeded site storage or left temporary files")
		}
	}
}

func TestAdminInitEmailValidationRejectsCanonicalEmpty(t *testing.T) {
	for _, email := range []struct{ name, value string }{{"explicit_empty", ""}, {"ascii_spaces", "   "}} {
		for _, apply := range []bool{false, true} {
			for _, empty := range []bool{false, true} {
				for _, selected := range []bool{false, true} {
					name := email.name + "/" + map[bool]string{false: "plan", true: "apply"}[apply] + "/" + map[bool]string{false: "absent", true: "empty"}[empty] + "/" + map[bool]string{false: "default_config", true: "selected_config"}[selected]
					t.Run(name, func(t *testing.T) {
						dir, preserved := admin72EmailDestination(t, empty)
						// Equals syntax distinguishes an explicitly empty value from
						// omitting the optional flag. Request seed content as well.
						args := []string{"--site", dir, "--example", "--admin-email=" + email.value}
						if apply {
							args = append(args, "--apply")
						}
						if selected {
							args = append(args, "--config", filepath.Join(root, "docs/phase1/examples/page-config-custom.json"))
						}
						code, stdout, stderr := admin72EmailProcess(t, binary, "", args...)
						admin72EmailRefusal(t, code, stdout, stderr)
						preserved()
					})
				}
			}
		}
	}
}

func TestAdminInitEmailValidationCanonicalEmailCompatibility(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(map[bool]string{false: "plan", true: "apply"}[apply], func(t *testing.T) {
			dir, preserved := admin72EmailDestination(t, true)
			args := []string{"--site", dir, "--example", "--admin-email", "  FIRST.ADMIN+EMAIL72@EXAMPLE.TEST  "}
			if apply {
				args = append(args, "--apply")
			}
			code, stdout, stderr := admin72EmailProcess(t, binary, "", args...)
			if code != 0 || stderr != "" || !strings.Contains(stdout, "Plan: initialize") {
				t.Fatal("valid canonicalizable email was not accepted; output withheld")
			}
			if !apply {
				if !strings.Contains(stdout, "No changes made") {
					t.Error("valid email planning did not report no changes")
				}
				preserved()
				return
			}
			repository := admin72Open(t, dir)
			defer admin72Close(t, repository)
			accounts, err := repository.ListAccounts(context.Background())
			if err != nil || len(accounts) != 1 {
				t.Fatal("valid email must create exactly one account")
			}
			account := accounts[0]
			if account.Email != "first.admin+email72@example.test" || account.State != "active" || !account.IsAdministrator || !account.IsEditor {
				t.Error("valid email lost ASCII canonicalization, dots/plus-tag, state, or roles")
			}
			if ok, err := auth.Verify(admin72EmailPassword, account.PasswordHash); err != nil || !ok {
				t.Error("password hash does not verify the exact opaque environment bytes")
			}
			if ok, err := auth.Verify(strings.TrimSpace(admin72EmailPassword), account.PasswordHash); err != nil || ok {
				t.Error("password hash accepted trimmed password bytes or has an invalid encoding")
			}
		})
	}
}

func TestAdminInitEmailValidationLegacyWithoutFlag(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(map[bool]string{false: "plan", true: "apply"}[apply], func(t *testing.T) {
			dir, preserved := admin72EmailDestination(t, false)
			args := []string{"--site", dir, "--example"}
			if apply {
				args = append(args, "--apply")
			}
			code, stdout, stderr := admin72EmailProcess(t, binary, "", args...)
			if code != 0 || stderr != "" || !strings.Contains(stdout, "Plan: initialize") {
				t.Fatal("legacy init without admin-email was refused; output withheld")
			}
			if !apply {
				preserved()
				return
			}
			repository := admin72Open(t, dir)
			defer admin72Close(t, repository)
			accounts, err := repository.ListAccounts(context.Background())
			if err != nil || len(accounts) != 0 {
				t.Error("legacy init must not seed an administrator from the password environment alone")
			}
		})
	}
}

// Coverage observes actual entry into auth.Hash in a separately built real CLI.
// This avoids timing assertions, entropy tricks, mocks, and production hooks.
// Include main in -coverpkg so the compiler installs the coverage exit hook.
// Application coverage then records counters on normal os.Exit, including refusal.
func TestAdminInitEmailValidationBeforePasswordHashing(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "composure-covered")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-cover", "-coverpkg=github.com/markdlabrecque/composure/internal/auth,github.com/markdlabrecque/composure/cmd/composure", "-o", executable, "./cmd/composure")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if _, err := build.CombinedOutput(); err != nil {
		t.Fatal("could not build coverage-instrumented real CLI")
	}
	for _, tc := range []struct {
		name, email string
		apply, hash bool
	}{
		{"empty_plan", "", false, false},
		{"empty_apply", "", true, false},
		{"spaces_plan", "   ", false, false},
		{"spaces_apply", "   ", true, false},
		{"valid_plan_control", admin72Email, false, false},
		{"valid_apply_control", admin72Email, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			coverageDir := t.TempDir()
			dir, preserved := admin72EmailDestination(t, false)
			args := []string{"--site", dir, "--admin-email=" + tc.email}
			if tc.apply {
				args = append(args, "--apply")
			}
			code, stdout, stderr := admin72EmailProcess(t, executable, coverageDir, args...)
			if tc.email == "" || tc.email == "   " {
				admin72EmailRefusal(t, code, stdout, stderr)
				preserved()
			} else if code != 0 || stderr != "" {
				t.Error("coverage positive control refused valid email; output withheld")
			}
			reportCtx, reportCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer reportCancel()
			report := exec.CommandContext(reportCtx, "go", "tool", "covdata", "func", "-i="+coverageDir)
			output, err := report.CombinedOutput()
			if err != nil {
				t.Fatal("could not read CLI function coverage")
			}
			found := false
			for _, line := range strings.Split(string(output), "\n") {
				fields := strings.Fields(line)
				if len(fields) != 3 || fields[1] != "Hash" {
					continue
				}
				found = true
				ran := fields[2] != "0.0%"
				t.Logf("auth.Hash executed=%t, want %t", ran, tc.hash)
				if ran != tc.hash {
					t.Errorf("auth.Hash executed=%t, want %t; email validation must precede password hashing", ran, tc.hash)
				}
			}
			if !found {
				t.Fatal("coverage did not report auth.Hash; no hashing-order evidence")
			}
		})
	}
}
