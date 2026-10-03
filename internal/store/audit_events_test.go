package store

import (
	"context"
	"reflect"
	"testing"

	"github.com/markdlabrecque/composure/internal/audit"
)

func auditEvent(id, eventTime, action, outcome string) audit.Event {
	return audit.Event{
		ID: id, Time: eventTime, Action: action, Outcome: outcome, Count: 1,
	}
}

func TestAuditEventsRoundTripAndRestart(t *testing.T) {
	s, path := accountSite(t)
	ctx := context.Background()
	actor := "018f47a2-4b9c-7abc-8def-0123456789ab"
	failureCode := "throttled"
	firstTime := "2026-09-30T12:00:00.001Z"
	lastTime := "2026-09-30T12:00:05.009Z"
	events := []audit.Event{
		{
			ID: "018f47a2-4b9c-7abc-8def-0123456789ac", Time: "2026-09-30T11:59:59.999Z",
			Action: "content.published", Actor: &actor,
			Target:  &audit.Target{Kind: "content", ID: "018f47a2-4b9c-7abc-8def-0123456789ad"},
			Outcome: "success", Count: 1,
		},
		auditEvent("018f47a2-4b9c-7abc-8def-0123456789ae", "2026-09-30T12:00:00.000Z", "site.exported", "failure"),
		{
			ID: "018f47a2-4b9c-7abc-8def-0123456789af", Time: lastTime,
			Action: "account.sign_in", Target: &audit.Target{Kind: "operation", ID: "account.sign_in"},
			Outcome: "failure", Count: 7, FailureCode: &failureCode,
			FirstTime: &firstTime, LastTime: &lastTime,
		},
	}
	for _, event := range events {
		if err := s.AppendAuditEvent(ctx, event); err != nil {
			t.Fatalf("append audit event %q: %v", event.ID, err)
		}
	}
	verify := func(store *Store) {
		t.Helper()
		got, err := store.ListAuditEvents(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, events) {
			t.Fatalf("audit event round trip differs:\n got: %#v\nwant: %#v", got, events)
		}
	}
	verify(s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	verify(reopened)
}

func TestAuditEventsListByTimeWithStableTies(t *testing.T) {
	s, _ := accountSite(t)
	ctx := context.Background()
	events := []audit.Event{
		auditEvent("018f47a2-4b9c-7abc-8def-0123456789b2", "2026-09-30T12:00:01.000Z", "site.exported", "success"),
		auditEvent("018f47a2-4b9c-7abc-8def-0123456789b1", "2026-09-30T12:00:00.000Z", "site.exported", "success"),
		auditEvent("018f47a2-4b9c-7abc-8def-0123456789b0", "2026-09-30T12:00:00.000Z", "site.exported", "success"),
	}
	for _, event := range events {
		if err := s.AppendAuditEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []audit.Event{events[2], events[1], events[0]}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("list order differs:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestAuditEventsEmptyList(t *testing.T) {
	s, _ := accountSite(t)
	got, err := s.ListAuditEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("empty audit list = %#v, want non-nil empty slice", got)
	}
}

func TestAppendAuditEventValidatesBeforeWriting(t *testing.T) {
	s, _ := accountSite(t)
	ctx := context.Background()
	valid := auditEvent("018f47a2-4b9c-7abc-8def-0123456789b3", "2026-09-30T12:00:00.000Z", "site.exported", "success")
	if err := s.AppendAuditEvent(ctx, valid); err != nil {
		t.Fatal(err)
	}
	before, err := s.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var changesBefore int64
	if err := s.db.QueryRow("SELECT total_changes()").Scan(&changesBefore); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.ID = "018f47a2-4b9c-7abc-8def-0123456789b4"
	invalid.Action = "submitted.untrusted_action"
	if err := s.AppendAuditEvent(ctx, invalid); err == nil {
		t.Fatal("invalid audit event append succeeded")
	}
	after, err := s.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var changesAfter int64
	if err := s.db.QueryRow("SELECT total_changes()").Scan(&changesAfter); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) || changesAfter != changesBefore {
		t.Fatal("validation failure wrote or changed an audit row")
	}
}

func TestAppendAuditEventDuplicateIDDoesNotOverwrite(t *testing.T) {
	s, _ := accountSite(t)
	ctx := context.Background()
	original := auditEvent("018f47a2-4b9c-7abc-8def-0123456789b5", "2026-09-30T12:00:00.000Z", "site.exported", "success")
	if err := s.AppendAuditEvent(ctx, original); err != nil {
		t.Fatal(err)
	}
	replacement := original
	replacement.Time = "2026-09-30T12:00:01.000Z"
	replacement.Action = "site.restored"
	if err := s.AppendAuditEvent(ctx, replacement); err == nil {
		t.Fatal("duplicate audit event ID append succeeded")
	}
	got, err := s.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []audit.Event{original}) {
		t.Fatalf("duplicate append changed original event: %#v", got)
	}
}
