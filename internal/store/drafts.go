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
