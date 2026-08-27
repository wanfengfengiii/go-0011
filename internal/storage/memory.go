// Package storage defines event persistence/recovery contracts and provides a
// concurrency-safe in-memory implementation for the executable foundation.
package storage

import (
	"context"
	"sync"
	"time"

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/ingest"
)

type storedIdentity struct {
	digest  string
	receipt ingest.Receipt
}

type MemoryRepository struct {
	mu         sync.Mutex
	clock      func() time.Time
	identities map[string]storedIdentity
	versions   map[string]uint64
	events     []domain.EventRecord
	status     ingest.RecoveryStatus
}

func NewMemoryRepository(clock func() time.Time) *MemoryRepository {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &MemoryRepository{
		clock: clock, identities: make(map[string]storedIdentity), versions: make(map[string]uint64),
		status: ingest.RecoveryStatus{Ready: true, Phase: "ready"},
	}
}

func (r *MemoryRepository) CommitEvent(_ context.Context, event ingest.Event) (ingest.Receipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, exists := r.identities[event.Key()]; exists {
		if prior.digest != event.PayloadDigest {
			return ingest.Receipt{}, domain.ErrIdentityConflict
		}
		duplicate := prior.receipt
		duplicate.Status = "duplicate"
		return duplicate, nil
	}
	current := r.versions[event.SpecimenID]
	if event.ExpectedVersion != current {
		return ingest.Receipt{}, domain.ErrVersionConflict
	}
	version := current + 1
	receipt := ingest.Receipt{Status: "buffered", Version: version}
	record := domain.EventRecord{
		GlobalPosition: uint64(len(r.events) + 1), EventID: event.Key(), Source: event.Source,
		SpecimenID: event.SpecimenID, Sequence: event.Sequence, OccurredAt: event.OccurredAt,
		ReceivedAt: r.clock(), ExpectedVersion: event.ExpectedVersion, Type: event.Type,
		CanonicalPayload: append([]byte(nil), event.CanonicalPayload...), PayloadDigest: event.PayloadDigest,
		AppliedStatus: receipt.Status,
	}
	r.events = append(r.events, record)
	r.versions[event.SpecimenID] = version
	r.identities[event.Key()] = storedIdentity{digest: event.PayloadDigest, receipt: receipt}
	r.status.LastGlobalPosition = record.GlobalPosition
	return receipt, nil
}

func (r *MemoryRepository) Receipt(_ context.Context, key, digest string) (ingest.Receipt, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prior, exists := r.identities[key]
	if !exists {
		return ingest.Receipt{}, false, nil
	}
	if prior.digest != digest {
		return ingest.Receipt{}, false, domain.ErrIdentityConflict
	}
	return prior.receipt, true, nil
}

func (r *MemoryRepository) Ready(_ context.Context) (ingest.RecoveryStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status, nil
}

func (r *MemoryRepository) Events() []domain.EventRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]domain.EventRecord, len(r.events))
	copy(result, r.events)
	return result
}
