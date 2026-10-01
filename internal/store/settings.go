package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/markdlabrecque/composure/internal/content"
)

// GetSetting returns the value stored for key, or content.ErrNotFound when the
// key has not been set.
func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM site_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", content.ErrNotFound
	}
	return value, err
}

// SetSetting creates or replaces the value associated with key.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO site_settings(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
