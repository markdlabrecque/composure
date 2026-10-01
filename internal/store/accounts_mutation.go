package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
)

// ErrLastActiveAdministrator is returned when a protected account mutation
// would leave the site without an active administrator.
var ErrLastActiveAdministrator = errors.New("cannot remove the last active administrator")

// DeactivateAccount deactivates an active account and revokes all of its
// unrevoked sessions atomically. Deactivating an already-deactivated account
// is a no-op.
func (s *Store) DeactivateAccount(ctx context.Context, id string, at time.Time) error {
	return s.mutateAccount(ctx, id, at, func(tx *sql.Tx, state string, administrator bool) (bool, error) {
		if state == "deactivated" {
			return false, nil
		}
		if administrator {
			if err := requireOtherActiveAdministrator(ctx, tx); err != nil {
				return false, err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE accounts SET state='deactivated',updated_at=? WHERE id=? AND state='active'`, formatSessionTime(at), id); err != nil {
			return false, err
		}
		return true, nil
	})
}

// SetAccountRoles changes the independent administrator and editor flags and
// revokes all of the account's unrevoked sessions in the same transaction.
// Reapplying the current role flags is a no-op.
func (s *Store) SetAccountRoles(ctx context.Context, id string, administrator, editor bool, at time.Time) error {
	return s.mutateAccount(ctx, id, at, func(tx *sql.Tx, state string, currentAdministrator bool) (bool, error) {
		var currentEditor int
		if err := tx.QueryRowContext(ctx, `SELECT is_editor FROM accounts WHERE id=?`, id).Scan(&currentEditor); err != nil {
			return false, err
		}
		if currentAdministrator == administrator && (currentEditor == 1) == editor {
			return false, nil
		}
		if state == "active" && currentAdministrator && !administrator {
			if err := requireOtherActiveAdministrator(ctx, tx); err != nil {
				return false, err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE accounts SET is_administrator=?,is_editor=?,updated_at=? WHERE id=?`, boolInt(administrator), boolInt(editor), formatSessionTime(at), id); err != nil {
			return false, err
		}
		return true, nil
	})
}

// mutateAccount starts an immediate SQLite write transaction before inspecting
// the account or administrator count, so concurrent protected mutations
// serialize across Store instances. It commits the account change and session
// revocation together, and returns errors without leaving either partially
// applied.
func (s *Store) mutateAccount(ctx context.Context, id string, at time.Time, change func(*sql.Tx, string, bool) (bool, error)) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var state string
	var administrator int
	err = tx.QueryRowContext(ctx, `SELECT state,is_administrator FROM accounts WHERE id=?`, id).Scan(&state, &administrator)
	if errors.Is(err, sql.ErrNoRows) {
		return content.ErrNotFound
	}
	if err != nil {
		return err
	}
	changed, err := change(tx, state, administrator == 1)
	if err != nil || !changed {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE account_id=? AND revoked_at IS NULL`, formatSessionTime(at), id); err != nil {
		return err
	}
	return tx.Commit()
}

// requireOtherActiveAdministrator is called only while holding the write
// transaction's SQLite lock. The current target is one active administrator,
// so at least two total rows are required for its removal to be safe.
func requireOtherActiveAdministrator(ctx context.Context, tx *sql.Tx) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM accounts WHERE state='active' AND is_administrator=1`).Scan(&count); err != nil {
		return err
	}
	if count <= 1 {
		return ErrLastActiveAdministrator
	}
	return nil
}
