package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Ticket #79's minimal API:
//
//   type Event struct {
//       ID, Time, Action string
//       Actor *string
//       Target *Target
//       Outcome string
//       Count int64
//       FailureCode, FirstTime, LastTime *string
//   }
//   type Target struct { Kind, ID string }
//   func (Event) Validate() error
//   type Recorder interface { Record(context.Context, Event) error }
//   func NewLogRecorder(io.Writer) Recorder
//
// Event's JSON tags implement the contract field names and omit only the three
// optional fields. Callers remain responsible for supplying server-owned IDs,
// times, action values and safe codes; provenance cannot be inferred from a Go
// string. The recorder validates and explicitly projects Event rather than
// accepting arbitrary request/application objects.

func stringPointer(value string) *string { return &value }

func validEvent() Event {
	return Event{
		ID:      "01a0f39f-e3a3-7abc-8def-0123456789ab",
		Time:    "2026-09-30T18:42:17.123Z",
		Action:  "account.sign_in",
		Actor:   nil,
		Target:  &Target{Kind: "operation", ID: "account.sign_in"},
		Outcome: "failure",
		Count:   1,
	}
}

func TestDocumentedAuditExamplesAreValidEvents(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "docs", "phase2", "examples", "audit*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 3 {
		t.Fatalf("found %d audit examples, want 3", len(paths))
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			var event Event
			if err := decoder.Decode(&event); err != nil {
				t.Fatalf("example does not decode into the allowlisted event shape: %v", err)
			}
			if err := event.Validate(); err != nil {
				t.Fatalf("documented example is invalid: %v", err)
			}
			var trailing any
			if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
				t.Fatalf("example has trailing JSON: %v", err)
			}
		})
	}
}

func TestEventJSONUsesOnlyContractFields(t *testing.T) {
	event := validEvent()
	event.Actor = stringPointer("018f47d2-9c2a-7abc-8def-0123456789ab")
	event.FailureCode = stringPointer("invalid_credentials")
	event.FirstTime = stringPointer("2026-09-30T18:40:00.000Z")
	event.LastTime = stringPointer(event.Time)
	event.Count = 7

	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	wantFields := []string{"action", "actor", "count", "failure_code", "first_time", "id", "last_time", "outcome", "target", "time"}
	gotFields := make([]string, 0, len(object))
	for _, field := range wantFields {
		if _, ok := object[field]; ok {
			gotFields = append(gotFields, field)
		}
	}
	if !reflect.DeepEqual(gotFields, wantFields) || len(object) != len(wantFields) {
		t.Fatalf("event JSON fields = %v, want exactly %v", gotFields, wantFields)
	}
	var target map[string]json.RawMessage
	if err := json.Unmarshal(object["target"], &target); err != nil {
		t.Fatal(err)
	}
	if len(target) != 2 || target["kind"] == nil || target["id"] == nil {
		t.Fatalf("target fields = %v, want exactly kind and id", target)
	}

	minimal, err := json.Marshal(validEvent())
	if err != nil {
		t.Fatal(err)
	}
	for _, omitted := range []string{"failure_code", "first_time", "last_time"} {
		if bytes.Contains(minimal, []byte(`"`+omitted+`"`)) {
			t.Fatalf("optional field %q was not omitted", omitted)
		}
	}
	if !bytes.Contains(minimal, []byte(`"actor":null`)) || !bytes.Contains(minimal, []byte(`"target":`)) {
		t.Fatal("required nullable actor and target fields must remain in the envelope")
	}
}

func TestEventValidateRejectsMalformedRequiredEnvelope(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Event)
	}{
		{"missing_id", func(e *Event) { e.ID = "" }},
		{"non_uuid_id", func(e *Event) { e.ID = "client-event-id" }},
		{"non_v7_id", func(e *Event) { e.ID = "550e8400-e29b-41d4-a716-446655440000" }},
		{"uppercase_id", func(e *Event) { e.ID = "01A0F39F-E3A3-7ABC-8DEF-0123456789AB" }},
		{"missing_time", func(e *Event) { e.Time = "" }},
		{"time_offset", func(e *Event) { e.Time = "2026-09-30T11:42:17.123-07:00" }},
		{"time_without_milliseconds", func(e *Event) { e.Time = "2026-09-30T18:42:17Z" }},
		{"time_with_microseconds", func(e *Event) { e.Time = "2026-09-30T18:42:17.123456Z" }},
		{"invalid_calendar_time", func(e *Event) { e.Time = "2026-09-31T18:42:17.123Z" }},
		{"missing_action", func(e *Event) { e.Action = "" }},
		{"submitted_style_action", func(e *Event) { e.Action = "Sign in for alice@example.test" }},
		{"invalid_actor", func(e *Event) { e.Actor = stringPointer("alice@example.test") }},
		{"missing_target_kind", func(e *Event) { e.Target.Kind = "" }},
		{"missing_target_id", func(e *Event) { e.Target.ID = "" }},
		{"operation_target_mismatch", func(e *Event) { e.Target.ID = "password.changed" }},
		{"unknown_outcome", func(e *Event) { e.Outcome = "denied" }},
		{"zero_count", func(e *Event) { e.Count = 0 }},
		{"negative_count", func(e *Event) { e.Count = -1 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event := validEvent()
			tc.mutate(&event)
			if err := event.Validate(); err == nil {
				t.Fatal("Validate accepted malformed required envelope")
			}
		})
	}
}

