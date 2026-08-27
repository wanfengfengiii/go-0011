package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

type checkpointState struct {
	GlobalPosition uint64              `json:"global_position"`
	Events         int                 `json:"events"`
	Pending        []checkpointPending `json:"pending"`
	Versions       []checkpointVersion `json:"versions"`
	Groups         []checkpointGroup   `json:"groups"`
}

type checkpointPending struct {
	EventID, SpecimenID, SortTime string
	Priority                      int
}

type checkpointVersion struct {
	SpecimenID string
	Version    uint64
}

type checkpointGroup struct {
	GroupID, Status, SnapshotID, SealedDigest string
	Version                                   uint64
}

// CreateCheckpoint captures the recovery-critical indexes in a consistent
// read transaction and stores a canonical digest beside the blob.
func (r *SQLiteRepository) CreateCheckpoint(ctx context.Context) (Checkpoint, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Checkpoint{}, err
	}
	defer tx.Rollback()
	state := checkpointState{}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(global_position), 0), COUNT(*) FROM specimen_events`).
		Scan(&state.GlobalPosition, &state.Events); err != nil {
		return Checkpoint{}, err
	}
	pendingRows, err := tx.QueryContext(ctx, `SELECT event_id, specimen_id, sort_time, business_priority
        FROM pending_events ORDER BY specimen_id, sort_time, business_priority, event_id`)
	if err != nil {
		return Checkpoint{}, err
	}
	for pendingRows.Next() {
		var item checkpointPending
		if err := pendingRows.Scan(&item.EventID, &item.SpecimenID, &item.SortTime, &item.Priority); err != nil {
			pendingRows.Close()
			return Checkpoint{}, err
		}
		state.Pending = append(state.Pending, item)
	}
	if err := pendingRows.Close(); err != nil {
		return Checkpoint{}, err
	}
	versionRows, err := tx.QueryContext(ctx, `SELECT id, version FROM specimens ORDER BY id`)
	if err != nil {
		return Checkpoint{}, err
	}
	for versionRows.Next() {
		var item checkpointVersion
		if err := versionRows.Scan(&item.SpecimenID, &item.Version); err != nil {
			versionRows.Close()
			return Checkpoint{}, err
		}
		state.Versions = append(state.Versions, item)
	}
	if err := versionRows.Close(); err != nil {
		return Checkpoint{}, err
	}
	groupRows, err := tx.QueryContext(ctx, `SELECT id, status, version, frozen_snapshot_id, sealed_digest
        FROM sample_groups ORDER BY id`)
	if err != nil {
		return Checkpoint{}, err
	}
	for groupRows.Next() {
		var item checkpointGroup
		if err := groupRows.Scan(&item.GroupID, &item.Status, &item.Version, &item.SnapshotID, &item.SealedDigest); err != nil {
			groupRows.Close()
			return Checkpoint{}, err
		}
		state.Groups = append(state.Groups, item)
	}
	if err := groupRows.Close(); err != nil {
		return Checkpoint{}, err
	}
	blob, err := json.Marshal(state)
	if err != nil {
		return Checkpoint{}, err
	}
	sum := sha256.Sum256(blob)
	checkpoint := Checkpoint{GlobalPosition: state.GlobalPosition, AggregateDigest: hex.EncodeToString(sum[:]),
		SnapshotBlob: blob, CreatedAt: r.clock().UTC()}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO checkpoints
        (global_position, aggregate_digest, snapshot_blob, created_at) VALUES (?, ?, ?, ?)`,
		checkpoint.GlobalPosition, checkpoint.AggregateDigest, []byte(checkpoint.SnapshotBlob),
		checkpoint.CreatedAt.Format(time.RFC3339Nano)); err != nil {
		return Checkpoint{}, err
	}
	if err := tx.Commit(); err != nil {
		return Checkpoint{}, err
	}
	return checkpoint, nil
}

// VerifyLatestCheckpoint distinguishes a valid recovery accelerator from a
// torn or externally corrupted blob. Callers then recover from durable tables.
func (r *SQLiteRepository) VerifyLatestCheckpoint(ctx context.Context) (bool, error) {
	checkpoint, err := r.LoadCheckpoint(ctx)
	if err != nil || checkpoint == nil {
		return checkpoint == nil, err
	}
	sum := sha256.Sum256(checkpoint.SnapshotBlob)
	if hex.EncodeToString(sum[:]) != checkpoint.AggregateDigest {
		return false, nil
	}
	var state checkpointState
	if err := json.Unmarshal(checkpoint.SnapshotBlob, &state); err != nil {
		return false, nil
	}
	position, err := r.lastPosition(ctx)
	if err != nil {
		return false, fmt.Errorf("verify checkpoint position: %w", err)
	}
	return state.GlobalPosition == checkpoint.GlobalPosition && checkpoint.GlobalPosition <= position, nil
}
