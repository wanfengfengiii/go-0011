package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/evaluation"
)

type EvaluationState struct {
	Snapshot evaluation.Snapshot `json:"snapshot"`
	Group    domain.SampleGroup  `json:"group"`
}

// SaveInitialEvaluation uses the group version as a compare-and-swap guard so
// concurrent final-specimen completions cannot create two initial snapshots.
func (r *SQLiteRepository) SaveInitialEvaluation(ctx context.Context, snapshot evaluation.Snapshot) error {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE sample_groups SET status = ?, version = version + 1,
        frozen_snapshot_id = ? WHERE id = ? AND version = ? AND frozen_snapshot_id = ''`,
		domain.GroupAwaitingReview, snapshot.ID, snapshot.GroupID, snapshot.GroupVersion)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return domain.ErrVersionConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO evaluation_snapshots
        (id, group_id, kind, parent_snapshot_id, group_version, canonical_json, canonical_digest, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, snapshot.ID, snapshot.GroupID, snapshot.Kind,
		snapshot.ParentSnapshotID, snapshot.GroupVersion, encoded, snapshot.CanonicalDigest,
		snapshot.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return tx.Commit()
}

// SaveReviewAndSeal appends a linked correction snapshot and seals the group
// in the same transaction without mutating the original snapshot.
func (r *SQLiteRepository) SaveReviewAndSeal(ctx context.Context, snapshot evaluation.Snapshot, sealedAt time.Time) error {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE sample_groups SET status = ?, version = version + 1,
        frozen_snapshot_id = ?, review_count = 1, sealed_conclusion = ?, sealed_at = ?, sealed_digest = ?
        WHERE id = ? AND status = ? AND review_count = 0 AND sealed_at IS NULL`, conclusionStatus(snapshot.CalculatedConclusion),
		snapshot.ID, snapshot.CalculatedConclusion, sealedAt.UTC().Format(time.RFC3339Nano), snapshot.CanonicalDigest,
		snapshot.GroupID, domain.GroupAwaitingReview)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return domain.ErrIllegalTransition
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO evaluation_snapshots
        (id, group_id, kind, parent_snapshot_id, group_version, canonical_json, canonical_digest, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, snapshot.ID, snapshot.GroupID, snapshot.Kind,
		snapshot.ParentSnapshotID, snapshot.GroupVersion, encoded, snapshot.CanonicalDigest,
		snapshot.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLiteRepository) SealEvaluation(ctx context.Context, groupID string, snapshot evaluation.Snapshot, sealedAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE sample_groups SET status = ?, version = version + 1,
        sealed_conclusion = ?, sealed_at = ?, sealed_digest = ?
        WHERE id = ? AND status = ? AND sealed_at IS NULL`, conclusionStatus(snapshot.CalculatedConclusion),
		snapshot.CalculatedConclusion, sealedAt.UTC().Format(time.RFC3339Nano), snapshot.CanonicalDigest,
		groupID, domain.GroupAwaitingReview)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return domain.ErrIllegalTransition
	}
	return nil
}

func (r *SQLiteRepository) Evaluation(ctx context.Context, groupID string) (EvaluationState, error) {
	group, _, err := r.SampleGroup(ctx, groupID)
	if err != nil {
		return EvaluationState{}, err
	}
	if group.FrozenSnapshotID == "" {
		return EvaluationState{Group: group}, nil
	}
	var encoded []byte
	err = r.db.QueryRowContext(ctx, `SELECT canonical_json FROM evaluation_snapshots WHERE id = ?`,
		group.FrozenSnapshotID).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return EvaluationState{}, domain.NewError("SNAPSHOT_NOT_FOUND", "RECOVERY", true)
	}
	if err != nil {
		return EvaluationState{}, err
	}
	var snapshot evaluation.Snapshot
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		return EvaluationState{}, err
	}
	return EvaluationState{Snapshot: snapshot, Group: group}, nil
}

func conclusionStatus(conclusion domain.Conclusion) domain.GroupStatus {
	switch conclusion {
	case domain.ConclusionPassed:
		return domain.GroupPassed
	case domain.ConclusionFailed:
		return domain.GroupFailed
	default:
		return domain.GroupInvalid
	}
}

func (r *SQLiteRepository) LoadCheckpoint(ctx context.Context) (*Checkpoint, error) {
	var checkpoint Checkpoint
	var created string
	err := r.db.QueryRowContext(ctx, `SELECT global_position, aggregate_digest, snapshot_blob, created_at
        FROM checkpoints ORDER BY global_position DESC LIMIT 1`).Scan(&checkpoint.GlobalPosition,
		&checkpoint.AggregateDigest, &checkpoint.SnapshotBlob, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	checkpoint.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return &checkpoint, nil
}

func (r *SQLiteRepository) SaveCheckpoint(ctx context.Context, checkpoint Checkpoint) error {
	_, err := r.db.ExecContext(ctx, `INSERT OR REPLACE INTO checkpoints
        (global_position, aggregate_digest, snapshot_blob, created_at) VALUES (?, ?, ?, ?)`,
		checkpoint.GlobalPosition, checkpoint.AggregateDigest, []byte(checkpoint.SnapshotBlob),
		checkpoint.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}
