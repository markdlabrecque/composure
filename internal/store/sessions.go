package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
)

const sessionLifetime = 8 * time.Hour

type SessionDraft struct {
	AccountID   string
	TokenDigest []byte
}

type Session struct {
	ID, AccountID string
	TokenDigest   []byte
	CreatedAt     string
	ExpiresAt     string
	RevokedAt     *string
}

func (s *Store) CreateSession(ctx context.Context, draft SessionDraft, at time.Time) (string, error) {
	id, err := content.NewID(at)
	if err != nil {
		return "", err
	}
	createdAt := formatSessionTime(at)
	expiresAt := formatSessionTime(at.Add(sessionLifetime))
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO sessions(id,account_id,token_digest,created_at,expires_at)
		VALUES(?,?,?,?,?)`, id, draft.AccountID, draft.TokenDigest, createdAt, expiresAt)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) GetSessionByTokenDigest(ctx context.Context, digest []byte, now time.Time) (Session, error) {
	var session Session
	var revokedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT s.id,s.account_id,s.token_digest,s.created_at,s.expires_at,s.revoked_at
		FROM sessions s
		JOIN accounts a ON a.id=s.account_id
		WHERE s.token_digest=? AND s.revoked_at IS NULL AND s.expires_at>? AND a.state='active'`,
		digest, formatSessionTime(now)).Scan(&session.ID, &session.AccountID, &session.TokenDigest,
		&session.CreatedAt, &session.ExpiresAt, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, content.ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	if revokedAt.Valid {
		session.RevokedAt = &revokedAt.String
	}
	return session, nil
}

func (s *Store) RevokeSession(ctx context.Context, sessionID string, at time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET revoked_at=? WHERE id=? AND revoked_at IS NULL`,
		formatSessionTime(at), sessionID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id=?`, sessionID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return content.ErrNotFound
		} else if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RevokeAllSessions(ctx context.Context, accountID string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET revoked_at=? WHERE account_id=? AND revoked_at IS NULL`,
		formatSessionTime(at), accountID)
	return err
}

func formatSessionTime(at time.Time) string {
	return at.UTC().Format("2006-01-02T15:04:05.000Z")
}
