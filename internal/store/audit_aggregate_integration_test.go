package store

import (
	"context"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/audit"
)

func TestAggregateRecorderPersistsOneThrottledWindowAcrossReopen(t *testing.T) {
	store, path := accountSite(t)
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	id := "01a0f39f-e3a3-7abc-8def-0123456789ab"
	recorder, err := audit.NewAggregateRecorder(store, audit.AggregateOptions{
		Capacity: 5,
		Now:      func() time.Time { now = now.Add(time.Millisecond); return now },
		NewID:    func() (string, error) { return id, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	code := "throttled"
	for i := 0; i < 10_000; i++ {
		if err := recorder.AddFailure(context.Background(), audit.FailureGroup{Action: "account.sign_in", FailureCode: &code}, 1); err != nil {
			t.Fatalf("AddFailure %d: %v", i, err)
		}
	}
	if err := recorder.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListAuditEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Count != 10_000 {
		t.Fatalf("SQLite events = %#v, want one count-10000 window", events)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	events, err = reopened.ListAuditEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Count != 10_000 {
		t.Fatalf("reopened SQLite events = %#v, want persisted aggregate", events)
	}

	// Pending state is process-local: constructing a replacement recorder does
	// not reconstruct, duplicate, or invent another window.
	replacement, err := audit.NewAggregateRecorder(reopened, audit.AggregateOptions{Capacity: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := replacement.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, err = reopened.ListAuditEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("empty replacement recorder invented %d events", len(events)-1)
	}
}
