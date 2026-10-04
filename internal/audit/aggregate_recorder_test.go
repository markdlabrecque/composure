package audit

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
	"time"
)

// Ticket #132's aggregate-recorder API:
//
//   type FailureGroup struct { Action string; FailureCode *string }
//   type AggregateOptions struct {
//       Capacity int
//       FlushInterval time.Duration
//       Now func() time.Time
//       NewID func() (string, error)
//   }
//   func NewAggregateRecorder(Recorder, AggregateOptions) (*AggregateRecorder, error)
//   func (*AggregateRecorder) AddFailure(context.Context, FailureGroup, int64) error
//   func (*AggregateRecorder) Flush(context.Context) error
//   func (*AggregateRecorder) Run(context.Context) error
//
// AddFailure accepts a positive occurrence count so overflow can be tested
// without billions of calls. Request paths pass one. Run owns the periodic
// worker and performs an orderly final flush before it returns.

var aggregateTimes = []time.Time{
	time.Date(2026, 10, 3, 12, 0, 2, 987654321, time.FixedZone("submitted", -7*60*60)),
	time.Date(2026, 10, 3, 19, 0, 1, 123456789, time.UTC),
}

type collectingRecorder struct {
	mu      sync.Mutex
	events  []Event
	err     error
	calls   int
	enter   chan struct{}
	release chan struct{}
}

func (r *collectingRecorder) Record(_ context.Context, event Event) error {
	r.mu.Lock()
	r.calls++
	err := r.err
	enter, release := r.enter, r.release
	if err == nil && release == nil {
		r.events = append(r.events, event)
	}
	r.mu.Unlock()
	if enter != nil {
		select {
		case enter <- struct{}{}:
		default:
		}
	}
	if release != nil {
		<-release
		if err == nil {
			r.mu.Lock()
			r.events = append(r.events, event)
			r.mu.Unlock()
		}
	}
	return err
}

func (r *collectingRecorder) snapshot() ([]Event, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.events...), r.calls
}

func newAggregateForTest(t *testing.T, sink Recorder, capacity int, times []time.Time) *AggregateRecorder {
	t.Helper()
	index := 0
	recorder, err := NewAggregateRecorder(sink, AggregateOptions{
		Capacity: capacity,
		Now: func() time.Time {
			value := times[index]
			index++
			return value
		},
		NewID: func() (string, error) { return "01a0f39f-e3a3-7abc-8def-0123456789ab", nil },
	})
	if err != nil {
		t.Fatalf("NewAggregateRecorder: %v", err)
	}
	return recorder
}

func TestAggregateRecorderEmitsOnlyTheFiniteFailureDomain(t *testing.T) {
	credential := "invalid_credentials"
	throttled := "throttled"
	allowed := []FailureGroup{
		{Action: "account.sign_in"},
		{Action: "account.sign_in", FailureCode: &credential},
		{Action: "account.sign_in", FailureCode: &throttled},
		{Action: "account.created"},
		{Action: "password.changed"},
	}
	times := make([]time.Time, len(allowed))
	for i := range times {
		times[i] = time.Date(2026, 10, 3, 12, 0, i, 0, time.UTC)
	}
	sink := &collectingRecorder{}
	recorder := newAggregateForTest(t, sink, len(allowed), times)
	for _, group := range allowed {
		if err := recorder.AddFailure(context.Background(), group, 1); err != nil {
			t.Fatalf("AddFailure(%#v): %v", group, err)
		}
	}
	if err := recorder.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, _ := sink.snapshot()
	if len(events) != len(allowed) {
		t.Fatalf("flushed %d groups, want %d", len(events), len(allowed))
	}
	for _, event := range events {
		if event.Actor != nil || event.Outcome != "failure" || event.Count != 1 {
			t.Fatalf("unsafe aggregate envelope: %#v", event)
		}
		if event.Target == nil || event.Target.Kind != "operation" || event.Target.ID != event.Action {
			t.Fatalf("aggregate target = %#v, want fixed operation target for %q", event.Target, event.Action)
		}
	}

	unsupportedCode := "database error for alice@example.test"
	invalid := []struct {
		name  string
		group FailureGroup
		count int64
	}{
		{"unknown action", FailureGroup{Action: "roles.changed"}, 1},
		{"unsupported code", FailureGroup{Action: "account.sign_in", FailureCode: &unsupportedCode}, 1},
		{"code on account creation", FailureGroup{Action: "account.created", FailureCode: &credential}, 1},
		{"zero count", FailureGroup{Action: "account.sign_in"}, 0},
		{"negative count", FailureGroup{Action: "account.sign_in"}, -1},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if err := recorder.AddFailure(context.Background(), tc.group, tc.count); err == nil {
				t.Fatal("invalid or unsupported aggregate input accepted")
			}
		})
	}
}

func TestAggregateRecorderRequiresCapacityForTheWholeDomain(t *testing.T) {
	for _, capacity := range []int{-1, 0, 4} {
		if _, err := NewAggregateRecorder(&collectingRecorder{}, AggregateOptions{Capacity: capacity}); err == nil {
			t.Fatalf("capacity %d accepted; supported domain requires capacity 5", capacity)
		}
	}
}

