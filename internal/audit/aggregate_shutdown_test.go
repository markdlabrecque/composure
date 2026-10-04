package audit

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

const shutdownFlushTimeoutOption = "ShutdownFlushTimeout"

func setShutdownFlushTimeout(options *AggregateOptions, timeout time.Duration) bool {
	field := reflect.ValueOf(options).Elem().FieldByName(shutdownFlushTimeoutOption)
	if !field.IsValid() || !field.CanSet() || field.Type() != reflect.TypeOf(time.Duration(0)) {
		return false
	}
	field.SetInt(int64(timeout))
	return true
}

func TestAggregateRecorderShutdownFlushTimeoutOption(t *testing.T) {
	options := AggregateOptions{Capacity: aggregateGroupCount}
	if !setShutdownFlushTimeout(&options, 25*time.Millisecond) {
		t.Fatalf("AggregateOptions.%s must be an exported time.Duration", shutdownFlushTimeoutOption)
	}
	if _, err := NewAggregateRecorder(&collectingRecorder{}, options); err != nil {
		t.Fatalf("positive shutdown flush timeout rejected: %v", err)
	}

	if !setShutdownFlushTimeout(&options, -time.Nanosecond) {
		t.Fatalf("AggregateOptions.%s must be an exported time.Duration", shutdownFlushTimeoutOption)
	}
	if _, err := NewAggregateRecorder(&collectingRecorder{}, options); err == nil {
		t.Fatal("negative shutdown flush timeout accepted")
	}
}

type deadlineObservingSink struct {
	deadline chan time.Duration
}

func (s *deadlineObservingSink) Record(ctx context.Context, _ Event) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		s.deadline <- 0
		return nil
	}
	s.deadline <- time.Until(deadline)
	return nil
}

func TestAggregateRecorderDefaultShutdownFlushHasOneSecondDeadline(t *testing.T) {
	sink := &deadlineObservingSink{deadline: make(chan time.Duration, 1)}
	recorder, err := NewAggregateRecorder(sink, AggregateOptions{
		Capacity:      aggregateGroupCount,
		FlushInterval: time.Hour,
		Now:           func() time.Time { return time.Unix(1, 0) },
		NewID:         func() (string, error) { return "01a0f39f-e3a3-7abc-8def-0123456789ab", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.AddFailure(context.Background(), FailureGroup{Action: "account.sign_in"}, 1); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := recorder.Run(ctx); err != nil {
		t.Fatalf("successful final flush: %v", err)
	}

	remaining := <-sink.deadline
	if remaining < 800*time.Millisecond || remaining > time.Second {
		t.Fatalf("default final-flush deadline remaining = %v, want approximately one second", remaining)
	}
}

type serializingSink struct {
	entered chan struct{}
	release chan struct{}
}

func (s *serializingSink) Record(ctx context.Context, _ Event) error {
	s.entered <- struct{}{}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.release:
		return nil
	}
}

func TestAggregateRecorderFlushSerializationHonorsWaitingContext(t *testing.T) {
	sink := &serializingSink{entered: make(chan struct{}, 1), release: make(chan struct{})}
	recorder := newAggregateForTest(t, sink, aggregateGroupCount, aggregateTimes[:1])
	if err := recorder.AddFailure(context.Background(), FailureGroup{Action: "account.sign_in"}, 1); err != nil {
		t.Fatal(err)
	}

	firstDone := make(chan error, 1)
	go func() { firstDone <- recorder.Flush(context.Background()) }()
	select {
	case <-sink.entered:
	case <-time.After(time.Second):
		t.Fatal("first flush did not enter sink")
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	secondDone := make(chan error, 1)
	started := time.Now()
	go func() { secondDone <- recorder.Flush(waitCtx) }()

	var secondErr error
	returnedBeforeRelease := false
	select {
	case secondErr = <-secondDone:
		returnedBeforeRelease = true
	case <-time.After(150 * time.Millisecond):
	}
	close(sink.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Flush: %v", err)
	}
	if !returnedBeforeRelease {
		secondErr = <-secondDone
	}

	if !returnedBeforeRelease || !errors.Is(secondErr, context.DeadlineExceeded) {
		t.Fatalf("waiting Flush returned after release in %v with %v; want its deadline error before release", time.Since(started), secondErr)
	}
}

type shutdownRetrySink struct {
	mu            sync.Mutex
	calls         int
	firstEntered  chan struct{}
	secondEntered chan struct{}
	releaseSecond chan struct{}
	events        []Event
	deadlines     []bool
}

func (s *shutdownRetrySink) Record(ctx context.Context, event Event) error {
	s.mu.Lock()
	s.calls++
	call := s.calls
	_, hasDeadline := ctx.Deadline()
	s.deadlines = append(s.deadlines, hasDeadline)
	s.mu.Unlock()

	switch call {
	case 1:
		close(s.firstEntered)
		<-ctx.Done()
		return ctx.Err()
	case 2:
		close(s.secondEntered)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.releaseSecond:
			return errors.New("forced test cleanup")
		}
	default:
		s.mu.Lock()
		s.events = append(s.events, event)
		s.mu.Unlock()
		return nil
	}
}

func TestAggregateRecorderShutdownRetryIsBoundedAndRetainsExactWindow(t *testing.T) {
	sink := &shutdownRetrySink{
		firstEntered:  make(chan struct{}),
		secondEntered: make(chan struct{}),
		releaseSecond: make(chan struct{}),
	}
	recorder, err := NewAggregateRecorder(sink, AggregateOptions{
		Capacity:      aggregateGroupCount,
		FlushInterval: time.Millisecond,
		Now:           func() time.Time { return time.Unix(1, 0) },
		NewID:         func() (string, error) { return "01a0f39f-e3a3-7abc-8def-0123456789ab", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.AddFailure(context.Background(), FailureGroup{Action: "account.sign_in"}, 7); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- recorder.Run(ctx) }()
	select {
	case <-sink.firstEntered:
	case <-time.After(time.Second):
		t.Fatal("periodic flush did not enter sink")
	}
	cancel()
	select {
	case <-sink.secondEntered:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not start final retry")
	}

	var runErr error
	bounded := false
	select {
	case runErr = <-runDone:
		bounded = true
	case <-time.After(1200 * time.Millisecond):
		close(sink.releaseSecond)
		runErr = <-runDone
	}
	if bounded {
		close(sink.releaseSecond)
	}
	if !bounded || !errors.Is(runErr, context.DeadlineExceeded) {
		t.Fatalf("Run final retry bounded/error = %v/%v, want joined one-second deadline failure", bounded, runErr)
	}

	if err := recorder.Flush(context.Background()); err != nil {
		t.Fatalf("explicit retry: %v", err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.deadlines) < 2 || sink.deadlines[0] || !sink.deadlines[1] {
		t.Fatalf("sink deadline observations = %v, want periodic canceled context then independently deadline-limited final context", sink.deadlines)
	}
	if sink.calls != 3 || len(sink.events) != 1 || sink.events[0].Count != 7 {
		t.Fatalf("retry calls/events = %d/%#v, want exact count 7 persisted once on explicit retry", sink.calls, sink.events)
	}
}
