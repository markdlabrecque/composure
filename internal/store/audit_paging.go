package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/markdlabrecque/composure/internal/audit"
)

// ListAuditEventsPage returns a bounded page of persisted audit events,
// ordered newest first by event time and then by ID.
func (s *Store) ListAuditEventsPage(ctx context.Context, limit, offset int) ([]audit.Event, error) {
	if limit <= 0 {
		return nil, errors.New("audit event page limit must be positive")
	}
	if offset < 0 {
		return nil, errors.New("audit event page offset must be nonnegative")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id,time,action,actor,target_kind,target_id,outcome,count,
		       failure_code,first_time,last_time
		FROM audit_events
		ORDER BY time DESC, id DESC
		LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]audit.Event, 0)
	for rows.Next() {
		var event audit.Event
		var actor, targetKind, targetID sql.NullString
		var failureCode, firstTime, lastTime sql.NullString
		if err := rows.Scan(&event.ID, &event.Time, &event.Action, &actor,
			&targetKind, &targetID, &event.Outcome, &event.Count,
			&failureCode, &firstTime, &lastTime); err != nil {
			return nil, err
		}
		if actor.Valid {
			event.Actor = &actor.String
		}
		if targetKind.Valid {
			event.Target = &audit.Target{Kind: targetKind.String, ID: targetID.String}
		}
		if failureCode.Valid {
			event.FailureCode = &failureCode.String
		}
		if firstTime.Valid {
			event.FirstTime = &firstTime.String
		}
		if lastTime.Valid {
			event.LastTime = &lastTime.String
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}
