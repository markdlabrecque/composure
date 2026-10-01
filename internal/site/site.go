// Package site owns the site directory and explicit initialization lifecycle.
package site

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/markdlabrecque/composure/internal/auth"
	"github.com/markdlabrecque/composure/internal/config"
	"github.com/markdlabrecque/composure/internal/content"
	"github.com/markdlabrecque/composure/internal/store"
)

//go:embed seed.json
var seed []byte

const SchemaVersion = store.SchemaVersion

type Error struct {
	Code int
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }
func state(code int, format string, args ...any) error {
	return &Error{code, fmt.Errorf(format, args...)}
}

// Init prints the plan before mutation. now supplies one timestamp for all rows.
func Init(ctx context.Context, dir string, example, apply bool, out io.Writer, now func() time.Time) (err error) {
	return initWithConfig(ctx, dir, config.Default, false, example, apply, out, now, nil)
}

// AdminSetup requests creation of the first administrator during a fresh-site
// initialization. Password is treated as opaque input and is never rendered.
type AdminSetup struct {
	Email, Password string
}

// InitWithAdmin preserves the legacy default configuration while creating the
// first administrator as part of a real apply.
func InitWithAdmin(ctx context.Context, dir string, example, apply bool, admin AdminSetup, out io.Writer, now func() time.Time) error {
	return initWithConfig(ctx, dir, config.Default, false, example, apply, out, now, &admin)
}

// InitWithConfig validates the selected document and optional fixed example
// before inspecting, printing, or mutating the destination.
func InitWithConfig(ctx context.Context, dir string, rawConfig []byte, example, apply bool, out io.Writer, now func() time.Time) (err error) {
	return initWithConfig(ctx, dir, rawConfig, true, example, apply, out, now, nil)
}

// InitWithConfigAndAdmin initializes a fresh site and its first administrator.
func InitWithConfigAndAdmin(ctx context.Context, dir string, rawConfig []byte, example, apply bool, admin AdminSetup, out io.Writer, now func() time.Time) error {
	return initWithConfig(ctx, dir, rawConfig, true, example, apply, out, now, &admin)
}

func initWithConfig(ctx context.Context, dir string, rawConfig []byte, explicitConfig, example, apply bool, out io.Writer, now func() time.Time, admin *AdminSetup) (err error) {
	if admin != nil && admin.Password == "" {
		return state(3, "COMPOSURE_ADMIN_PASSWORD must be set and non-empty when --admin-email is supplied")
	}
	document, err := config.Validate(rawConfig)
	if err != nil {
		var validation *config.ValidationError
		if errors.As(err, &validation) {
			return &Error{Code: validation.ExitCode, Err: validation}
		}
		return &Error{Code: 3, Err: err}
	}
	canonical, err := config.Canonical(document)
	if err != nil {
		return err
	}
	definition := pageDefinition(document)
	var seedPage *content.Snapshot
	if example {
		seedPage, err = validateSeed(seed, definition)
		if err != nil {
			return &Error{Code: 3, Err: err}
		}
	}
	existing, err := empty(dir, "")
	if err != nil {
		return err
	}
	suffix := " (will be created)"
	if existing {
		suffix = " (existing empty directory)"
	}
	fieldSummaries := make([]string, 0, len(definition.Fields))
	for _, field := range definition.Fields {
		required := "optional"
		if field.Required {
			required = "required"
		}
		fieldSummaries = append(fieldSummaries, fmt.Sprintf("%s (%s, %q, %s, order %s, help %q)", field.ID, field.Kind, field.Label, required, field.Order, field.HelpText))
	}
	configuration := "built-in default"
	contentTypes := fmt.Sprintf("page %q (fields: body)", document.ContentTypes[0].Label)
	if explicitConfig {
		configuration = "selected definition"
		contentTypes = fmt.Sprintf("page %q (fields: %s)", document.ContentTypes[0].Label, strings.Join(fieldSummaries, ", "))
	}
	if _, err = fmt.Fprintf(out, "Plan: initialize a Composure site\n  site directory:  %s%s\n  database:        %s (schema v1, config format v1)\n  configuration:   %s, revision 1\n  content types:   %s\n", dir, suffix, filepath.Join(dir, "composure.db"), configuration, contentTypes); err != nil {
		return err
	}
	if example {
		_, err = fmt.Fprintln(out, "  example content: 1 published Page at /example")
	} else {
		_, err = fmt.Fprintln(out, "  example content: none")
	}
	if err != nil {
		return err
	}
	if !apply {
		_, err = fmt.Fprintln(out, "No changes made. Re-run with --apply to initialize.")
		return err
	}
	var firstAccount *store.AccountDraft
	if admin != nil {
		passwordHash, hashErr := auth.Hash(admin.Password)
		if hashErr != nil {
			return fmt.Errorf("cannot hash first administrator password")
		}
		firstAccount = &store.AccountDraft{
			Email: admin.Email, PasswordHash: passwordHash,
			IsAdministrator: true, IsEditor: true, State: "active",
		}
	}
	created := false
	if !existing {
		if err = os.Mkdir(dir, 0755); err != nil {
			return err
		}
		created = true
	}
	// Remove only our empty directory on failure, preserving any concurrent files.
	defer func() {
		if err != nil && created {
			_ = os.Remove(dir)
		}
	}()
	lockPath := filepath.Join(dir, ".composure-init.lock")
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return state(3, "site directory %s is not empty; phase 1 does not reinitialize sites", dir)
	}
	if err != nil {
		return err
	}
	if err = lock.Close(); err != nil {
		_ = os.Remove(lockPath)
		return err
	}
	defer func() { _ = os.Remove(lockPath) }()
	if _, err = empty(dir, filepath.Base(lockPath)); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(dir, "composure.db.init-")
	if err != nil {
		return err
	}
	tempPath := temporary.Name()
	if err = temporary.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	defer func() {
		for _, path := range []string{tempPath, tempPath + "-wal", tempPath + "-shm"} {
			_ = os.Remove(path)
		}
	}()
	at := now().UTC()
	siteID, err := content.NewID(at)
	if err != nil {
		return err
	}
	if seedPage != nil {
		seedPage.ItemID, err = content.NewID(at)
		if err != nil {
			return err
		}
		seedPage.ID, err = content.NewID(at)
		if err != nil {
			return err
		}
	}
	if err = store.InitializeWithConfigAndAccount(ctx, tempPath, siteID, at, canonical, seedPage, firstAccount); err != nil {
		return err
	}
	// Store closed all connections and checkpointed WAL before publication.
	for _, sidecar := range []string{tempPath + "-wal", tempPath + "-shm"} {
		if _, e := os.Stat(sidecar); !errors.Is(e, os.ErrNotExist) {
			return fmt.Errorf("temporary database still has sidecar %s", sidecar)
		}
	}
	file, err := os.OpenFile(tempPath, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if err = errors.Join(syncErr, closeErr); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(tempPath) && entry.Name() != filepath.Base(lockPath) {
			return state(3, "site directory %s is not empty; phase 1 does not reinitialize sites", dir)
		}
	}
	// Link publishes the same closed file atomically and never replaces a final
	// database created between the directory check and this operation. Unlike
	// os.Rename on Unix, this operation fails when the destination already exists.
	final := filepath.Join(dir, "composure.db")
	if err = os.Link(tempPath, final); err != nil {
		if errors.Is(err, os.ErrExist) {
			return state(3, "site directory %s is not empty; phase 1 does not reinitialize sites", dir)
		}
		return err
	}
	// Once published, preserve the valid database even if later output fails.
	if err = os.Remove(tempPath); err != nil {
		return err
	}
	if err = os.Remove(lockPath); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	syncErr = directory.Sync()
	closeErr = directory.Close()
	if err = errors.Join(syncErr, closeErr); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Initialized site %s at %s.\n", siteID, dir)
	return err
}

