// Package audit defines the allowlisted event envelope and development/test
// recorder. Callers are responsible for sourcing every value from trusted,
// server-owned state; validation can check representation, not provenance.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Event is the version 1 audit event envelope. Optional counted-failure fields
// are present only when non-nil.
type Event struct {
	ID          string  `json:"id"`
	Time        string  `json:"time"`
	Action      string  `json:"action"`
	Actor       *string `json:"actor"`
	Target      *Target `json:"target"`
	Outcome     string  `json:"outcome"`
	Count       int64   `json:"count"`
	FailureCode *string `json:"failure_code,omitempty"`
	FirstTime   *string `json:"first_time,omitempty"`
	LastTime    *string `json:"last_time,omitempty"`
}

// Target identifies one affected resource or a fixed operation.
type Target struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

var (
	uuidV7Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	machineValue  = regexp.MustCompile(`^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9]*(?:_[a-z0-9]+)*)*$`)
	safeCode      = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
)

func validTime(value string) (time.Time, bool) {
	if len(value) != len("2006-01-02T15:04:05.000Z") || !strings.HasSuffix(value, "Z") {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02T15:04:05.000Z", value)
	if err != nil || t.UTC().Format("2006-01-02T15:04:05.000Z") != value {
		return time.Time{}, false
	}
	return t, true
}

// Validate checks the event's contract shape. It cannot establish that
// syntactically safe values were actually assigned by the server.
func (e Event) Validate() error {
	if !uuidV7Pattern.MatchString(e.ID) {
		return fmt.Errorf("audit event id must be a lowercase UUIDv7")
	}
	if _, ok := validTime(e.Time); !ok {
		return fmt.Errorf("audit event time must be UTC RFC 3339 with milliseconds")
	}
	if !machineValue.MatchString(e.Action) {
		return fmt.Errorf("audit event action must be a machine-readable identifier")
	}
	if e.Actor != nil && *e.Actor != "local-prototype" && !uuidV7Pattern.MatchString(*e.Actor) {
		return fmt.Errorf("audit event actor must be a stable actor id")
	}
	if e.Target != nil {
		if !machineValue.MatchString(e.Target.Kind) || e.Target.ID == "" || strings.TrimSpace(e.Target.ID) != e.Target.ID {
			return fmt.Errorf("audit event target must have a kind and id")
		}
		if e.Target.Kind == "operation" {
			if e.Target.ID != e.Action {
				return fmt.Errorf("audit operation target must match the action")
			}
		} else if !uuidV7Pattern.MatchString(e.Target.ID) {
			return fmt.Errorf("audit resource target id must be a lowercase UUIDv7")
		}
	}
	if e.Outcome != "success" && e.Outcome != "failure" {
		return fmt.Errorf("audit event outcome must be success or failure")
	}
	if e.Count <= 0 {
		return fmt.Errorf("audit event count must be positive")
	}
	if e.FailureCode != nil && !safeCode.MatchString(*e.FailureCode) {
		return fmt.Errorf("audit failure code must be a machine-readable value")
	}
	windowFields := e.FirstTime != nil || e.LastTime != nil
	if !windowFields {
		if e.Count != 1 {
			return fmt.Errorf("counted audit events require a time window")
		}
		return nil
	}
	if e.FirstTime == nil || e.LastTime == nil || e.Outcome != "failure" || e.Actor != nil {
		return fmt.Errorf("counted audit event requires a failure, null actor, and both window times")
	}
	first, firstOK := validTime(*e.FirstTime)
	last, lastOK := validTime(*e.LastTime)
	eventTime, eventOK := validTime(e.Time)
	if !firstOK || !lastOK || !eventOK || last.Before(first) || !eventTime.Equal(last) {
		return fmt.Errorf("counted audit event has an inconsistent time window")
	}
	return nil
}

// Recorder records one event. A successful return only reflects the sink's
// Write result; it does not promise stable storage or atomicity with other data.
type Recorder interface {
	Record(context.Context, Event) error
}

type logRecorder struct {
	writer io.Writer
	mu     sync.Mutex
}

// NewLogRecorder returns a JSON-lines recorder intended for development and
// tests. It makes no durability or state-commit atomicity guarantee.
func NewLogRecorder(w io.Writer) Recorder { return &logRecorder{writer: w} }

func (r *logRecorder) Record(ctx context.Context, event Event) error {
	if err := event.Validate(); err != nil {
		return fmt.Errorf("invalid audit event: %w", err)
	}
	if r.writer == nil {
		return fmt.Errorf("audit log write failed: nil writer")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("audit log write canceled: %w", err)
	}
	// Build a fresh value containing only contract fields. This keeps the sink
	// boundary explicitly allowlisted even if Event later gains internal data.
	projected := Event{
		ID: event.ID, Time: event.Time, Action: event.Action, Actor: cloneString(event.Actor),
		Outcome: event.Outcome, Count: event.Count, FailureCode: cloneString(event.FailureCode),
		FirstTime: cloneString(event.FirstTime), LastTime: cloneString(event.LastTime),
	}
	if event.Target != nil {
		projected.Target = &Target{Kind: event.Target.Kind, ID: event.Target.ID}
	}
	line, err := json.Marshal(projected)
	if err != nil {
		return fmt.Errorf("audit event encoding failed")
	}
	line = append(line, '\n')
	r.mu.Lock()
	defer r.mu.Unlock()
	n, err := r.writer.Write(line)
	if err != nil {
		return fmt.Errorf("audit log write failed: %w", err)
	}
	if n != len(line) {
		return fmt.Errorf("audit log write failed: %w", io.ErrShortWrite)
	}
	return nil
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
