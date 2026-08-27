package domain_test

import (
	"testing"

	"concrete-specimen-chain-service/internal/domain"
)

func TestGroupTransitions(t *testing.T) {
	tests := []struct {
		name     string
		from, to domain.GroupStatus
		cancel   bool
		want     bool
	}{
		{"sampling to curing", domain.GroupAwaitingSampling, domain.GroupCuring, false, true},
		{"cannot skip curing", domain.GroupAwaitingSampling, domain.GroupAwaitingTest, false, false},
		{"explicit test cancellation", domain.GroupTesting, domain.GroupAwaitingTest, true, true},
		{"cannot leave sealed state", domain.GroupPassed, domain.GroupTesting, false, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := domain.CanTransition(test.from, test.to, test.cancel); got != test.want {
				t.Fatalf("CanTransition() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestFreezeRuleCopiesDimensionFactor(t *testing.T) {
	rule := domain.DefaultInspectionRule("project-1", 3)
	frozen, err := rule.Freeze(40, 150)
	if err != nil {
		t.Fatal(err)
	}
	rule.DimensionFactors[150] = 9
	if frozen.DimensionFactor != 1 || frozen.DesignStrengthMPa != 40 || frozen.Revision != 3 {
		t.Fatalf("unexpected frozen rule: %+v", frozen)
	}
}
