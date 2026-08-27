// Package ingest normalizes specimen event envelopes and provides deterministic
// idempotency and watermark ordering primitives.
package ingest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"concrete-specimen-chain-service/internal/domain"
)

type Envelope struct {
	Source          string           `json:"source"`
	SpecimenID      string           `json:"specimen_id"`
	Sequence        uint64           `json:"sequence"`
	OccurredAt      time.Time        `json:"occurred_at"`
	ExpectedVersion uint64           `json:"expected_version"`
	Type            domain.EventType `json:"type"`
	Payload         json.RawMessage  `json:"payload"`
}

type Event struct {
	Envelope
	CanonicalPayload json.RawMessage
	PayloadDigest    string
}

func (e Envelope) Key() string {
	return fmt.Sprintf("%s/%s/%d", e.Source, e.SpecimenID, e.Sequence)
}

func Normalize(e Envelope) (Event, error) {
	if e.Source == "" || e.SpecimenID == "" || e.Sequence == 0 || e.OccurredAt.IsZero() || e.Type.Priority() == 0 || len(e.Payload) == 0 {
		return Event{}, domain.NewError("VALIDATION", "VALIDATION", false)
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(e.Payload))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return Event{}, domain.NewError("VALIDATION", "VALIDATION", false)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return Event{}, err
	}
	sum := sha256.Sum256(canonical)
	return Event{Envelope: e, CanonicalPayload: canonical, PayloadDigest: hex.EncodeToString(sum[:])}, nil
}
