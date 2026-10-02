// Package store is the only production package that opens SQLite or runs SQL.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/markdlabrecque/composure/internal/config"
	"github.com/markdlabrecque/composure/internal/content"
	_ "modernc.org/sqlite"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

const ApplicationID = 0x434D5053
const SchemaVersion = 1

type Store struct {
	db *sql.DB

	lifecycleMu sync.Mutex
	closing     bool
	workers     []*storeWorker
	closeDone   chan struct{}
	closeErr    error
	lifecycle   context.Context
	cancel      context.CancelFunc
}

// StateError identifies a rejected site without treating it as an I/O failure.
type StateError struct {
	Code    int
	Message string
}

func (e *StateError) Error() string { return e.Message }

func connect(path string) (*sql.DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}
	q := url.Values{"mode": {"rw"}, "_txlock": {"immediate"}, "_pragma": {"foreign_keys(1)", "busy_timeout(5000)", "synchronous(FULL)"}}
	uri.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Initialize writes only the already-reserved temporary database.
func Initialize(ctx context.Context, path, siteID string, at time.Time, example *content.Snapshot) (err error) {
	return InitializeWithConfig(ctx, path, siteID, at, config.Default, example)
}

// InitializeWithConfig creates a site with the validated configuration chosen
// by init. The document becomes the site's active revision 1.
func InitializeWithConfig(ctx context.Context, path, siteID string, at time.Time, document []byte, example *content.Snapshot) error {
	return InitializeWithConfigAndAccount(ctx, path, siteID, at, document, example, nil)
}

