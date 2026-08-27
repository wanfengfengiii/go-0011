package domain

import (
	"fmt"
	"time"
)

// InspectionRule is the mutable catalog rule. Freeze creates the immutable
// value copied into each new sample group.
type InspectionRule struct {
	ID                     string          `json:"id"`
	ProjectID              string          `json:"project_id"`
	Revision               int             `json:"revision"`
	TargetEquivalentDays   float64         `json:"target_equivalent_days"`
	RequiredSpecimens      int             `json:"required_specimens"`
	AllowedTemperatureMinC float64         `json:"allowed_temperature_min_c"`
	AllowedTemperatureMaxC float64         `json:"allowed_temperature_max_c"`
	MissingLimitMinutes    int             `json:"missing_limit_minutes"`
	OutOfRangeLimitMinutes int             `json:"out_of_range_limit_minutes"`
	DimensionFactors       map[int]float64 `json:"dimension_factors"`
	MeanFactor             float64         `json:"mean_factor"`
	MinimumFactor          float64         `json:"minimum_factor"`
	CreatedAt              time.Time       `json:"created_at"`
}

type FrozenRule struct {
	Revision               int     `json:"revision"`
	DesignStrengthMPa      float64 `json:"design_strength_mpa"`
	TargetEquivalentDays   float64 `json:"target_equivalent_days"`
	RequiredSpecimens      int     `json:"required_specimens"`
	NominalSideMM          int     `json:"nominal_side_mm"`
	AllowedTemperatureMinC float64 `json:"allowed_temperature_min_c"`
	AllowedTemperatureMaxC float64 `json:"allowed_temperature_max_c"`
	MissingLimitMinutes    int     `json:"missing_limit_minutes"`
	OutOfRangeLimitMinutes int     `json:"out_of_range_limit_minutes"`
	DimensionFactor        float64 `json:"dimension_factor"`
	MeanFactor             float64 `json:"mean_factor"`
	MinimumFactor          float64 `json:"minimum_factor"`
}

func DefaultInspectionRule(projectID string, revision int) InspectionRule {
	return InspectionRule{
		ProjectID: projectID, Revision: revision, TargetEquivalentDays: 28,
		RequiredSpecimens: 3, AllowedTemperatureMinC: 5, AllowedTemperatureMaxC: 35,
		MissingLimitMinutes: 360, OutOfRangeLimitMinutes: 120,
		DimensionFactors: map[int]float64{100: .95, 150: 1, 200: 1.05},
		MeanFactor:       1.15, MinimumFactor: .95,
	}
}

func (r InspectionRule) Freeze(designStrength float64, sideMM int) (FrozenRule, error) {
	factor, ok := r.DimensionFactors[sideMM]
	if !ok || designStrength <= 0 || r.RequiredSpecimens <= 0 {
		return FrozenRule{}, fmt.Errorf("invalid frozen rule: %w", ErrUnitOrDimension)
	}
	return FrozenRule{
		Revision: r.Revision, DesignStrengthMPa: designStrength,
		TargetEquivalentDays: r.TargetEquivalentDays, RequiredSpecimens: r.RequiredSpecimens,
		NominalSideMM: sideMM, AllowedTemperatureMinC: r.AllowedTemperatureMinC,
		AllowedTemperatureMaxC: r.AllowedTemperatureMaxC,
		MissingLimitMinutes:    r.MissingLimitMinutes, OutOfRangeLimitMinutes: r.OutOfRangeLimitMinutes,
		DimensionFactor: factor, MeanFactor: r.MeanFactor, MinimumFactor: r.MinimumFactor,
	}, nil
}
