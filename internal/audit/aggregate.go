package audit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
)

const aggregateGroupCount = 5

const defaultAggregateShutdownFlushTimeout = time.Second

// FailureGroup identifies one fixed, supported class of authentication failure.
// Its values are validated against the contract allowlist before they are kept.
type FailureGroup struct {
	Action      string
	FailureCode *string
}

// AggregateOptions configures the in-memory failure aggregation worker.
type AggregateOptions struct {
	Capacity      int
	FlushInterval time.Duration
	// ShutdownFlushTimeout bounds the final flush attempt; zero selects one second.
	ShutdownFlushTimeout time.Duration
	Now                  func() time.Time
	NewID                func() (string, error)
}

type failureKey struct {
	action string
	code   string
}

type failureWindow struct {
	count int64
	first time.Time
	last  time.Time
}

// AggregateRecorder counts only the finite set of covered authentication
// failures. Pending counts are process-local until a sink accepts a flush.
type AggregateRecorder struct {
	sink          Recorder
	flushInterval time.Duration
	now           func() time.Time
	newID         func() (string, error)

	mu       sync.Mutex
	pending  map[failureKey]failureWindow
	inFlight map[failureKey]failureWindow

	flushSlot            chan struct{}
	shutdownFlushTimeout time.Duration
	runMu                sync.Mutex
	runOnce              bool
}

var supportedFailureGroups = [...]failureKey{
	{action: "account.sign_in"},
	{action: "account.sign_in", code: "invalid_credentials"},
	{action: "account.sign_in", code: "throttled"},
	{action: "account.created"},
	{action: "password.changed"},
}

