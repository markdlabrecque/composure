package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
)

func (s *Store) ActiveConfig(ctx context.Context) (content.ActiveConfig, error) {
	var active content.ActiveConfig
	if err := s.db.QueryRowContext(ctx, "SELECT document,revision FROM active_config WHERE singleton=1").Scan(&active.Document, &active.Revision); err != nil {
		return active, err
	}
	return active, nil
}

func (s *Store) ListItems(ctx context.Context, typeID string) ([]content.ItemSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id,i.title,i.path,
			CASE WHEN i.published_snapshot_id IS NULL THEN 'Draft'
				 WHEN i.draft_revision>s.source_draft_revision THEN 'Changes pending'
				 ELSE 'Published' END,
			i.updated_at
		FROM items i
		LEFT JOIN snapshots s ON s.id=i.published_snapshot_id AND s.item_id=i.id
		WHERE i.type_id=?
		ORDER BY i.updated_at DESC,i.id`, typeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []content.ItemSummary
	for rows.Next() {
		var item content.ItemSummary
		if err := rows.Scan(&item.ID, &item.Title, &item.Path, &item.State, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetItem(ctx context.Context, id string) (content.Item, error) {
	var item content.Item
	var fields string
	var published sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id,type_id,title,path,fields,draft_revision,created_at,updated_at,published_snapshot_id
		FROM items WHERE id=?`, id).Scan(&item.ID, &item.TypeID, &item.Title, &item.Path, &fields, &item.Revision, &item.CreatedAt, &item.UpdatedAt, &published)
	if errors.Is(err, sql.ErrNoRows) {
		return item, content.ErrNotFound
	}
	if err != nil {
		return item, err
	}
	item.Published = published.Valid
	if err := json.Unmarshal([]byte(fields), &item.Fields); err != nil {
		return item, fmt.Errorf("decode Page fields: %w", err)
	}
	return item, nil
}

func (s *Store) CreateItem(ctx context.Context, draft content.ItemDraft, at time.Time) (string, error) {
	id, err := content.NewID(at)
	if err != nil {
		return "", err
	}
	fields, err := json.Marshal(draft.Fields)
	if err != nil {
		return "", err
	}
	timestamp := at.UTC().Format("2006-01-02T15:04:05.000Z")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var ownerID, ownerTitle string
	err = tx.QueryRowContext(ctx, `
		SELECT i.id,i.title FROM routes r JOIN items i ON i.id=r.item_id WHERE r.path=?`, draft.Path).Scan(&ownerID, &ownerTitle)
	if err == nil {
		return "", &content.PathTakenError{OwnerID: ownerID, OwnerTitle: ownerTitle}
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO items(id,type_id,title,path,fields,draft_revision,created_at,created_by,updated_at,updated_by,published_snapshot_id)
		VALUES(?,'page',?,?,?,1,?,'local-prototype',?,'local-prototype',NULL)`, id, draft.Title, draft.Path, string(fields), timestamp, timestamp)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

// SaveDraft applies a validated draft only when the caller's revision is current.
// The store connection is configured for BEGIN IMMEDIATE, so ownership checks,
// no-op detection, and the update share one serialized transaction.
func (s *Store) SaveDraft(ctx context.Context, id string, expectedRevision int, draft content.ItemDraft, at time.Time, actor string) (bool, error) {
	fields, err := json.Marshal(draft.Fields)
	if err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var current content.Item
	var currentFields string
	var published sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT id,type_id,title,path,fields,draft_revision,created_at,updated_at,published_snapshot_id
		FROM items WHERE id=?`, id).Scan(&current.ID, &current.TypeID, &current.Title, &current.Path,
		&currentFields, &current.Revision, &current.CreatedAt, &current.UpdatedAt, &published)
	if errors.Is(err, sql.ErrNoRows) {
		return false, content.ErrNotFound
	}
	if err != nil {
		return false, err
	}
	if current.Revision != expectedRevision {
		return false, content.ErrStaleDraft
	}

	if published.Valid {
		var ownedPath string
		err := tx.QueryRowContext(ctx, `SELECT path FROM routes WHERE item_id=?`, id).Scan(&ownedPath)
		if errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("published Page %q has no owned route", id)
		}
		if err != nil {
			return false, err
		}
		if draft.Path != ownedPath {
			return false, &content.PathChangeUnsupportedError{OwnedPath: ownedPath}
		}
	}

	var ownerID, ownerTitle string
	err = tx.QueryRowContext(ctx, `
		SELECT i.id,i.title FROM routes r JOIN items i ON i.id=r.item_id
		WHERE r.path=? AND i.id<>?`, draft.Path, id).Scan(&ownerID, &ownerTitle)
	if err == nil {
		return false, &content.PathTakenError{OwnerID: ownerID, OwnerTitle: ownerTitle}
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	var storedFields map[string]string
	if err := json.Unmarshal([]byte(currentFields), &storedFields); err != nil {
		return false, fmt.Errorf("decode Page fields: %w", err)
	}
	if current.Title == draft.Title && current.Path == draft.Path && equalFields(storedFields, draft.Fields) {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}

	timestamp := at.UTC().Format("2006-01-02T15:04:05.000Z")
	result, err := tx.ExecContext(ctx, `
		UPDATE items SET title=?,path=?,fields=?,draft_revision=draft_revision+1,updated_at=?,updated_by=?
		WHERE id=? AND draft_revision=?`, draft.Title, draft.Path, string(fields), timestamp, actor, id, expectedRevision)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected != 1 {
		return false, content.ErrStaleDraft
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func equalFields(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if other, ok := b[key]; !ok || other != value {
			return false
		}
	}
	return true
}
