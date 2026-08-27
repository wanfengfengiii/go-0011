package domain

import (
	"encoding/json"
	"time"
)

type EventType string

const (
	EventSampled       EventType = "SAMPLED"
	EventMolded        EventType = "MOLDED"
	EventTemperature   EventType = "TEMPERATURE"
	EventDemolded      EventType = "DEMOLDED"
	EventTransported   EventType = "TRANSPORTED"
	EventTestStarted   EventType = "TEST_STARTED"
	EventLoadCurve     EventType = "LOAD_CURVE"
	EventTestCompleted EventType = "TEST_COMPLETED"
)

func (t EventType) Priority() int {
	order := map[EventType]int{
		EventSampled: 1, EventMolded: 2, EventTemperature: 3, EventDemolded: 4,
		EventTransported: 5, EventTestStarted: 6, EventLoadCurve: 7, EventTestCompleted: 8,
	}
	return order[t]
}

type EventRecord struct {
	GlobalPosition   uint64          `json:"global_position"`
	EventID          string          `json:"event_id"`
	Source           string          `json:"source"`
	SpecimenID       string          `json:"specimen_id"`
	Sequence         uint64          `json:"sequence"`
	OccurredAt       time.Time       `json:"occurred_at"`
	ReceivedAt       time.Time       `json:"received_at"`
	ExpectedVersion  uint64          `json:"expected_version"`
	Type             EventType       `json:"type"`
	CanonicalPayload json.RawMessage `json:"canonical_payload"`
	PayloadDigest    string          `json:"payload_digest"`
	AppliedStatus    string          `json:"applied_status"`
	ClassifiedError  string          `json:"classified_error,omitempty"`
}

type PressureResult struct {
	SpecimenID, MachineID, CurveDigest string
	PeakLoadKN, Factor, StrengthMPa    float64
	SideMM                             int
	Validity                           Validity
}
