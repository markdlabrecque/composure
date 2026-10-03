package store

import (
	"context"

	"github.com/markdlabrecque/composure/internal/audit"
)

// Record appends one validated event and returns the append error, if any.
// A successful return means the SQLite insert committed; it does not make an
// unrelated domain-state change atomic with the audit event.
func (s *Store) Record(ctx context.Context, event audit.Event) error {
	return s.AppendAuditEvent(ctx, event)
}
