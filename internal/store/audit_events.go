package store

import (
	"context"
	"database/sql"

	"github.com/markdlabrecque/composure/internal/audit"
)

// AppendAuditEvent stores one validated event using only the fields in the
// audit event contract. It does not coordinate the event with other writes.
func (s *Store) AppendAuditEvent(ctx context.Context, event audit.Event) error {
	if err := event.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var targetKind, targetID any
	if event.Target != nil {
		targetKind, targetID = event.Target.Kind, event.Target.ID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_events(
			id,time,action,actor,target_kind,target_id,outcome,count,
			failure_code,first_time,last_time
		) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		event.ID, event.Time, event.Action, event.Actor, targetKind, targetID,
		event.Outcome, event.Count, event.FailureCode, event.FirstTime, event.LastTime)
	return err
}

// ListAuditEvents returns persisted events ordered by event time, then ID.
func (s *Store) ListAuditEvents(ctx context.Context) ([]audit.Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id,time,action,actor,target_kind,target_id,outcome,count,
		       failure_code,first_time,last_time
		FROM audit_events
		ORDER BY time ASC, id ASC`)
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
