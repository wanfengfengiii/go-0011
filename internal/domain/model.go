package domain

import "time"

type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	SiteCode  string    `json:"site_code"`
	CreatedAt time.Time `json:"created_at"`
}

type PourSection struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"project_id"`
	Name          string    `json:"name"`
	Location      string    `json:"location"`
	PlannedPourAt time.Time `json:"planned_pour_at"`
}

type MixDesign struct {
	ID                string  `json:"id"`
	ProjectID         string  `json:"project_id"`
	Code              string  `json:"code"`
	MaterialRevision  string  `json:"material_revision"`
	DesignStrengthMPa float64 `json:"design_strength_mpa"`
}

type SampleGroup struct {
	ID               string      `json:"id"`
	PourSectionID    string      `json:"pour_section_id"`
	MixDesignID      string      `json:"mix_design_id"`
	Rule             FrozenRule  `json:"frozen_rule"`
	Status           GroupStatus `json:"status"`
	Version          uint64      `json:"version"`
	SampledAt        *time.Time  `json:"sampled_at,omitempty"`
	ReviewCount      int         `json:"review_count"`
	FrozenSnapshotID string      `json:"frozen_snapshot_id,omitempty"`
	SealedConclusion Conclusion  `json:"sealed_conclusion,omitempty"`
	SealedAt         *time.Time  `json:"sealed_at,omitempty"`
	SealedDigest     string      `json:"sealed_digest,omitempty"`
}

type Validity string

const (
	ValidityValid            Validity = "VALID"
	ValidityMissingData      Validity = "MISSING_DATA"
	ValidityTemperatureRange Validity = "TEMPERATURE_OUT_OF_RANGE"
	ValidityDeviceData       Validity = "DEVICE_DATA_INVALID"
)

type Specimen struct {
	ID                  string    `json:"id"`
	GroupID             string    `json:"group_id"`
	SpecimenNo          string    `json:"specimen_no"`
	BoundIdentity       string    `json:"bound_identity,omitempty"`
	NominalSideMM       int       `json:"nominal_side_mm"`
	Version             uint64    `json:"version"`
	LastAppliedAt       time.Time `json:"last_applied_at,omitempty"`
	MaxSeenAt           time.Time `json:"max_seen_at,omitempty"`
	EffectiveAgeMinutes int       `json:"effective_age_minutes"`
	Validity            Validity  `json:"validity"`
	CurrentLocation     string    `json:"current_location,omitempty"`
}

type Conclusion string

const (
	ConclusionPassed  Conclusion = "PASSED"
	ConclusionFailed  Conclusion = "FAILED"
	ConclusionInvalid Conclusion = "INVALID"
)
