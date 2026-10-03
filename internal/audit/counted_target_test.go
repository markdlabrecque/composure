package audit

import (
	"context"
	"fmt"
	"testing"
)

func countedFailureEvent(count int64) Event {
	event := validEvent()
	event.Count = count
	event.FirstTime = stringPointer("2026-09-30T18:41:01.000Z")
	event.LastTime = stringPointer(event.Time)
	return event
}

func TestCountedTargetContract(t *testing.T) {
	t.Run("aggregated failures require the action operation target", func(t *testing.T) {
		invalidTargets := []struct {
			name   string
			target *Target
		}{
			{name: "null", target: nil},
			{name: "resource", target: &Target{Kind: "account", ID: "018f47d2-9c2a-7abc-8def-0123456789ab"}},
			{name: "mismatched operation", target: &Target{Kind: "operation", ID: "password.changed"}},
		}

		for _, count := range []int64{1, 3} {
			for _, tc := range invalidTargets {
				t.Run(fmt.Sprintf("%s/count=%d", tc.name, count), func(t *testing.T) {
					event := countedFailureEvent(count)
					event.Target = tc.target

					if err := event.Validate(); err == nil {
						t.Error("Validate accepted an aggregated failure without the action's fixed operation target")
					}

					sink := &writeCountingSink{}
					if err := NewLogRecorder(sink).Record(context.Background(), event); err == nil {
						t.Error("Record accepted an aggregated failure without the action's fixed operation target")
					}
					if sink.writes != 0 {
						t.Fatalf("invalid aggregate reached sink: writes = %d, want 0", sink.writes)
					}
				})
			}
		}
	})

	t.Run("aggregated failures allow the action operation target", func(t *testing.T) {
		for _, count := range []int64{1, 3} {
			event := countedFailureEvent(count)
			if err := event.Validate(); err != nil {
				t.Fatalf("valid aggregate with count %d rejected: %v", count, err)
			}
		}
	})

	t.Run("ordinary single events retain envelope target choices", func(t *testing.T) {
		targets := []struct {
			name   string
			target *Target
		}{
			{name: "null", target: nil},
			{name: "resource", target: &Target{Kind: "account", ID: "018f47d2-9c2a-7abc-8def-0123456789ab"}},
			{name: "action operation", target: &Target{Kind: "operation", ID: "account.sign_in"}},
		}

		for _, tc := range targets {
			t.Run(tc.name, func(t *testing.T) {
				event := validEvent()
				event.Target = tc.target
				if err := event.Validate(); err != nil {
					t.Fatalf("valid ordinary event rejected: %v", err)
				}
			})
		}
	})
}
