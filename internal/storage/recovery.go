package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"concrete-specimen-chain-service/internal/domain"
)

type Checkpoint struct {
	GlobalPosition  uint64          `json:"global_position"`
	AggregateDigest string          `json:"aggregate_digest"`
	SnapshotBlob    json.RawMessage `json:"snapshot_blob"`
	CreatedAt       time.Time       `json:"created_at"`
}

type Replayer interface {
	RestoreCheckpoint(context.Context, json.RawMessage) error
	ApplyRecord(context.Context, domain.EventRecord) error
	Digest(context.Context) (string, error)
	Reset(context.Context) error
}

// Recover restores a valid checkpoint, replays committed records, and falls
// back to a full replay when the recovered aggregate digest disagrees.
func Recover(ctx context.Context, repository TransactionalRepository, aggregate Replayer) (uint64, error) {
	checkpoint, err := repository.LoadCheckpoint(ctx)
	if err != nil {
		return 0, err
	}
	position := uint64(0)
	if checkpoint != nil {
		if restoreErr := aggregate.RestoreCheckpoint(ctx, checkpoint.SnapshotBlob); restoreErr == nil {
			digest, digestErr := aggregate.Digest(ctx)
			if digestErr != nil {
				return 0, digestErr
			}
			if digest == checkpoint.AggregateDigest {
				position = checkpoint.GlobalPosition
			} else if err := aggregate.Reset(ctx); err != nil {
				return 0, err
			}
		} else if err := aggregate.Reset(ctx); err != nil {
			return 0, err
		}
	}
	return replayAfter(ctx, repository, aggregate, position)
}

func replayAfter(ctx context.Context, repository TransactionalRepository, aggregate Replayer, after uint64) (uint64, error) {
	records, err := repository.LoadEventsAfter(ctx, after)
	if err != nil {
		return after, err
	}
	position := after
	for _, record := range records {
		if err := aggregate.ApplyRecord(ctx, record); err != nil {
			return position, err
		}
		position = record.GlobalPosition
	}
	return position, nil
}

func Digest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
