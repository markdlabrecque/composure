package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
)

type AccountDraft struct {
	Email, PasswordHash       string
	IsAdministrator, IsEditor bool
	State                     string
}

type Account struct {
	ID, Email, PasswordHash     string
	IsAdministrator, IsEditor   bool
	State, CreatedAt, UpdatedAt string
}

func (s *Store) CreateAccount(ctx context.Context, draft AccountDraft, at time.Time) (string, error) {
	id, err := content.NewID(at)
	if err != nil {
		return "", err
	}
	timestamp := at.UTC().Format("2006-01-02T15:04:05.000Z")
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO accounts(id,email,password_hash,is_administrator,is_editor,state,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?)`, id, normalizeAccountEmail(draft.Email), draft.PasswordHash,
		boolInt(draft.IsAdministrator), boolInt(draft.IsEditor), draft.State, timestamp, timestamp)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) GetAccount(ctx context.Context, id string) (Account, error) {
	var account Account
	err := scanAccount(s.db.QueryRowContext(ctx, `
		SELECT id,email,password_hash,is_administrator,is_editor,state,created_at,updated_at
		FROM accounts WHERE id=?`, id), &account)
	return account, err
}

func (s *Store) GetAccountByEmail(ctx context.Context, email string) (Account, error) {
	var account Account
	err := scanAccount(s.db.QueryRowContext(ctx, `
		SELECT id,email,password_hash,is_administrator,is_editor,state,created_at,updated_at
		FROM accounts WHERE email=?`, normalizeAccountEmail(email)), &account)
	return account, err
}

func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id,email,password_hash,is_administrator,is_editor,state,created_at,updated_at
		FROM accounts ORDER BY email,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []Account
	for rows.Next() {
		var account Account
		if err := scanAccount(rows, &account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

type accountRow interface {
	Scan(dest ...any) error
}

func scanAccount(row accountRow, account *Account) error {
	var administrator, editor int
	err := row.Scan(&account.ID, &account.Email, &account.PasswordHash, &administrator, &editor,
		&account.State, &account.CreatedAt, &account.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return content.ErrNotFound
	}
	if err != nil {
		return err
	}
	account.IsAdministrator = administrator == 1
	account.IsEditor = editor == 1
	return nil
}

func normalizeAccountEmail(email string) string {
	canonical := []byte(email)
	start, end := 0, len(canonical)
	for start < end && canonical[start] == ' ' {
		start++
	}
	for end > start && canonical[end-1] == ' ' {
		end--
	}
	canonical = canonical[start:end]
	for i, char := range canonical {
		if char >= 'A' && char <= 'Z' {
			canonical[i] = char + ('a' - 'A')
		}
	}
	return string(canonical)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