// NewAggregateRecorder constructs a recorder whose capacity can hold every
// group in the fixed supported failure domain.
func NewAggregateRecorder(sink Recorder, options AggregateOptions) (*AggregateRecorder, error) {
	if sink == nil {
		return nil, errors.New("aggregate audit recorder requires a sink")
	}
	if options.Capacity < aggregateGroupCount {
		return nil, fmt.Errorf("aggregate audit capacity must be at least %d", aggregateGroupCount)
	}
	if options.FlushInterval < 0 {
		return nil, errors.New("aggregate audit flush interval cannot be negative")
	}
	if options.ShutdownFlushTimeout < 0 {
		return nil, errors.New("aggregate audit shutdown flush timeout cannot be negative")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewID == nil {
		options.NewID = func() (string, error) { return content.NewID(options.Now().UTC()) }
	}
	if options.FlushInterval == 0 {
		options.FlushInterval = time.Minute
	}
	if options.ShutdownFlushTimeout == 0 {
		options.ShutdownFlushTimeout = defaultAggregateShutdownFlushTimeout
	}
	flushSlot := make(chan struct{}, 1)
	flushSlot <- struct{}{}
	return &AggregateRecorder{
		sink:                 sink,
		flushInterval:        options.FlushInterval,
		shutdownFlushTimeout: options.ShutdownFlushTimeout,
		flushSlot:            flushSlot,
		now:                  options.Now,
		newID:                options.NewID,
		pending:              make(map[failureKey]failureWindow, aggregateGroupCount),
		inFlight:             make(map[failureKey]failureWindow, aggregateGroupCount),
	}, nil
}

// AddFailure adds count occurrences to one fixed failure group. Callers should
// pass one for each request; the count parameter also permits safe overflow
// detection without requiring an impractical number of calls.
func (r *AggregateRecorder) AddFailure(ctx context.Context, group FailureGroup, count int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if count <= 0 {
		return errors.New("aggregate audit count must be positive")
	}
	key, ok := supportedFailureKey(group)
	if !ok {
		return errors.New("unsupported aggregate audit failure group")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.pending[key]
	reserved := r.inFlight[key]
	if count > math.MaxInt64-current.count-reserved.count {
		return errors.New("aggregate audit count overflow")
	}
	now := r.now().UTC().Truncate(time.Millisecond)
	if current.count == 0 {
		current = failureWindow{count: count, first: now, last: now}
	} else {
		current.count += count
		if now.Before(current.first) {
			current.first = now
		}
		if now.After(current.last) {
			current.last = now
		}
	}
	r.pending[key] = current
	return nil
}

func supportedFailureKey(group FailureGroup) (failureKey, bool) {
	code := ""
	if group.FailureCode != nil {
		code = *group.FailureCode
		if code == "" {
			return failureKey{}, false
		}
	}
	key := failureKey{action: group.Action, code: code}
	for _, supported := range supportedFailureGroups {
		if key == supported {
			return key, true
		}
	}
	return failureKey{}, false
}

// Flush persists each captured group once. Arrivals during a successful sink
// write form a new window. If a write fails, the captured window is retained
// together with any arrivals so the next attempt does not lose occurrences.
func (r *AggregateRecorder) Flush(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.flushSlot:
	}
	defer func() { r.flushSlot <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	captured := r.pending
	r.pending = make(map[failureKey]failureWindow, aggregateGroupCount)
	r.inFlight = captured
	r.mu.Unlock()
	if len(captured) == 0 {
		return nil
	}

	events := make([]struct {
		key   failureKey
		event Event
	}, 0, len(captured))
	for _, key := range supportedFailureGroups {
		window, exists := captured[key]
		if !exists {
			continue
		}
		id, err := r.newID()
		if err != nil {
			r.restoreCaptured(captured)
			return fmt.Errorf("generate aggregate audit event id: %w", err)
		}
		first := formatAuditTime(window.first)
		last := formatAuditTime(window.last)
		var code *string
		if key.code != "" {
			code = aggregateStringPointer(key.code)
		}
		events = append(events, struct {
			key   failureKey
			event Event
		}{key: key, event: Event{
			ID: id, Time: last, Action: key.action, Actor: nil,
			Target:  &Target{Kind: "operation", ID: key.action},
			Outcome: "failure", Count: window.count,
			FailureCode: code, FirstTime: &first, LastTime: &last,
		}})
	}

	for i, item := range events {
		if err := ctx.Err(); err != nil {
			r.restoreCaptured(capturedFrom(events[i:]))
			return err
		}
		if err := r.sink.Record(ctx, item.event); err != nil {
			r.restoreCaptured(capturedFrom(events[i:]))
			return fmt.Errorf("flush aggregate audit event %s: %w", item.key.action, err)
		}
		r.mu.Lock()
		delete(r.inFlight, item.key)
		r.mu.Unlock()
	}
	return nil
}

func capturedFrom(events []struct {
	key   failureKey
	event Event
}) map[failureKey]failureWindow {
	captured := make(map[failureKey]failureWindow, len(events))
	for _, item := range events {
		first, _ := validTime(*item.event.FirstTime)
		last, _ := validTime(*item.event.LastTime)
		captured[item.key] = failureWindow{count: item.event.Count, first: first, last: last}
	}
	return captured
}

func (r *AggregateRecorder) restoreCaptured(captured map[failureKey]failureWindow) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, old := range captured {
		current := r.pending[key]
		current.count += old.count // AddFailure reserved this combined range.
		if current.first.IsZero() || old.first.Before(current.first) {
			current.first = old.first
		}
		if old.last.After(current.last) {
			current.last = old.last
		}
		r.pending[key] = current
		delete(r.inFlight, key)
	}
}

// Run periodically flushes pending groups and attempts a final flush after
// shutdown with an independent deadline. The sink must honor context
// cancellation for shutdown to return within that deadline; writes stay
// synchronous so the worker joins each sink call before returning.
func (r *AggregateRecorder) Run(ctx context.Context) error {
	r.runMu.Lock()
	if r.runOnce {
		r.runMu.Unlock()
		return errors.New("aggregate audit recorder Run may only be called once")
	}
	r.runOnce = true
	r.runMu.Unlock()

	ticker := time.NewTicker(r.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), r.shutdownFlushTimeout)
			err := r.Flush(flushCtx)
			cancel()
			return err
		case <-ticker.C:
			// A failed periodic flush retains its window for the next cadence.
			_ = r.Flush(ctx)
		}
	}
}

func formatAuditTime(value time.Time) string {
	return value.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}

func aggregateStringPointer(value string) *string {
	return &value
}
