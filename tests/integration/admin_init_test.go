package integration_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/auth"
	"github.com/markdlabrecque/composure/internal/content"
	"github.com/markdlabrecque/composure/internal/store"
)

// These are harmless test credentials, never operator input. Password bytes
// reach the real CLI only through its environment, not arguments or stdin.
const admin72Password = "  disposable-init-72-password-A!  "
const admin72ReplacementPassword = "disposable-init-72-password-B!"
const admin72Email = "first.admin+init72@example.test"

func admin72Command(t *testing.T, want int, password string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, append([]string{"init"}, args...)...)
	// Replace, rather than inherit, any operator password. Nil Stdin supplies
	// the null device, so successful initialization cannot depend on a prompt.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "COMPOSURE_ADMIN_PASSWORD=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "COMPOSURE_ADMIN_PASSWORD="+password)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	// Check before printing any diagnostic. Even a failing test must not log
	// the password or an encoded hash returned by a broken implementation.
	for _, output := range []string{stdout.String(), stderr.String()} {
		for _, credential := range []string{password, strings.TrimSpace(password), admin72Password, strings.TrimSpace(admin72Password), admin72ReplacementPassword, "$argon2id$"} {
			if credential != "" && strings.Contains(output, credential) {
				t.Fatal("init exposed a password or password hash in process output; output withheld")
			}
		}
	}
	if ctx.Err() != nil {
		t.Fatal("init timed out")
	}
	code := 0
	if err != nil {
		if exited, ok := err.(*exec.ExitError); ok {
			code = exited.ExitCode()
		} else {
			t.Fatal("could not run init process")
		}
	}
	if code != want {
		t.Fatalf("init: exit %d, want %d; stdout=%q stderr=%q", code, want, stdout.String(), stderr.String())
	}
	if want == 0 {
		if stderr.Len() != 0 {
			t.Errorf("successful init wrote stderr: %q", stderr.String())
		}
	} else {
		line := strings.TrimSuffix(stderr.String(), "\n")
		if !strings.HasPrefix(line, "composure init: ") || strings.ContainsAny(line, "\r\n") {
			t.Errorf("init must report one prefixed error line: %q", stderr.String())
		}
		if strings.Contains(stdout.String(), "Initialized site") {
			t.Error("refused init announced success")
		}
	}
	return stdout.String()
}

func admin72Open(t *testing.T, site string) *store.Store {
	t.Helper()
	repository, err := store.Open(context.Background(), filepath.Join(site, "composure.db"))
	if err != nil {
		t.Fatal("cannot open CLI-initialized site through store")
	}
	t.Cleanup(func() { _ = repository.Close() })
	return repository
}

func admin72Account(t *testing.T, repository *store.Store) store.Account {
	t.Helper()
	ctx := context.Background()
	accounts, err := repository.ListAccounts(ctx)
	if err != nil {
		t.Fatal("cannot list initialized accounts")
	}
	if len(accounts) != 1 {
		t.Fatalf("init created %d accounts, want exactly one", len(accounts))
	}
	account := accounts[0]
	if account.Email != admin72Email || account.State != "active" || !account.IsAdministrator || !account.IsEditor {
		t.Error("first account must have canonical email, active state, and both roles")
	}
	if !uuid7.MatchString(account.ID) || !timestamp.MatchString(account.CreatedAt) || !timestamp.MatchString(account.UpdatedAt) {
		t.Error("first account must have a server-assigned UUIDv7 and UTC millisecond timestamps")
	}
	if account.CreatedAt != account.UpdatedAt {
		t.Error("new account creation and update timestamps differ")
	}
	byEmail, err := repository.GetAccountByEmail(ctx, "  FIRST.ADMIN+INIT72@EXAMPLE.TEST  ")
	if err != nil || byEmail != account {
		t.Error("canonical email lookup did not return the first account")
	}
	byID, err := repository.GetAccount(ctx, account.ID)
	if err != nil || byID != account {
		t.Error("account ID lookup did not return the first account")
	}
	if account.PasswordHash == "" || account.PasswordHash == admin72Password || !strings.HasPrefix(account.PasswordHash, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Fatal("first account password is not stored with the accepted Argon2id profile")
	}
	// Verify also rejects noncanonical PHC encodings and incorrect salt/tag sizes.
	if ok, err := auth.Verify(admin72Password, account.PasswordHash); err != nil || !ok {
		t.Error("stored hash does not verify the exact password bytes supplied in the environment")
	}
	for _, wrong := range []string{strings.TrimSpace(admin72Password), admin72ReplacementPassword} {
		if ok, err := auth.Verify(wrong, account.PasswordHash); err != nil || ok {
			t.Error("stored password hash accepted different password bytes or has an invalid encoding")
		}
	}
	return account
}

func admin72Close(t *testing.T, repository *store.Store) {
	t.Helper()
	if err := repository.Close(); err != nil {
		t.Fatal("cannot close initialized store")
	}
}

