package ingest_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/ingest"
)

func orderedEvent(t *testing.T, kind domain.EventType, source string, sequence uint64, at time.Time) ingest.Event {
	t.Helper()
	event, err := ingest.Normalize(ingest.Envelope{
		Source: source, SpecimenID: "s1", Sequence: sequence, OccurredAt: at,
		Type: kind, Payload: json.RawMessage(`{"ok":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func TestBufferUsesStableBusinessOrder(t *testing.T) {
	at := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	buffer := &ingest.Buffer{}
	for _, event := range []ingest.Event{
		orderedEvent(t, domain.EventTransported, "b", 2, at),
		orderedEvent(t, domain.EventTemperature, "z", 3, at),
		orderedEvent(t, domain.EventDemolded, "a", 1, at),
	} {
		if _, err := buffer.Add(event); err != nil {
			t.Fatal(err)
		}
	}
	ready := buffer.Advance(at)
	got := []domain.EventType{ready[0].Type, ready[1].Type, ready[2].Type}
	want := []domain.EventType{domain.EventTemperature, domain.EventDemolded, domain.EventTransported}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestBufferRejectsEventBeforeClosedWatermark(t *testing.T) {
	at := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	buffer := &ingest.Buffer{}
	buffer.Advance(at)
	_, err := buffer.Add(orderedEvent(t, domain.EventTransported, "a", 1, at.Add(-time.Second)))
	if !errors.Is(err, domain.ErrLateEvent) {
		t.Fatalf("late error = %v", err)
	}
}
