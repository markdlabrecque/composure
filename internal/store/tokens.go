package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"io"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
)

// TokenDraft contains the purpose-bound data supplied when issuing a link.
// ExpiresAt is explicit: token lifetime policy belongs to the caller.
type TokenDraft struct {
	Purpose   string
	AccountID *string
	Email     *string
	ExpiresAt time.Time
}

// Token contains persisted token metadata. The raw credential is returned
// separately by IssueToken and is never stored.
type Token struct {
	ID, Purpose string
	TokenDigest []byte
	AccountID   *string
	Email       *string
	CreatedAt   string
	ExpiresAt   string
	UsedAt      *string
}

const tokenBytes = 32

func (s *Store) IssueToken(ctx context.Context, draft TokenDraft, at time.Time) (Token, string, error) {
	return s.issueTokenWithRandom(ctx, draft, at, rand.Reader)
}

// issueTokenWithRandom permits deterministic entropy-failure tests. Production
// issuance always supplies crypto/rand.Reader through IssueToken.
func (s *Store) issueTokenWithRandom(ctx context.Context, draft TokenDraft, at time.Time, random io.Reader) (Token, string, error) {
	if err := validateTokenDraft(draft, at); err != nil {
		return Token{}, "", err
	}
	if err := ctx.Err(); err != nil {
		return Token{}, "", err
	}
	if random == nil {
		return Token{}, "", errors.New("token random source is nil")
	}
	var raw [tokenBytes]byte
	if _, err := io.ReadFull(random, raw[:]); err != nil {
		return Token{}, "", err
	}
	id, err := content.NewID(at)
	if err != nil {
		return Token{}, "", err
	}
	digest := sha256.Sum256(raw[:])
	createdAt := formatSessionTime(at)
	token := Token{
		ID:          id,
		Purpose:     draft.Purpose,
		TokenDigest: append([]byte(nil), digest[:]...),
		AccountID:   cloneTokenString(draft.AccountID),
		Email:       cloneTokenString(draft.Email),
		CreatedAt:   createdAt,
		ExpiresAt:   formatSessionTime(draft.ExpiresAt),
	}
	if token.Purpose == "invitation" {
		canonical := normalizeAccountEmail(*draft.Email)
		token.Email = &canonical
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO tokens(id,purpose,token_digest,account_id,email,created_at,expires_at,used_at)
		VALUES(?,?,?,?,?,?,?,NULL)`, token.ID, token.Purpose, token.TokenDigest,
		token.AccountID, token.Email, token.CreatedAt, token.ExpiresAt)
	if err != nil {
		return Token{}, "", err
	}
	return token, base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func validateTokenDraft(draft TokenDraft, at time.Time) error {
	if draft.ExpiresAt.IsZero() || formatSessionTime(draft.ExpiresAt) <= formatSessionTime(at) {
		return errors.New("token expiry must be explicitly after issuance")
	}
	switch draft.Purpose {
	case "invitation":
		if draft.Email == nil || draft.AccountID != nil || normalizeAccountEmail(*draft.Email) == "" {
			return errors.New("invitation token requires an email and no account ID")
		}
	case "password_reset":
		if draft.AccountID == nil || *draft.AccountID == "" || draft.Email != nil {
			return errors.New("password-reset token requires an account ID and no email")
		}
	default:
		return errors.New("unknown token purpose")
	}
	return nil
}

func cloneTokenString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func scanToken(row interface{ Scan(...any) error }) (Token, error) {
	var token Token
	var accountID, email, usedAt sql.NullString
	err := row.Scan(&token.ID, &token.Purpose, &token.TokenDigest, &accountID, &email,
		&token.CreatedAt, &token.ExpiresAt, &usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Token{}, content.ErrNotFound
	}
	if err != nil {
		return Token{}, err
	}
	if accountID.Valid {
		token.AccountID = &accountID.String
	}
	if email.Valid {
		token.Email = &email.String
	}
	if usedAt.Valid {
		token.UsedAt = &usedAt.String
	}
	return token, nil
}

func (s *Store) GetTokenByTokenDigest(ctx context.Context, purpose string, digest []byte, now time.Time) (Token, error) {
	return scanToken(s.db.QueryRowContext(ctx, `
		SELECT id,purpose,token_digest,account_id,email,created_at,expires_at,used_at
		FROM tokens
		WHERE purpose=? AND token_digest=? AND used_at IS NULL AND expires_at>?`,
		purpose, digest, formatSessionTime(now)))
}

// consumeToken atomically claims a usable token and runs the store-domain
// operation it authorizes. It deliberately remains private so callers cannot
// use token consumption without a trusted operation in the same transaction.
func (s *Store) consumeToken(ctx context.Context, purpose string, digest []byte, now time.Time, operation func(*sql.Tx, Token) error) error {
	if operation == nil {
		return errors.New("token consumption requires an authorized operation")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	token, err := scanToken(tx.QueryRowContext(ctx, `
		SELECT id,purpose,token_digest,account_id,email,created_at,expires_at,used_at
		FROM tokens
		WHERE purpose=? AND token_digest=? AND used_at IS NULL AND expires_at>?`,
		purpose, digest, formatSessionTime(now)))
	if err != nil {
		return err
	}
	usedAt := formatSessionTime(now)
	result, err := tx.ExecContext(ctx, `
		UPDATE tokens SET used_at=?
		WHERE id=? AND used_at IS NULL AND expires_at>?`, usedAt, token.ID, usedAt)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return content.ErrNotFound
	}
	if err := operation(tx, token); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return tx.Commit()
}
