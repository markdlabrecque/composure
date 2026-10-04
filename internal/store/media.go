package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
)

// MediaDraft contains metadata for bytes that have already been validated and stored.
// Width and Height are nil when the stored media has no image dimensions.
type MediaDraft struct {
	StorageName, Kind, CreatedBy string
	Size                         int64
	Width, Height                *int64
}

// Media is the persisted metadata for one stored media object.
type Media struct {
	ID                           string
	StorageName, Kind, CreatedBy string
	Size                         int64
	Width, Height                *int64
}

// CreateMedia records metadata for bytes that have already been validated and stored.
func (s *Store) CreateMedia(ctx context.Context, draft MediaDraft, at time.Time) (string, error) {
	id, err := content.NewID(at)
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO media(id, storage_name, kind, size, width, height, created_by)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, id, draft.StorageName, draft.Kind, draft.Size,
		draft.Width, draft.Height, draft.CreatedBy)
	if err != nil {
		return "", err
	}
	return id, nil
}

// GetMedia returns persisted metadata by its generated identifier.
func (s *Store) GetMedia(ctx context.Context, id string) (Media, error) {
	var media Media
	var width, height sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, storage_name, kind, size, width, height, created_by
		FROM media WHERE id = ?`, id).Scan(
		&media.ID, &media.StorageName, &media.Kind, &media.Size, &width, &height, &media.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return Media{}, content.ErrNotFound
	}
	if err != nil {
		return Media{}, err
	}
	if width.Valid {
		media.Width = &width.Int64
	}
	if height.Valid {
		media.Height = &height.Int64
	}
	return media, nil
}
