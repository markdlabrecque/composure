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
	"time"

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
	existing, err := empty(dir, "")
	if err != nil {
		return err
	}
	suffix := " (will be created)"
	if existing {
		suffix = " (existing empty directory)"
	}
	if _, err = fmt.Fprintf(out, "Plan: initialize a Composure site\n  site directory:  %s%s\n  database:        %s (schema v1, config format v1)\n  configuration:   built-in default, revision 1\n  content types:   page \"Page\" (fields: body)\n", dir, suffix, filepath.Join(dir, "composure.db")); err != nil {
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
	var page *content.Snapshot
	if example {
		page = &content.Snapshot{}
		if err = json.Unmarshal(seed, page); err != nil {
			return err
		}
		if page.ItemID, err = content.NewID(at); err != nil {
			return err
		}
		if page.ID, err = content.NewID(at); err != nil {
			return err
		}
	}
	if err = store.Initialize(ctx, tempPath, siteID, at, page); err != nil {
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
