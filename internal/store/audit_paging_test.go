package store

import (
	"context"
	"reflect"
	"testing"

	"github.com/markdlabrecque/composure/internal/audit"
)

func TestListAuditEventsPageOrdersNewestFirstAndPagesWithoutGaps(t *testing.T) {
	s, _ := accountSite(t)
	ctx := context.Background()
	events := []audit.Event{
		auditEvent("018f47a2-4b9c-7abc-8def-0123456789c2", "2026-09-30T12:00:02.000Z", "site.exported", "success"),
		auditEvent("018f47a2-4b9c-7abc-8def-0123456789c0", "2026-09-30T12:00:00.000Z", "site.exported", "success"),
		auditEvent("018f47a2-4b9c-7abc-8def-0123456789c4", "2026-09-30T12:00:03.000Z", "site.exported", "success"),
		auditEvent("018f47a2-4b9c-7abc-8def-0123456789c3", "2026-09-30T12:00:02.000Z", "site.exported", "success"),
		auditEvent("018f47a2-4b9c-7abc-8def-0123456789c1", "2026-09-30T12:00:01.000Z", "site.exported", "success"),
	}
	for _, event := range events {
		if err := s.AppendAuditEvent(ctx, event); err != nil {
			t.Fatalf("append audit event %q: %v", event.ID, err)
		}
	}

	want := []audit.Event{events[2], events[3], events[0], events[4], events[1]}
	var got []audit.Event
	for offset := 0; offset < len(want); offset += 2 {
		page, err := s.ListAuditEventsPage(ctx, 2, offset)
		if err != nil {
			t.Fatalf("page at offset %d: %v", offset, err)
		}
		got = append(got, page...)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paged events differ:\n got: %#v\nwant: %#v", got, want)
	}

	first, err := s.ListAuditEventsPage(ctx, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ListAuditEventsPage(ctx, 3, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, want[:3]) || !reflect.DeepEqual(second, want[3:]) {
		t.Fatalf("partial final page differs:\n first: %#v\nsecond: %#v", first, second)
	}
}

func TestListAuditEventsPageBoundaries(t *testing.T) {
	t.Run("empty database", func(t *testing.T) {
		s, _ := accountSite(t)
		got, err := s.ListAuditEventsPage(context.Background(), 2, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got == nil || len(got) != 0 {
			t.Fatalf("empty page = %#v, want non-nil empty slice", got)
		}
	})

	t.Run("exact and beyond end offsets", func(t *testing.T) {
		s, _ := accountSite(t)
		ctx := context.Background()
		for _, event := range []audit.Event{
			auditEvent("018f47a2-4b9c-7abc-8def-0123456789d0", "2026-09-30T12:00:00.000Z", "site.exported", "success"),
			auditEvent("018f47a2-4b9c-7abc-8def-0123456789d1", "2026-09-30T12:00:01.000Z", "site.exported", "success"),
		} {
			if err := s.AppendAuditEvent(ctx, event); err != nil {
				t.Fatal(err)
			}
		}
		for _, offset := range []int{2, 3} {
			got, err := s.ListAuditEventsPage(ctx, 2, offset)
			if err != nil {
				t.Fatalf("offset %d: %v", offset, err)
			}
			if got == nil || len(got) != 0 {
				t.Fatalf("offset %d page = %#v, want non-nil empty slice", offset, got)
			}
		}
	})
}

func TestListAuditEventsPageDecodesEnvelopeAfterReopen(t *testing.T) {
	s, path := accountSite(t)
	ctx := context.Background()
	actor := "018f47a2-4b9c-7abc-8def-0123456789e0"
	failureCode := "throttled"
	firstTime := "2026-09-30T12:00:01.000Z"
	lastTime := "2026-09-30T12:00:05.000Z"
	nullable := auditEvent("018f47a2-4b9c-7abc-8def-0123456789e1", "2026-09-30T12:00:00.000Z", "site.exported", "failure")
	withActorAndTarget := audit.Event{
		ID: "018f47a2-4b9c-7abc-8def-0123456789e2", Time: "2026-09-30T12:00:03.000Z",
		Action: "content.published", Actor: &actor,
		Target:  &audit.Target{Kind: "content", ID: "018f47a2-4b9c-7abc-8def-0123456789e3"},
		Outcome: "success", Count: 1,
	}
	counted := audit.Event{
		ID: "018f47a2-4b9c-7abc-8def-0123456789e4", Time: lastTime,
		Action: "account.sign_in", Target: &audit.Target{Kind: "operation", ID: "account.sign_in"},
		Outcome: "failure", Count: 9, FailureCode: &failureCode,
		FirstTime: &firstTime, LastTime: &lastTime,
	}
	for _, event := range []audit.Event{withActorAndTarget, nullable, counted} {
		if err := s.AppendAuditEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	got, err := reopened.ListAuditEventsPage(ctx, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []audit.Event{counted, withActorAndTarget, nullable}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reopened page differs:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestListAuditEventsPageRejectsInvalidBounds(t *testing.T) {
	s, _ := accountSite(t)
	for _, tc := range []struct {
		name          string
		limit, offset int
	}{
		{name: "zero limit", limit: 0},
		{name: "negative limit", limit: -1},
		{name: "negative offset", limit: 1, offset: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.ListAuditEventsPage(context.Background(), tc.limit, tc.offset)
			if err == nil {
				t.Fatalf("ListAuditEventsPage(%d, %d) succeeded with %#v", tc.limit, tc.offset, got)
			}
		})
	}
}

func TestListAuditEventsPageReturnsContextAndQueryErrors(t *testing.T) {
	t.Run("canceled context", func(t *testing.T) {
		s, _ := accountSite(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := s.ListAuditEventsPage(ctx, 1, 0); err == nil {
			t.Fatal("query with canceled context succeeded")
		}
	})

	t.Run("closed database", func(t *testing.T) {
		s, _ := accountSite(t)
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ListAuditEventsPage(context.Background(), 1, 0); err == nil {
			t.Fatal("query on closed database succeeded")
		}
	})
}

func TestListAuditEventsPageLeavesAscendingListUnchanged(t *testing.T) {
	s, _ := accountSite(t)
	ctx := context.Background()
	older := auditEvent("018f47a2-4b9c-7abc-8def-0123456789f0", "2026-09-30T12:00:00.000Z", "site.exported", "success")
	newer := auditEvent("018f47a2-4b9c-7abc-8def-0123456789f1", "2026-09-30T12:00:01.000Z", "site.exported", "success")
	for _, event := range []audit.Event{newer, older} {
		if err := s.AppendAuditEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []audit.Event{older, newer}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ascending list changed:\n got: %#v\nwant: %#v", got, want)
	}
}