func TestEventValidateAllowsContractNullableAndActorValues(t *testing.T) {
	for _, actor := range []*string{
		nil,
		stringPointer("local-prototype"),
		stringPointer("018f47d2-9c2a-7abc-8def-0123456789ab"),
	} {
		event := validEvent()
		event.Actor = actor
		if err := event.Validate(); err != nil {
			t.Fatalf("valid actor %v rejected: %v", actor, err)
		}
	}
	event := validEvent()
	event.Target = nil
	if err := event.Validate(); err != nil {
		t.Fatalf("nullable target rejected: %v", err)
	}
}

func TestEventValidateCountedWindow(t *testing.T) {
	valid := validEvent()
	valid.Count = 3
	valid.FailureCode = stringPointer("throttled")
	valid.FirstTime = stringPointer("2026-09-30T18:41:01.000Z")
	valid.LastTime = stringPointer(valid.Time)
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid counted failure rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Event)
	}{
		{"count_without_window", func(e *Event) { e.FirstTime, e.LastTime = nil, nil }},
		{"first_without_last", func(e *Event) { e.LastTime = nil }},
		{"last_without_first", func(e *Event) { e.FirstTime = nil }},
		{"malformed_first", func(e *Event) { e.FirstTime = stringPointer("2026-09-30 18:41:01Z") }},
		{"last_before_first", func(e *Event) { e.LastTime = stringPointer("2026-09-30T18:40:00.000Z") }},
		{"time_not_last_time", func(e *Event) { e.Time = "2026-09-30T18:42:17.122Z" }},
		{"counted_success", func(e *Event) { e.Outcome = "success" }},
		{"counted_known_actor", func(e *Event) { e.Actor = stringPointer("local-prototype") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event := valid
			tc.mutate(&event)
			if err := event.Validate(); err == nil {
				t.Fatal("Validate accepted an invalid counted-event window")
			}
		})
	}
}

func TestLogRecorderWritesOneValidatedJSONLine(t *testing.T) {
	var output bytes.Buffer
	var recorder Recorder = NewLogRecorder(&output)
	event := validEvent()
	if err := recorder.Record(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if bytes.Count(output.Bytes(), []byte{'\n'}) != 1 || output.Len() == 0 || output.Bytes()[output.Len()-1] != '\n' {
		t.Fatalf("log output must be exactly one newline-terminated record: %q", output.Bytes())
	}
	line := bytes.TrimSuffix(output.Bytes(), []byte{'\n'})
	var got Event
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&got); err != nil {
		t.Fatalf("log line is not a contract event: %v", err)
	}
	if !reflect.DeepEqual(got, event) {
		t.Fatalf("logged event = %#v, want %#v", got, event)
	}
}

func TestLogRecorderRejectsInvalidEventWithoutWriting(t *testing.T) {
	var output bytes.Buffer
	recorder := NewLogRecorder(&output)
	event := validEvent()
	event.Count = 0
	if err := recorder.Record(context.Background(), event); err == nil {
		t.Fatal("Record accepted an invalid event")
	}
	if output.Len() != 0 {
		t.Fatalf("invalid event reached the sink: %q", output.Bytes())
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestLogRecorderExposesSinkFailureWithoutEventData(t *testing.T) {
	sinkErr := errors.New("sink unavailable")
	event := validEvent()
	event.Actor = stringPointer("018f47d2-9c2a-7abc-8def-0123456789ab")
	err := NewLogRecorder(failingWriter{err: sinkErr}).Record(context.Background(), event)
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Record error = %v, want wrapped sink failure", err)
	}
	if err == nil {
		t.Fatal("Record reported success when the sink rejected the line")
	}
	for _, eventValue := range []string{event.ID, event.Time, event.Action, *event.Actor, event.Target.ID} {
		if strings.Contains(err.Error(), eventValue) {
			t.Fatalf("sink failure exposed event value %q: %v", eventValue, err)
		}
	}
}