// InitializeWithConfigAndAccount creates a fresh site and, when supplied, its
// first administrator in the same initialization transaction.
func InitializeWithConfigAndAccount(ctx context.Context, path, siteID string, at time.Time, document []byte, example *content.Snapshot, firstAccount *AccountDraft) (err error) {
	db, err := connect(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	var journal string
	if err = db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&journal); err != nil {
		return err
	}
	if journal != "wal" {
		return fmt.Errorf("cannot enable WAL")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	timestamp := at.UTC().Format("2006-01-02T15:04:05.000Z")
	statements := []struct {
		query string
		args  []any
	}{
		{"PRAGMA application_id=1129140307", nil},
		{"PRAGMA user_version=1", nil},
		{`INSERT INTO site VALUES(1,?,?,1)`, []any{siteID, timestamp}},
		{`INSERT INTO active_config VALUES(1,1,?,?,?)`, []any{string(document), timestamp, "local-prototype"}},
	}
	migrations, err := fs.ReadDir(schemaFiles, "schema")
	if err != nil {
		return err
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Name() < migrations[j].Name() })
	for _, migration := range migrations {
		if migration.IsDir() {
			continue
		}
		query, readErr := schemaFiles.ReadFile("schema/" + migration.Name())
		if readErr != nil {
			return readErr
		}
		if _, err = tx.ExecContext(ctx, string(query)); err != nil {
			return err
		}
	}
	for _, s := range statements {
		if _, err = tx.ExecContext(ctx, s.query, s.args...); err != nil {
			return err
		}
	}
	if example != nil {
		fields, e := json.Marshal(example.Fields)
		if e != nil {
			return e
		}
		statements = []struct {
			query string
			args  []any
		}{
			{`INSERT INTO items(id,type_id,title,path,fields,draft_revision,created_at,created_by,updated_at,updated_by) VALUES(?,'page',?,?,?,1,?,'local-prototype',?,'local-prototype')`, []any{example.ItemID, example.Title, example.Path, string(fields), timestamp, timestamp}},
			{`INSERT INTO snapshots(id,item_id,seq,type_id,config_revision,source_draft_revision,title,path,fields,published_at,published_by) VALUES(?,?,1,'page',1,1,?,?,?,?,'local-prototype')`, []any{example.ID, example.ItemID, example.Title, example.Path, string(fields), timestamp}},
			{`UPDATE items SET published_snapshot_id=? WHERE id=?`, []any{example.ID, example.ItemID}},
			{`INSERT INTO routes VALUES(?,'item',?,?)`, []any{example.Path, example.ItemID, timestamp}},
		}
		for _, s := range statements {
			if _, err = tx.ExecContext(ctx, s.query, s.args...); err != nil {
				return err
			}
		}
	}
	if firstAccount != nil {
		accountID, idErr := content.NewID(at)
		if idErr != nil {
			return idErr
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO accounts(id,email,password_hash,is_administrator,is_editor,state,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?)`, accountID, normalizeAccountEmail(firstAccount.Email), firstAccount.PasswordHash,
			boolInt(firstAccount.IsAdministrator), boolInt(firstAccount.IsEditor), firstAccount.State, timestamp, timestamp)
		if err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	// Checkpoint before closing; publishing only the main file must retain all rows.
	var busy, log, checkpointed int
	if err = db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &log, &checkpointed); err != nil {
		return err
	}
	if busy != 0 || log != checkpointed {
		return fmt.Errorf("initial database checkpoint incomplete")
	}
	return nil
}

// Open checks identity, integrity and supported versions without running DDL.
func Open(ctx context.Context, path string) (*Store, error) {
	store, _, err := open(ctx, path, true, false)
	return store, err
}

func open(ctx context.Context, path string, validateDocument, readOnly bool) (*Store, ActiveConfig, error) {
	var active ActiveConfig
	db, err := connect(path)
	if err != nil {
		return nil, active, &StateError{3, fmt.Sprintf("%s is not a Composure site database: %v", path, err)}
	}
	ok := false
	defer func() {
		if !ok {
			db.Close()
		}
	}()
	var id, version, format int
	var integrity, journal, document string
	if err = db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&id); err != nil || id != ApplicationID {
		return nil, active, &StateError{3, fmt.Sprintf("%s is not a Composure site database", path)}
	}
	if err = db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&integrity); err != nil {
		return nil, active, &StateError{3, fmt.Sprintf("%s failed integrity check: %v", path, err)}
	}
	if err = db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil || integrity != "ok" || journal != "wal" {
		return nil, active, &StateError{3, fmt.Sprintf("%s failed integrity check: %s, journal mode %s", path, integrity, journal)}
	}
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return nil, active, err
	}
	if err = compatible("schema", version); err != nil {
		return nil, active, err
	}
	if err = db.QueryRowContext(ctx, "SELECT config_format_version FROM site WHERE singleton=1").Scan(&format); err != nil {
		return nil, active, &StateError{4, "active configuration is invalid: " + err.Error()}
	}
	if err = compatible("configuration format", format); err != nil {
		return nil, active, err
	}
	if err = db.QueryRowContext(ctx, "SELECT revision,document FROM active_config WHERE singleton=1").Scan(&active.Revision, &document); err != nil {
		return nil, active, &StateError{4, "active configuration is invalid: " + err.Error()}
	}
	active.Document = []byte(document)
	if validateDocument {
		if _, err = config.Decode(active.Document); err != nil {
			return nil, active, &StateError{4, "active configuration is invalid: " + err.Error()}
		}
	}
	if readOnly {
		if _, err = db.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
			return nil, active, err
		}
	}
	lifecycle, cancel := context.WithCancel(context.Background())
	ok = true
	return &Store{db: db, lifecycle: lifecycle, cancel: cancel, closeDone: make(chan struct{})}, active, nil
}
func compatible(kind string, version int) error {
	if version == 1 {
		return nil
	}
	if version < 1 {
		return &StateError{4, fmt.Sprintf("%s version %d is older than supported 1; a migration is required, which phase 1 does not provide", kind, version)}
	}
	return &StateError{4, fmt.Sprintf("%s version %d is newer than supported 1; use a newer composure binary", kind, version)}
}
func (s *Store) Close() error {
	s.lifecycleMu.Lock()
	if s.closing {
		done := s.closeDone
		s.lifecycleMu.Unlock()
		<-done
		return s.closeErr
	}
	s.closing = true
	s.cancel()
	workers := append([]*storeWorker(nil), s.workers...)
	s.lifecycleMu.Unlock()

	for _, worker := range workers {
		worker.cancel()
	}
	for _, worker := range workers {
		<-worker.done
	}
	err := s.db.Close()
	s.lifecycleMu.Lock()
	s.closeErr = err
	close(s.closeDone)
	s.lifecycleMu.Unlock()
	return err
}

func (s *Store) registerWorker(worker *storeWorker) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.closing {
		return errors.New("store is closing")
	}
	s.workers = append(s.workers, worker)
	return nil
}
func (s *Store) PublishedByPath(ctx context.Context, path string) (content.Snapshot, error) {
	var page content.Snapshot
	var fields string
	err := s.db.QueryRowContext(ctx, `SELECT s.id,s.item_id,s.title,s.path,s.fields FROM routes r JOIN items i ON i.id=r.item_id JOIN snapshots s ON s.id=i.published_snapshot_id AND s.item_id=i.id WHERE r.path=? AND r.kind='item'`, path).Scan(&page.ID, &page.ItemID, &page.Title, &page.Path, &fields)
	if errors.Is(err, sql.ErrNoRows) {
		return page, content.ErrNotFound
	}
	if err != nil {
		return page, err
	}
	err = json.Unmarshal([]byte(fields), &page.Fields)
	return page, err
}
