package evaluation_test

import (
	"errors"
	"testing"
	"time"

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/evaluation"
)

func evaluationRule(t *testing.T) domain.FrozenRule {
	t.Helper()
	catalog := domain.DefaultInspectionRule("p1", 1)
	catalog.RequiredSpecimens = 3
	rule, err := catalog.Freeze(30, 150)
	if err != nil {
		t.Fatal(err)
	}
	return rule
}

func results(strengths ...float64) []evaluation.SpecimenResult {
	items := make([]evaluation.SpecimenResult, len(strengths))
	for index, strength := range strengths {
		items[index] = evaluation.SpecimenResult{SpecimenID: string(rune('a' + index)), StrengthMPa: strength, Validity: domain.ValidityValid}
	}
	return items
}

func TestFreezeEvaluatesOnlyCopiedSnapshot(t *testing.T) {
	at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	inputs := results(36, 35, 34)
	snapshot, err := evaluation.FreezeInitial("g1", 4, evaluationRule(t), inputs, at)
	if err != nil {
		t.Fatal(err)
	}
	inputs[0].StrengthMPa = 0
	if snapshot.CalculatedConclusion != domain.ConclusionPassed || snapshot.SpecimenResults[0].StrengthMPa == 0 {
		t.Fatalf("snapshot changed or conclusion incorrect: %+v", snapshot)
	}
}

func TestFreezeMarksInsufficientValidSpecimensInvalid(t *testing.T) {
	items := results(40, 40, 40)
	items[2].Validity = domain.ValidityDeviceData
	snapshot, err := evaluation.FreezeInitial("g1", 4, evaluationRule(t), items, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CalculatedConclusion != domain.ConclusionInvalid {
		t.Fatalf("conclusion = %s", snapshot.CalculatedConclusion)
	}
}

func TestReviewOnceAndSealOnce(t *testing.T) {
	at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	initial, err := evaluation.FreezeInitial("g1", 4, evaluationRule(t), results(30, 30, 30), at)
	if err != nil {
		t.Fatal(err)
	}
	state := &evaluation.SealState{}
	review, err := state.Review(initial, evaluation.DisputeMachineCalibration, results(40, 40, 40), []string{"event-9"}, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if review.ParentSnapshotID != initial.ID || review.CalculatedConclusion != domain.ConclusionPassed || initial.CalculatedConclusion != domain.ConclusionFailed {
		t.Fatalf("review = %+v, initial = %+v", review, initial)
	}
	if _, err := state.Review(initial, evaluation.DisputeMachineCalibration, results(40, 40, 40), []string{"event-9"}, at); !errors.Is(err, domain.ErrIllegalTransition) {
		t.Fatalf("second review error = %v", err)
	}
	if err := state.Seal(review, at.Add(2*time.Hour)); !errors.Is(err, domain.ErrIllegalTransition) {
		t.Fatalf("second seal error = %v", err)
	}
}
