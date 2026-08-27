package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/ingest"
	"concrete-specimen-chain-service/internal/storage"
)

func storageEvent(t *testing.T, source string, sequence, expected uint64) ingest.Event {
	t.Helper()
	event, err := ingest.Normalize(ingest.Envelope{
		Source: source, SpecimenID: "s1", Sequence: sequence, ExpectedVersion: expected,
		OccurredAt: time.Date(2026, 4, 1, 0, 0, int(sequence), 0, time.UTC),
		Type:       domain.EventSampled, Payload: json.RawMessage(`{"identity":"seal-1"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func TestMemoryRepositoryIdempotency(t *testing.T) {
	repository := storage.NewMemoryRepository(func() time.Time { return time.Unix(1, 0).UTC() })
	event := storageEvent(t, "device", 1, 0)
	first, err := repository.CommitEvent(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.CommitEvent(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 || second.Status != "duplicate" || len(repository.Events()) != 1 {
		t.Fatalf("receipts = %+v %+v, events = %d", first, second, len(repository.Events()))
	}
}

func TestMemoryRepositoryConcurrentExpectedVersion(t *testing.T) {
	repository := storage.NewMemoryRepository(nil)
	events := []ingest.Event{storageEvent(t, "a", 1, 0), storageEvent(t, "b", 1, 0)}
	errorsSeen := make([]error, 2)
	var wait sync.WaitGroup
	for index := range events {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			_, errorsSeen[index] = repository.CommitEvent(context.Background(), events[index])
		}(index)
	}
	wait.Wait()
	successes, conflicts := 0, 0
	for _, err := range errorsSeen {
		if err == nil {
			successes++
		} else if errors.Is(err, domain.ErrVersionConflict) {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 || len(repository.Events()) != 1 {
		t.Fatalf("successes=%d conflicts=%d records=%d errors=%v", successes, conflicts, len(repository.Events()), errorsSeen)
	}
}