func TestAggregateRecorderUsesServerClockAndWindowOrdering(t *testing.T) {
	sink := &collectingRecorder{}
	recorder := newAggregateForTest(t, sink, 5, aggregateTimes)
	group := FailureGroup{Action: "account.sign_in"}
	if err := recorder.AddFailure(context.Background(), group, 2); err != nil {
		t.Fatal(err)
	}
	if err := recorder.AddFailure(context.Background(), group, 3); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, _ := sink.snapshot()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	event := events[0]
	if event.Count != 5 || event.FirstTime == nil || *event.FirstTime != "2026-10-03T19:00:01.123Z" ||
		event.LastTime == nil || *event.LastTime != "2026-10-03T19:00:02.987Z" || event.Time != *event.LastTime {
		t.Fatalf("aggregate window = %#v, want count 5 and chronological UTC millisecond bounds", event)
	}
}

func TestAggregateRecorderOverflowPreservesPendingWindow(t *testing.T) {
	sink := &collectingRecorder{}
	recorder := newAggregateForTest(t, sink, 5, aggregateTimes)
	group := FailureGroup{Action: "account.sign_in"}
	if err := recorder.AddFailure(context.Background(), group, math.MaxInt64); err != nil {
		t.Fatal(err)
	}
	if err := recorder.AddFailure(context.Background(), group, 1); err == nil {
		t.Fatal("int64 overflow accepted")
	}
	if err := recorder.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, _ := sink.snapshot()
	if len(events) != 1 || events[0].Count != math.MaxInt64 || *events[0].FirstTime != *events[0].LastTime {
		t.Fatalf("overflow changed pending window: %#v", events)
	}
}

func TestAggregateRecorderRetriesFailedFlushExactly(t *testing.T) {
	sinkErr := errors.New("sink unavailable")
	sink := &collectingRecorder{err: sinkErr}
	recorder := newAggregateForTest(t, sink, 5, aggregateTimes)
	if err := recorder.AddFailure(context.Background(), FailureGroup{Action: "account.sign_in"}, 4); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Flush(context.Background()); !errors.Is(err, sinkErr) {
		t.Fatalf("Flush error = %v", err)
	}
	sink.mu.Lock()
	sink.err = nil
	sink.mu.Unlock()
	if err := recorder.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, calls := sink.snapshot()
	if calls != 2 || len(events) != 1 || events[0].Count != 4 {
		t.Fatalf("retry calls/events = %d/%#v, want one exact retained window after two attempts", calls, events)
	}
}

func TestAggregateRecorderKeepsArrivalsDuringFlushInANewWindow(t *testing.T) {
	sink := &collectingRecorder{enter: make(chan struct{}, 1), release: make(chan struct{})}
	recorder := newAggregateForTest(t, sink, 5, aggregateTimes)
	group := FailureGroup{Action: "account.sign_in"}
	if err := recorder.AddFailure(context.Background(), group, 2); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- recorder.Flush(context.Background()) }()
	<-sink.enter
	addDone := make(chan error, 1)
	go func() { addDone <- recorder.AddFailure(context.Background(), group, 3) }()
	select {
	case err := <-addDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("AddFailure blocked behind the sink write")
	}
	close(sink.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	sink.enter, sink.release = nil, nil
	sink.mu.Unlock()
	if err := recorder.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, _ := sink.snapshot()
	if len(events) != 2 || events[0].Count != 2 || events[1].Count != 3 {
		t.Fatalf("flush windows = %#v, want counts 2 then 3", events)
	}
}

func TestAggregateRecorderSimultaneousFlushesDoNotDuplicateAWindow(t *testing.T) {
	sink := &collectingRecorder{enter: make(chan struct{}, 1), release: make(chan struct{})}
	recorder := newAggregateForTest(t, sink, 5, aggregateTimes[:1])
	if err := recorder.AddFailure(context.Background(), FailureGroup{Action: "account.sign_in"}, 6); err != nil {
		t.Fatal(err)
	}
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { first <- recorder.Flush(context.Background()) }()
	<-sink.enter
	go func() { second <- recorder.Flush(context.Background()) }()
	close(sink.release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	events, calls := sink.snapshot()
	if calls != 1 || len(events) != 1 || events[0].Count != 6 {
		t.Fatalf("simultaneous flush calls/events = %d/%#v, want one persisted window", calls, events)
	}
}

func TestAggregateRecorderRunFlushesPeriodicallyAndOnOrderlyShutdown(t *testing.T) {
	sink := &collectingRecorder{}
	ticks := make([]time.Time, 3)
	for i := range ticks {
		ticks[i] = time.Date(2026, 10, 3, 12, 0, i, 0, time.UTC)
	}
	index := 0
	recorder, err := NewAggregateRecorder(sink, AggregateOptions{
		Capacity: 5, FlushInterval: 10 * time.Millisecond,
		Now: func() time.Time { value := ticks[index]; index++; return value },
		NewID: func() (string, error) {
			ids := []string{"01a0f39f-e3a3-7abc-8def-0123456789ab", "01a0f39f-e3a3-7abc-8def-0123456789ac"}
			return ids[len(mustEvents(sink))], nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- recorder.Run(ctx) }()
	if err := recorder.AddFailure(context.Background(), FailureGroup{Action: "account.sign_in"}, 1); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(time.Second)
	for len(mustEvents(sink)) == 0 {
		select {
		case <-deadline:
			t.Fatal("periodic flush did not run")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if err := recorder.AddFailure(context.Background(), FailureGroup{Action: "account.created"}, 1); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not join its worker after cancellation")
	}
	events := mustEvents(sink)
	if len(events) != 2 || !reflect.DeepEqual([]string{events[0].Action, events[1].Action}, []string{"account.sign_in", "account.created"}) {
		t.Fatalf("periodic/final flush events = %#v", events)
	}
}

func mustEvents(sink *collectingRecorder) []Event {
	events, _ := sink.snapshot()
	return events
}
