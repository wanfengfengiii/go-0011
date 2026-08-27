package calculation_test

import (
	"testing"
	"time"

	"concrete-specimen-chain-service/internal/calculation"
	"concrete-specimen-chain-service/internal/domain"
)

func frozenRule(t *testing.T) domain.FrozenRule {
	t.Helper()
	rule, err := domain.DefaultInspectionRule("p1", 1).Freeze(30, 150)
	if err != nil {
		t.Fatal(err)
	}
	return rule
}

func TestCuringAtTwentyDegreesForOneDay(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	points := make([]calculation.TemperaturePoint, 0, 12)
	for hour := 0; hour < 24; hour += 2 {
		points = append(points, calculation.TemperaturePoint{OccurredAt: start.Add(time.Duration(hour) * time.Hour), TemperatureC: 20})
	}
	result := calculation.CalculateCuring(points, start.Add(24*time.Hour), frozenRule(t))
	if result.EquivalentDays != 1 || result.Validity != domain.ValidityValid {
		t.Fatalf("curing result = %+v", result)
	}
}

func TestCuringCountsOnlyGapBeyondHoldWindowAsMissing(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	points := []calculation.TemperaturePoint{{OccurredAt: start, TemperatureC: 20}, {OccurredAt: start.Add(3 * time.Hour), TemperatureC: 20}}
	result := calculation.CalculateCuring(points, start.Add(3*time.Hour), frozenRule(t))
	if result.MissingMinutes != 60 || result.EquivalentMinutes != 120 || result.Validity != domain.ValidityValid {
		t.Fatalf("curing result = %+v", result)
	}
}

func TestCuringInvalidityBoundaries(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t.Run("continuous missing over limit", func(t *testing.T) {
		points := []calculation.TemperaturePoint{{OccurredAt: start, TemperatureC: 20}, {OccurredAt: start.Add(8*time.Hour + time.Minute), TemperatureC: 20}}
		result := calculation.CalculateCuring(points, start.Add(8*time.Hour+time.Minute), frozenRule(t))
		if result.Validity != domain.ValidityMissingData {
			t.Fatalf("validity = %s", result.Validity)
		}
	})
	t.Run("continuous temperature excursion over limit", func(t *testing.T) {
		points := []calculation.TemperaturePoint{
			{OccurredAt: start, TemperatureC: 40},
			{OccurredAt: start.Add(time.Hour), TemperatureC: 40},
			{OccurredAt: start.Add(2 * time.Hour), TemperatureC: 40},
		}
		result := calculation.CalculateCuring(points, start.Add(2*time.Hour+time.Minute), frozenRule(t))
		if result.Validity != domain.ValidityTemperatureRange {
			t.Fatalf("validity = %s", result.Validity)
		}
	})
}
