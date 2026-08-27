package calculation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"time"

	"concrete-specimen-chain-service/internal/domain"
)

type LoadPoint struct {
	Elapsed time.Duration `json:"elapsed"`
	LoadKN  float64       `json:"load_kn"`
}

type PressureInput struct {
	SpecimenID, MachineID string
	LoadUnit, SideUnit    string
	SideMM                int
	DeclaredPeakKN        float64
	Curve                 []LoadPoint
}

func CalculatePressure(input PressureInput, rule domain.FrozenRule) (domain.PressureResult, error) {
	if input.LoadUnit != "kN" || input.SideUnit != "mm" || input.SideMM != rule.NominalSideMM {
		return domain.PressureResult{}, domain.ErrUnitOrDimension
	}
	if len(input.Curve) < 5 {
		return domain.PressureResult{}, domain.ErrAbnormalLoadCurve
	}
	peakIndex, firstPositive := 0, -1
	for index, point := range input.Curve {
		if point.LoadKN < 0 || index > 0 && point.Elapsed <= input.Curve[index-1].Elapsed {
			return domain.PressureResult{}, domain.ErrAbnormalLoadCurve
		}
		if firstPositive < 0 && point.LoadKN > 0 {
			firstPositive = index
		}
		if point.LoadKN > input.Curve[peakIndex].LoadKN {
			peakIndex = index
		}
	}
	peak := input.Curve[peakIndex].LoadKN
	if peak <= 0 || firstPositive < 0 || input.Curve[peakIndex].Elapsed-input.Curve[firstPositive].Elapsed < 3*time.Second {
		return domain.PressureResult{}, domain.ErrAbnormalLoadCurve
	}
	for index := firstPositive + 1; index <= peakIndex; index++ {
		if input.Curve[index-1].LoadKN-input.Curve[index].LoadKN > peak*.15 {
			return domain.PressureResult{}, domain.ErrAbnormalLoadCurve
		}
	}
	dropped := false
	for _, point := range input.Curve[peakIndex+1:] {
		if peak-point.LoadKN >= peak*.1 {
			dropped = true
			break
		}
	}
	if !dropped || math.Abs(input.DeclaredPeakKN-peak) > peak*.01 {
		return domain.PressureResult{}, domain.ErrAbnormalLoadCurve
	}
	strength := peak * 1000 / float64(input.SideMM*input.SideMM) * rule.DimensionFactor
	canonical, _ := json.Marshal(input.Curve)
	digest := sha256.Sum256(canonical)
	return domain.PressureResult{
		SpecimenID: input.SpecimenID, MachineID: input.MachineID,
		CurveDigest: hex.EncodeToString(digest[:]), PeakLoadKN: peak,
		SideMM: input.SideMM, Factor: rule.DimensionFactor,
		StrengthMPa: math.Round(strength*10) / 10, Validity: domain.ValidityValid,
	}, nil
}
