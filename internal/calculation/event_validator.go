package calculation

import (
	"encoding/json"

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/ingest"
)

type pressurePayload struct {
	MachineID      string            `json:"machine_id"`
	LoadUnit       string            `json:"load_unit"`
	SideUnit       string            `json:"side_unit"`
	SideMM         int               `json:"side_mm"`
	DeclaredPeakKN float64           `json:"declared_peak_kn"`
	Curve          []LoadPoint       `json:"curve"`
	Rule           domain.FrozenRule `json:"frozen_rule"`
}

// ValidateEvent connects pressure curve validation to the documented event
// transaction boundary. Other event types are left to their aggregate rules.
func ValidateEvent(event ingest.Event) error {
	_, _, err := PressureFromEvent(event)
	return err
}

// PressureFromEvent parses and evaluates a LOAD_CURVE event so a repository
// can persist the accepted pressure result in the same event transaction.
func PressureFromEvent(event ingest.Event) (domain.PressureResult, bool, error) {
	if event.Type != domain.EventLoadCurve {
		return domain.PressureResult{}, false, nil
	}
	var payload pressurePayload
	if err := json.Unmarshal(event.CanonicalPayload, &payload); err != nil {
		return domain.PressureResult{}, true, domain.NewError("VALIDATION", "VALIDATION", false)
	}
	result, err := CalculatePressure(PressureInput{
		SpecimenID: event.SpecimenID, MachineID: payload.MachineID,
		LoadUnit: payload.LoadUnit, SideUnit: payload.SideUnit, SideMM: payload.SideMM,
		DeclaredPeakKN: payload.DeclaredPeakKN, Curve: payload.Curve,
	}, payload.Rule)
	return result, true, err
}
