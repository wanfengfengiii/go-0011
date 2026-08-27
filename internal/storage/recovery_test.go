package storage_test

import (
	"context"
	"encoding/json"
	"testing"

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/storage"
)

type recoveryRepository struct {
	checkpoint *storage.Checkpoint
	records    []domain.EventRecord
}

func (r recoveryRepository) WithinTransaction(context.Context, func(storage.Transaction) error) error {
	return nil
}
func (r recoveryRepository) LoadEventsAfter(_ context.Context, after uint64) ([]domain.EventRecord, error) {
	var records []domain.EventRecord
	for _, record := range r.records {
		if record.GlobalPosition > after {
			records = append(records, record)
		}
	}
	return records, nil
}
func (r recoveryRepository) LoadCheckpoint(context.Context) (*storage.Checkpoint, error) {
	return r.checkpoint, nil
}
func (r recoveryRepository) SaveCheckpoint(context.Context, storage.Checkpoint) error { return nil }

type recoveryAggregate struct {
	digest  string
	applied []uint64
	resets  int
}

func (a *recoveryAggregate) RestoreCheckpoint(_ context.Context, blob json.RawMessage) error {
	return json.Unmarshal(blob, &a.digest)
}
func (a *recoveryAggregate) ApplyRecord(_ context.Context, record domain.EventRecord) error {
	a.applied = append(a.applied, record.GlobalPosition)
	return nil
}
func (a *recoveryAggregate) Digest(context.Context) (string, error) { return a.digest, nil }
func (a *recoveryAggregate) Reset(context.Context) error {
	a.resets++
	a.applied = nil
	a.digest = ""
	return nil
}

func TestRecoverReplaysAfterValidCheckpoint(t *testing.T) {
	repository := recoveryRepository{
		checkpoint: &storage.Checkpoint{GlobalPosition: 2, AggregateDigest: "valid", SnapshotBlob: json.RawMessage(`"valid"`)},
		records:    []domain.EventRecord{{GlobalPosition: 1}, {GlobalPosition: 2}, {GlobalPosition: 3}},
	}
	aggregate := &recoveryAggregate{}
	position, err := storage.Recover(context.Background(), repository, aggregate)
	if err != nil {
		t.Fatal(err)
	}
	if position != 3 || len(aggregate.applied) != 1 || aggregate.applied[0] != 3 || aggregate.resets != 0 {
		t.Fatalf("position=%d aggregate=%+v", position, aggregate)
	}
}

func TestRecoverFallsBackOnDigestMismatch(t *testing.T) {
	repository := recoveryRepository{
		checkpoint: &storage.Checkpoint{GlobalPosition: 2, AggregateDigest: "expected", SnapshotBlob: json.RawMessage(`"different"`)},
		records:    []domain.EventRecord{{GlobalPosition: 1}, {GlobalPosition: 2}, {GlobalPosition: 3}},
	}
	aggregate := &recoveryAggregate{}
	position, err := storage.Recover(context.Background(), repository, aggregate)
	if err != nil {
		t.Fatal(err)
	}
	if position != 3 || len(aggregate.applied) != 3 || aggregate.resets != 1 {
		t.Fatalf("position=%d aggregate=%+v", position, aggregate)
	}
}
