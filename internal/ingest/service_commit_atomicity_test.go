package ingest_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/ingest"
	"concrete-specimen-chain-service/internal/storage"
)

func TestModel_CommitFailureDoesNotPolluteOrdering(t *testing.T) {
	base := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name             string
		conflictOffset   time.Duration
		followupOffset   time.Duration
		hasFollowup      bool
		advanceOffset    time.Duration
		wantWatermark    time.Time
		wantAppliedKeys  []string
		wantPersistedIDs []string
	}{
		{
			name:             "version conflict is absent when watermark advances",
			conflictOffset:   time.Minute,
			advanceOffset:    time.Minute,
			wantAppliedKeys:  []string{"collector-a/specimen-1/1"},
			wantPersistedIDs: []string{"collector-a/specimen-1/1"},
		},
		{
			name:             "version conflict cannot raise a later receipt watermark",
			conflictOffset:   30 * time.Minute,
			followupOffset:   15 * time.Minute,
			hasFollowup:      true,
			advanceOffset:    30 * time.Minute,
			wantWatermark:    base.Add(5 * time.Minute),
			wantAppliedKeys:  []string{"collector-a/specimen-1/1", "collector-c/specimen-1/1"},
			wantPersistedIDs: []string{"collector-a/specimen-1/1", "collector-c/specimen-1/1"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repository := storage.NewMemoryRepository(func() time.Time { return base })
			service := ingest.NewService(repository)

			first := ingest.Envelope{
				Source: "collector-a", SpecimenID: "specimen-1", Sequence: 1,
				OccurredAt: base, ExpectedVersion: 0, Type: domain.EventSampled,
				Payload: json.RawMessage(`{"value":"accepted-first"}`),
			}
			firstReceipt, err := service.Submit(ctx, first)
			if err != nil || firstReceipt.Version != 1 {
				t.Fatalf("first Submit() receipt = %+v, error = %v", firstReceipt, err)
			}

			conflicting := ingest.Envelope{
				Source: "collector-b", SpecimenID: "specimen-1", Sequence: 1,
				OccurredAt: base.Add(test.conflictOffset), ExpectedVersion: 0,
				Type: domain.EventSampled, Payload: json.RawMessage(`{"value":"rejected"}`),
			}
			if _, err := service.Submit(ctx, conflicting); !errors.Is(err, domain.ErrVersionConflict) {
				t.Fatalf("conflicting Submit() error = %v, want %v", err, domain.ErrVersionConflict)
			}

			if test.hasFollowup {
				followup := ingest.Envelope{
					Source: "collector-c", SpecimenID: "specimen-1", Sequence: 1,
					OccurredAt: base.Add(test.followupOffset), ExpectedVersion: 1,
					Type: domain.EventSampled, Payload: json.RawMessage(`{"value":"accepted-followup"}`),
				}
				receipt, err := service.Submit(ctx, followup)
				if err != nil {
					t.Fatalf("follow-up Submit() error = %v", err)
				}
				if !receipt.Watermark.Equal(test.wantWatermark) {
					t.Errorf("follow-up watermark = %v, want %v", receipt.Watermark, test.wantWatermark)
				}
			}

			applied := service.Advance("specimen-1", base.Add(test.advanceOffset))
			appliedKeys := make([]string, len(applied))
			for index := range applied {
				appliedKeys[index] = applied[index].Key()
			}
			if !reflect.DeepEqual(appliedKeys, test.wantAppliedKeys) {
				t.Errorf("Advance() keys = %v, want %v", appliedKeys, test.wantAppliedKeys)
			}

			persisted := repository.Events()
			persistedIDs := make([]string, len(persisted))
			for index := range persisted {
				persistedIDs[index] = persisted[index].EventID
			}
			if !reflect.DeepEqual(persistedIDs, test.wantPersistedIDs) {
				t.Errorf("persisted event IDs = %v, want %v", persistedIDs, test.wantPersistedIDs)
			}
		})
	}
}
