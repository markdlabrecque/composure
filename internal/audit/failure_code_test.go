package audit

import (
	"context"
	"encoding/json"
	"testing"
)

func TestFailureCodeAllowlist(t *testing.T) {
	t.Run("absent is allowed", func(t *testing.T) {
		event := validCatalogEvent("account.sign_in")
		if err := event.Validate(); err != nil {
			t.Fatalf("absent failure code rejected: %v", err)
		}
	})

	t.Run("null is allowed", func(t *testing.T) {
		event := validCatalogEvent("account.sign_in")
		raw, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw[:len(raw)-1], []byte(`,"failure_code":null}`)...)
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		if err := event.Validate(); err != nil {
			t.Fatalf("null failure code rejected: %v", err)
		}
	})

	for _, code := range []string{"invalid_credentials", "throttled"} {
		t.Run("account.sign_in allows "+code, func(t *testing.T) {
			event := validCatalogEvent("account.sign_in")
			event.FailureCode = stringPointer(code)
			if err := event.Validate(); err != nil {
				t.Fatalf("account.sign_in failure code %q rejected: %v", code, err)
			}
		})
	}

	for _, code := range []string{"unknown", "invalid_credentials_extra", "THROTTLED", ""} {
		t.Run("account.sign_in rejects "+code, func(t *testing.T) {
			event := validCatalogEvent("account.sign_in")
			event.FailureCode = stringPointer(code)
			if err := event.Validate(); err == nil {
				t.Fatalf("account.sign_in accepted disallowed failure code %q", code)
			}
		})
	}
}

func TestFailureCodeRejectedForOtherCatalogActions(t *testing.T) {
	for action := range coveredActions {
		if action == "account.sign_in" {
			continue
		}
		for _, code := range []string{"invalid_credentials", "throttled"} {
			t.Run(action+"/"+code, func(t *testing.T) {
				event := validCatalogEvent(action)
				event.FailureCode = stringPointer(code)
				if err := event.Validate(); err == nil {
					t.Fatalf("action %q accepted supplied failure code %q", action, code)
				}
			})
		}
	}
}

type writeCountingSink struct {
	writes int
}

func (s *writeCountingSink) Write(p []byte) (int, error) {
	s.writes++
	return len(p), nil
}

func TestLogRecorderRejectsDisallowedFailureCodeBeforeSinkWrite(t *testing.T) {
	tests := []struct {
		name   string
		action string
		code   string
	}{
		{name: "unknown sign-in code", action: "account.sign_in", code: "account_locked"},
		{name: "known code on another action", action: "account.created", code: "invalid_credentials"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &writeCountingSink{}
			event := validCatalogEvent(tc.action)
			event.FailureCode = stringPointer(tc.code)

			if err := NewLogRecorder(sink).Record(context.Background(), event); err == nil {
				t.Fatalf("Record accepted failure code %q for action %q", tc.code, tc.action)
			}
			if sink.writes != 0 {
				t.Fatalf("disallowed failure code reached sink: writes = %d, want 0", sink.writes)
			}
		})
	}
}
