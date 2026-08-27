package storage

import (
	"context"

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/evaluation"
)

// Transaction is the atomic boundary for a completed pressure test and its
// specimen/group versions, pressure result, and optional frozen snapshot.
type Transaction interface {
	AppendEvent(domain.EventRecord) error
	SavePressureResult(domain.PressureResult) error
	CompareGroupVersion(groupID string, expected uint64) error
	SaveSnapshot(evaluation.Snapshot) error
}

// TransactionalRepository is the stable persistence seam intended for the
// SQLite implementation in the next project increment.
type TransactionalRepository interface {
	WithinTransaction(context.Context, func(Transaction) error) error
	LoadEventsAfter(context.Context, uint64) ([]domain.EventRecord, error)
	LoadCheckpoint(context.Context) (*Checkpoint, error)
	SaveCheckpoint(context.Context, Checkpoint) error
}
