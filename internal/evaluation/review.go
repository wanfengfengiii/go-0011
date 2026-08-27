package evaluation

import (
	"sync"
	"time"

	"concrete-specimen-chain-service/internal/domain"
)

type DisputeType string

const (
	DisputeMachineCalibration DisputeType = "MACHINE_CALIBRATION"
	DisputeDimensionEntry     DisputeType = "DIMENSION_ENTRY"
	DisputeSpecimenIdentity   DisputeType = "SPECIMEN_IDENTITY"
)

// SealState coordinates the one-review and one-seal invariants for a group.
type SealState struct {
	mu         sync.Mutex
	reviewed   bool
	sealedAt   *time.Time
	conclusion domain.Conclusion
	digest     string
}

func (s *SealState) Review(original Snapshot, dispute DisputeType, corrected []SpecimenResult, evidenceRefs []string, at time.Time) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reviewed || s.sealedAt != nil || len(evidenceRefs) == 0 || !validDispute(dispute) {
		return Snapshot{}, domain.ErrIllegalTransition
	}
	review, err := freeze(Snapshot{
		GroupID: original.GroupID, Kind: SnapshotReview, ParentSnapshotID: original.ID,
		GroupVersion: original.GroupVersion + 1, Rule: original.Rule,
		SpecimenResults: append([]SpecimenResult(nil), corrected...),
		EvidenceRefs:    append([]string(nil), evidenceRefs...), CreatedAt: at.UTC(),
	})
	if err != nil {
		return Snapshot{}, err
	}
	s.reviewed = true
	s.seal(review, at)
	return review, nil
}

func (s *SealState) Seal(initial Snapshot, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sealedAt != nil {
		return domain.ErrIllegalTransition
	}
	s.seal(initial, at)
	return nil
}

func (s *SealState) seal(snapshot Snapshot, at time.Time) {
	sealedAt := at.UTC()
	s.sealedAt = &sealedAt
	s.conclusion = snapshot.CalculatedConclusion
	s.digest = snapshot.CanonicalDigest
}

func (s *SealState) Sealed() (domain.Conclusion, time.Time, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sealedAt == nil {
		return "", time.Time{}, "", false
	}
	return s.conclusion, *s.sealedAt, s.digest, true
}

func validDispute(dispute DisputeType) bool {
	return dispute == DisputeMachineCalibration || dispute == DisputeDimensionEntry || dispute == DisputeSpecimenIdentity
}