func TestAdminInitCreatesFirstAccount(t *testing.T) {
	var previousHash string
	for _, tc := range []struct {
		name       string
		empty      bool
		configFile string
	}{
		{name: "absent_default"},
		{name: "empty_selected_config", empty: true, configFile: filepath.Join(root, "docs/phase1/examples/page-config-custom.json")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			site := filepath.Join(t.TempDir(), "site")
			if tc.empty {
				if err := os.Mkdir(site, 0755); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"--site", site, "--apply", "--example", "--admin-email", "  FIRST.ADMIN+INIT72@EXAMPLE.TEST  "}
			if tc.configFile != "" {
				args = append(args, "--config", tc.configFile)
			}
			admin72Command(t, 0, admin72Password, args...)
			if got := strings.Join(entries(t, site), ","); got != "composure.db" {
				t.Fatalf("closed init left files %q", got)
			}
			repository := admin72Open(t, site)
			first := admin72Account(t, repository)
			if first.PasswordHash == previousHash {
				t.Error("independent initializations reused a password hash instead of fresh salt")
			}
			previousHash = first.PasswordHash
			page, err := repository.PublishedByPath(context.Background(), "/example")
			if err != nil || page.ID == "" || page.ItemID == "" {
				t.Fatal("administrator setup lost the requested published example")
			}
			admin72Close(t, repository)
			// The CLI has exited and the first connection is closed. Reopening
			// proves the account and hash are persisted, not in-memory state.
			repository = admin72Open(t, site)
			if again := admin72Account(t, repository); again != first {
				t.Error("first account changed after closing and reopening storage")
			}
			again, err := repository.PublishedByPath(context.Background(), "/example")
			if err != nil || !reflect.DeepEqual(again, page) {
				t.Error("published example changed after reopening storage")
			}
			admin72Close(t, repository)
		})
	}
}

func TestAdminInitPlanChangesNothing(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "empty"}[empty], func(t *testing.T) {
			site := filepath.Join(t.TempDir(), "site")
			if empty {
				if err := os.Mkdir(site, 0755); err != nil {
					t.Fatal(err)
				}
			}
			out := admin72Command(t, 0, admin72Password, "--site", site, "--admin-email", admin72Email)
			if !strings.Contains(out, "No changes made") {
				t.Error("init without --apply did not report a no-change plan")
			}
			if empty {
				if len(entries(t, site)) != 0 {
					t.Error("administrator plan changed the empty site directory")
				}
			} else {
				noSite(t, site)
			}
		})
	}
}

func TestAdminInitRefusesExistingAccountWithoutChanges(t *testing.T) {
	site := filepath.Join(t.TempDir(), "site")
	admin72Command(t, 0, admin72Password, "--site", site, "--apply", "--example", "--admin-email", admin72Email)
	repository := admin72Open(t, site)
	first := admin72Account(t, repository)
	// Preserve real content created after init as well as its seed/config.
	draftID, err := repository.CreateItem(context.Background(), content.ItemDraft{
		Title: "Keep editorial work", Path: "/keep-work", Fields: map[string]string{"body": "Do not discard this draft"},
	}, time.Now())
	if err != nil || draftID == "" {
		t.Fatal("cannot create preservation fixture through store")
	}
	admin72Close(t, repository)
	before := readFile(t, filepath.Join(site, "composure.db"))
	for _, email := range []string{admin72Email, "another.admin@example.test"} {
		t.Run(email, func(t *testing.T) {
			admin72Command(t, 3, admin72ReplacementPassword, "--site", site, "--apply", "--example", "--admin-email", email)
			if !bytes.Equal(before, readFile(t, filepath.Join(site, "composure.db"))) {
				t.Error("refused administrator init changed the committed database")
			}
			if got := strings.Join(entries(t, site), ","); got != "composure.db" {
				t.Errorf("refused administrator init left files %q", got)
			}
			repository := admin72Open(t, site)
			if again := admin72Account(t, repository); again != first {
				t.Error("refused init replaced or changed the existing account")
			}
			draft, err := repository.GetItem(context.Background(), draftID)
			if err != nil || draft.Title != "Keep editorial work" || draft.Fields["body"] != "Do not discard this draft" || draft.Published {
				t.Error("refused init lost or changed existing editorial data")
			}
			admin72Close(t, repository)
		})
	}
}

func TestAdminInitRefusesUnrelatedFilesWithoutChanges(t *testing.T) {
	site := t.TempDir()
	keep := filepath.Join(site, "keep.txt")
	if err := os.WriteFile(keep, []byte("unrelated operator data"), 0600); err != nil {
		t.Fatal(err)
	}
	admin72Command(t, 3, admin72Password, "--site", site, "--apply", "--admin-email", admin72Email)
	if string(readFile(t, keep)) != "unrelated operator data" || strings.Join(entries(t, site), ",") != "keep.txt" {
		t.Error("administrator init changed a non-empty destination")
	}
}
