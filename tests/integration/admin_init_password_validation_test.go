package integration_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/auth"
	"github.com/markdlabrecque/composure/internal/site"
	"github.com/markdlabrecque/composure/internal/store"
)

// Run the shipped CLI with either no password key or an explicitly empty key.
// Never inherit the coordinator's password, and never print process output:
// an implementation bug must not leak inherited secrets into test diagnostics.
func admin72InvalidPasswordCommand(t *testing.T, empty bool, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, append([]string{"init"}, args...)...)
	cmd.WaitDelay = time.Second
	cmd.Env = make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "COMPOSURE_ADMIN_PASSWORD=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	if empty {
		cmd.Env = append(cmd.Env, "COMPOSURE_ADMIN_PASSWORD=")
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatal("init did not finish within the process deadline")
	}
	if err == nil {
		t.Error("init accepted an absent or empty administrator password; want nonzero refusal")
	} else if exited, ok := err.(*exec.ExitError); !ok || exited.ExitCode() <= 0 {
		t.Fatal("init did not exit normally with a nonzero refusal")
	}
	// The required flag is deliberately absent in the red baseline. A usage
	// failure is not evidence that password validation ran. This guard does
	// not prescribe the password-validation message or its exact exit code.
	if strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Error("init lacks a required flag; password validation was not exercised")
	}
}

func TestAdminInitRejectsMissingOrEmptyPasswordWithoutChanges(t *testing.T) {
	// Build an existing account fixture through exported APIs, not through
	// the missing CLI flag. Close storage before copying or comparing bytes.
	fixture := filepath.Join(t.TempDir(), "fixture")
	if err := site.Init(context.Background(), fixture, false, true, io.Discard, time.Now); err != nil {
		t.Fatal("cannot initialize preservation fixture")
	}
	hash, err := auth.Hash(admin72Password)
	if err != nil {
		t.Fatal("cannot hash harmless fixture password")
	}
	repository := admin72Open(t, fixture)
	if _, err := repository.CreateAccount(context.Background(), store.AccountDraft{
		Email: admin72Email, PasswordHash: hash,
		IsAdministrator: true, IsEditor: true, State: "active",
	}, time.Now()); err != nil {
		t.Fatal("cannot create preservation account fixture")
	}
	admin72Close(t, repository)
	databaseBefore := readFile(t, filepath.Join(fixture, "composure.db"))

	for _, password := range []struct {
		name  string
		empty bool
	}{{name: "unset"}, {name: "explicitly_empty", empty: true}} {
		for _, mode := range []struct {
			name  string
			apply bool
		}{{name: "plan"}, {name: "apply", apply: true}} {
			for _, destination := range []string{"absent", "empty", "existing_account"} {
				t.Run(password.name+"/"+mode.name+"/"+destination, func(t *testing.T) {
					parent := t.TempDir()
					dir := filepath.Join(parent, "site")
					if destination != "absent" {
						if err := os.Mkdir(dir, 0755); err != nil {
							t.Fatal(err)
						}
					}
					if destination == "existing_account" {
						if err := os.WriteFile(filepath.Join(dir, "composure.db"), databaseBefore, 0600); err != nil {
							t.Fatal(err)
						}
					}
					args := []string{"--site", dir, "--admin-email", admin72Email}
					if mode.apply {
						args = append(args, "--apply")
					}
					admin72InvalidPasswordCommand(t, password.empty, args...)
					switch destination {
					case "absent":
						// No directory means no final or temporary database, or account.
						noSite(t, dir)
						if len(entries(t, parent)) != 0 {
							t.Error("invalid password left files in the destination parent")
						}
					case "empty":
						if len(entries(t, dir)) != 0 {
							t.Error("invalid password created site storage or temporary files")
						}
					case "existing_account":
						if !bytes.Equal(databaseBefore, readFile(t, filepath.Join(dir, "composure.db"))) {
							t.Error("invalid password changed the existing database or account")
						}
						if strings.Join(entries(t, dir), ",") != "composure.db" {
							t.Error("invalid password left files beside the existing database")
						}
					}
				})
			}
		}
	}
}
