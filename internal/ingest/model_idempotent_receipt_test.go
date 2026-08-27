package ingest_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/ingest"
	"concrete-specimen-chain-service/internal/storage"
)

func TestModel_IdempotentReceiptPreservesVersionAndWatermark(t *testing.T) {
	testCases := []struct {
		name             string
		restartBeforeTry bool
		retryPayload     json.RawMessage
		wantConflict     bool
	}{
		{
			name:         "identical canonical payload in the same process",
			retryPayload: json.RawMessage(`{"unit":"C","temperature_c":20}`),
		},
		{
			name:             "identical canonical payload after process restart",
			restartBeforeTry: true,
			retryPayload:     json.RawMessage(`{"unit":"C","temperature_c":20}`),
		},
		{
			name:         "same identity with a different canonical payload",
			retryPayload: json.RawMessage(`{"unit":"C","temperature_c":21}`),
			wantConflict: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "events.db")
			repository, err := storage.OpenSQLite(ctx, path, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = repository.Close() }()

			service := ingest.NewService(repository)
			occurredAt := time.Date(2026, 4, 5, 6, 30, 0, 0, time.UTC)
			firstEnvelope := ingest.Envelope{
				Source:          "field-device-1",
				SpecimenID:      "specimen-1",
				Sequence:        41,
				OccurredAt:      occurredAt,
				ExpectedVersion: 0,
				Type:            domain.EventTemperature,
				Payload:         json.RawMessage(`{"temperature_c":20,"unit":"C"}`),
			}
			first, err := service.Submit(ctx, firstEnvelope)
			if err != nil {
				t.Fatalf("first submit: %v", err)
			}
			wantWatermark := occurredAt.Add(-10 * time.Minute)
			if first.Status != "buffered" || first.Version != 1 || !first.Watermark.Equal(wantWatermark) {
				t.Fatalf("first receipt = %+v, want status buffered, version 1, watermark %s", first, wantWatermark)
			}

			if testCase.restartBeforeTry {
				if err := repository.Close(); err != nil {
					t.Fatal(err)
				}
				repository, err = storage.OpenSQLite(ctx, path, nil)
				if err != nil {
					t.Fatal(err)
				}
				service = ingest.NewService(repository)
				if err := service.RecoverPending(ctx); err != nil {
					t.Fatalf("recover pending events: %v", err)
				}
			}

			retryEnvelope := firstEnvelope
			retryEnvelope.Payload = testCase.retryPayload
			retry, retryErr := service.Submit(ctx, retryEnvelope)
			if testCase.wantConflict {
				if !errors.Is(retryErr, domain.ErrIdentityConflict) {
					t.Fatalf("retry error = %v, want %v", retryErr, domain.ErrIdentityConflict)
				}
			} else {
				if retryErr != nil {
					t.Fatalf("retry submit: %v", retryErr)
				}
				if retry.Status != "duplicate" || retry.Version != first.Version || !retry.Watermark.Equal(first.Watermark) {
					t.Errorf("retry receipt = %+v, want duplicate with original version %d and watermark %s", retry, first.Version, first.Watermark)
				}
			}

			count, err := repository.EventCount(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Errorf("event count after retry = %d, want 1", count)
			}

			followUp := firstEnvelope
			followUp.Sequence++
			followUp.ExpectedVersion = first.Version
			followUp.OccurredAt = occurredAt.Add(time.Minute)
			followUp.Payload = json.RawMessage(`{"temperature_c":20.5,"unit":"C"}`)
			followReceipt, err := service.Submit(ctx, followUp)
			if err != nil {
				t.Fatalf("follow-up submit after retry: %v", err)
			}
			if followReceipt.Version != 2 {
				t.Errorf("follow-up version = %d, want 2", followReceipt.Version)
			}
		})
	}
}