func pageDefinition(document config.Document) content.PageDefinition {
	definition := content.PageDefinition{}
	for _, field := range document.ContentTypes[0].Fields {
		definition.Fields = append(definition.Fields, content.FieldDefinition{
			ID: field.ID, Kind: field.Kind, Label: field.Label, HelpText: field.HelpText,
			Required: field.Required, Order: field.Order.String(),
		})
	}
	content.SortPageFields(definition.Fields)
	return definition
}

func validateSeed(data []byte, definition content.PageDefinition) (*content.Snapshot, error) {
	var page content.Snapshot
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, fmt.Errorf("field_value_invalid at $.fields: invalid fixed example seed")
	}
	allowed := make(map[string]bool, len(definition.Fields))
	for _, field := range definition.Fields {
		allowed[field.ID] = true
	}
	for id := range page.Fields {
		if !allowed[id] {
			return nil, fmt.Errorf("field_value_invalid at $.fields.%s: seed field is not configured", id)
		}
	}
	values := map[string]string{"title": page.Title, "path": page.Path}
	for id, value := range page.Fields {
		values[id] = value
	}
	draft, problems := content.PreparePageDraft(definition, values)
	if len(problems) > 0 {
		problem := problems[0]
		class := "field_value_invalid"
		if problem.Code == "required" {
			class = "required_value_missing"
		}
		return nil, fmt.Errorf("%s at $.fields.%s: fixed example seed failed %s validation", class, problem.Field, problem.Code)
	}
	seedFields := make(map[string]string, len(page.Fields))
	for id := range page.Fields {
		seedFields[id] = draft.Fields[id]
	}
	page.Title, page.Path, page.Fields = draft.Title, draft.Path, seedFields
	return &page, nil
}

func empty(dir, owned string) (bool, error) {
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return true, state(3, "site directory %s is not a directory", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true, err
	}
	for _, entry := range entries {
		if entry.Name() != owned {
			return true, state(3, "site directory %s is not empty; phase 1 does not reinitialize sites", dir)
		}
	}
	return true, nil
}

// Open checks an existing site without creating a directory or database.
func Open(ctx context.Context, dir string) (*store.Store, error) {
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, state(3, "site directory %s does not exist; run composure init --site %s", dir, dir)
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, state(3, "site directory %s is not a directory", dir)
	}
	path := filepath.Join(dir, "composure.db")
	info, err = os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, state(3, "site %s is not initialized (composure.db missing)", dir)
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, state(3, "%s is not a Composure site database", path)
	}
	repository, err := store.Open(ctx, path)
	var rejected *store.StateError
	if errors.As(err, &rejected) {
		return nil, &Error{rejected.Code, rejected}
	}
	return repository, err
}
