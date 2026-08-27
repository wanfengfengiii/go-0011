// Package calculation implements the frozen curing and pressure rules.
package calculation

import (
	"math"
	"sort"
	"time"

	"concrete-specimen-chain-service/internal/domain"
)

type TemperaturePoint struct {
	OccurredAt   time.Time `json:"occurred_at"`
	TemperatureC float64   `json:"temperature_c"`
}

type CuringResult struct {
	EquivalentMinutes float64         `json:"equivalent_minutes"`
	EquivalentDays    float64         `json:"equivalent_days"`
	MissingMinutes    int             `json:"missing_minutes"`
	Validity          domain.Validity `json:"validity"`
}

// CalculateCuring integrates prior-value temperature readings by whole minute.
func CalculateCuring(points []TemperaturePoint, end time.Time, rule domain.FrozenRule) CuringResult {
	result := CuringResult{Validity: domain.ValidityValid}
	if len(points) == 0 {
		result.Validity = domain.ValidityMissingData
		return result
	}
	ordered := append([]TemperaturePoint(nil), points...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].OccurredAt.Before(ordered[j].OccurredAt) })
	maxMissing, outOfRangeRun, maxOutOfRange := 0, 0, 0
	for index, point := range ordered {
		segmentEnd := end
		if index+1 < len(ordered) && ordered[index+1].OccurredAt.Before(segmentEnd) {
			segmentEnd = ordered[index+1].OccurredAt
		}
		minutes := int(segmentEnd.Sub(point.OccurredAt) / time.Minute)
		if minutes <= 0 {
			continue
		}
		observedMinutes := min(minutes, 120)
		result.EquivalentMinutes += float64(observedMinutes) * maturityFactor(point.TemperatureC)
		if point.TemperatureC < rule.AllowedTemperatureMinC || point.TemperatureC > rule.AllowedTemperatureMaxC {
			outOfRangeRun += observedMinutes
			maxOutOfRange = max(maxOutOfRange, outOfRangeRun)
		} else {
			outOfRangeRun = 0
		}
		if missing := minutes - observedMinutes; missing > 0 {
			result.MissingMinutes += missing
			maxMissing = max(maxMissing, missing)
			outOfRangeRun = 0
		}
	}
	if maxMissing > rule.MissingLimitMinutes {
		result.Validity = domain.ValidityMissingData
	} else if maxOutOfRange > rule.OutOfRangeLimitMinutes {
		result.Validity = domain.ValidityTemperatureRange
	}
	result.EquivalentDays = math.Round(result.EquivalentMinutes/1440*1000) / 1000
	return result
}

func maturityFactor(temperatureC float64) float64 {
	switch {
	case temperatureC < 0:
		return 0
	case temperatureC < 5:
		return .25
	case temperatureC < 10:
		return .5
	case temperatureC < 15:
		return .75
	case temperatureC < 25:
		return 1
	case temperatureC < 35:
		return 1.2
	default:
		return .8
	}
}
