package ingest_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/ingest"
)

func envelope(payload string) ingest.Envelope {
	return ingest.Envelope{
		Source: "sensor-1", SpecimenID: "specimen-1", Sequence: 1,
		OccurredAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Type:       domain.EventTemperature, Payload: json.RawMessage(payload),
	}
}

func TestNormalizeCanonicalPayload(t *testing.T) {
	left, err := ingest.Normalize(envelope(`{"temperature_c":20,"unit":"C"}`))
	if err != nil {
		t.Fatal(err)
	}
	right, err := ingest.Normalize(envelope(`{"unit":"C", "temperature_c":20}`))
	if err != nil {
		t.Fatal(err)
	}
	if left.PayloadDigest != right.PayloadDigest {
		t.Fatalf("canonical digests differ: %s and %s", left.PayloadDigest, right.PayloadDigest)
	}
}

func TestIdempotencyBookDuplicateAndConflict(t *testing.T) {
	book := ingest.NewIdempotencyBook()
	first, _ := ingest.Normalize(envelope(`{"temperature_c":20}`))
	receipt := ingest.Receipt{Status: "buffered", Version: 1}
	if _, duplicate, err := book.Remember(first, receipt); err != nil || duplicate {
		t.Fatalf("first remember = duplicate %v, error %v", duplicate, err)
	}
	got, duplicate, err := book.Remember(first, receipt)
	if err != nil || !duplicate || got.Status != "duplicate" || got.Version != 1 {
		t.Fatalf("duplicate receipt = %+v, duplicate %v, error %v", got, duplicate, err)
	}
	changed, _ := ingest.Normalize(envelope(`{"temperature_c":21}`))
	if _, _, err := book.Remember(changed, receipt); !errors.Is(err, domain.ErrIdentityConflict) {
		t.Fatalf("conflict error = %v", err)
	}
}
