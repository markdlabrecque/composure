package store

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/markdlabrecque/composure/internal/audit"
)

func storeAuditRecorder(t *testing.T, store *Store) audit.Recorder {
	t.Helper()
	recorder, ok := any(store).(audit.Recorder)
	if !ok {
		t.Fatal("*Store does not implement audit.Recorder")
	}
	return recorder
}

func TestStoreRecorderPersistsEventAcrossReopen(t *testing.T) {
	store, path := accountSite(t)
	event := auditEvent("018f47a2-4b9c-7abc-8def-0123456789c0", "2026-10-03T12:00:00.000Z", "site.exported", "success")

	if err := storeAuditRecorder(t, store).Record(context.Background(), event); err != nil {
		t.Fatalf("record audit event: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	events, err := reopened.ListAuditEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []audit.Event{event}) {
		t.Fatalf("recorded events after reopen = %#v, want %#v", events, []audit.Event{event})
	}
}

func TestStoreRecorderRejectsInvalidAndCanceledEventsWithoutWriting(t *testing.T) {
	store, _ := accountSite(t)
	recorder := storeAuditRecorder(t, store)
	invalid := auditEvent("018f47a2-4b9c-7abc-8def-0123456789c1", "2026-10-03T12:00:00.000Z", "submitted.action", "failure")
	if err := recorder.Record(context.Background(), invalid); err == nil {
		t.Fatal("invalid audit event was recorded")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	valid := auditEvent("018f47a2-4b9c-7abc-8def-0123456789c2", "2026-10-03T12:00:01.000Z", "site.exported", "failure")
	if err := recorder.Record(canceled, valid); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Record error = %v, want context.Canceled", err)
	}
	events, err := store.ListAuditEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("rejected Record calls persisted %d events", len(events))
	}
}

func TestStoreRecorderPropagatesClosedStoreAndDuplicateErrors(t *testing.T) {
	t.Run("closed store", func(t *testing.T) {
		store, _ := accountSite(t)
		recorder := storeAuditRecorder(t, store)
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		event := auditEvent("018f47a2-4b9c-7abc-8def-0123456789c3", "2026-10-03T12:00:00.000Z", "site.exported", "failure")
		if err := recorder.Record(context.Background(), event); err == nil {
			t.Fatal("Record on a closed Store succeeded")
		}
	})

	t.Run("duplicate ID", func(t *testing.T) {
		store, _ := accountSite(t)
		recorder := storeAuditRecorder(t, store)
		original := auditEvent("018f47a2-4b9c-7abc-8def-0123456789c4", "2026-10-03T12:00:00.000Z", "site.exported", "success")
		if err := recorder.Record(context.Background(), original); err != nil {
			t.Fatal(err)
		}
		replacement := original
		replacement.Time = "2026-10-03T12:00:01.000Z"
		replacement.Action = "site.restored"
		if err := recorder.Record(context.Background(), replacement); err == nil {
			t.Fatal("Record with a duplicate event ID succeeded")
		}
		events, err := store.ListAuditEvents(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(events, []audit.Event{original}) {
			t.Fatalf("duplicate Record changed persisted event: %#v", events)
		}
	})
}
