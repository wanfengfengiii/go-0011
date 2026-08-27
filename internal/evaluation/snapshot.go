// Package evaluation freezes specimen results and derives one-time group conclusions.
package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"concrete-specimen-chain-service/internal/domain"
)

type SpecimenResult struct {
	SpecimenID  string          `json:"specimen_id"`
	StrengthMPa float64         `json:"strength_mpa"`
	Validity    domain.Validity `json:"validity"`
}

type SnapshotKind string

const (
	SnapshotInitial SnapshotKind = "INITIAL"
	SnapshotReview  SnapshotKind = "REVIEW"
)

type Snapshot struct {
	ID                   string            `json:"id"`
	GroupID              string            `json:"group_id"`
	Kind                 SnapshotKind      `json:"kind"`
	ParentSnapshotID     string            `json:"parent_snapshot_id,omitempty"`
	GroupVersion         uint64            `json:"group_version"`
	Rule                 domain.FrozenRule `json:"rule"`
	SpecimenResults      []SpecimenResult  `json:"specimen_results"`
	EvidenceRefs         []string          `json:"evidence_refs,omitempty"`
	CalculatedConclusion domain.Conclusion `json:"calculated_conclusion"`
	MeanStrengthMPa      float64           `json:"mean_strength_mpa"`
	MinimumStrengthMPa   float64           `json:"minimum_strength_mpa"`
	CanonicalDigest      string            `json:"canonical_digest"`
	CreatedAt            time.Time         `json:"created_at"`
}

func sealDigest(snapshot Snapshot) (Snapshot, error) {
	copyForDigest := snapshot
	copyForDigest.ID = ""
	copyForDigest.CanonicalDigest = ""
	encoded, err := json.Marshal(copyForDigest)
	if err != nil {
		return Snapshot{}, err
	}
	sum := sha256.Sum256(encoded)
	snapshot.CanonicalDigest = hex.EncodeToString(sum[:])
	snapshot.ID = snapshot.CanonicalDigest[:16]
	return snapshot, nil
}
