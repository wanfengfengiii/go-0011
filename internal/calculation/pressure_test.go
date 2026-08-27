package calculation_test

import (
	"errors"
	"testing"
	"time"

	"concrete-specimen-chain-service/internal/calculation"
	"concrete-specimen-chain-service/internal/domain"
)

func validPressure() calculation.PressureInput {
	return calculation.PressureInput{
		SpecimenID: "s1", MachineID: "m1", LoadUnit: "kN", SideUnit: "mm", SideMM: 150,
		DeclaredPeakKN: 500,
		Curve: []calculation.LoadPoint{
			{Elapsed: 0, LoadKN: 0}, {Elapsed: time.Second, LoadKN: 100},
			{Elapsed: 2 * time.Second, LoadKN: 300}, {Elapsed: 4 * time.Second, LoadKN: 500},
			{Elapsed: 5 * time.Second, LoadKN: 400},
		},
	}
}

func TestPressureStrengthAndRounding(t *testing.T) {
	result, err := calculation.CalculatePressure(validPressure(), frozenRule(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.StrengthMPa != 22.2 || result.Factor != 1 || result.CurveDigest == "" {
		t.Fatalf("pressure result = %+v", result)
	}
}

func TestPressureRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name   string
		change func(*calculation.PressureInput)
		want   error
	}{
		{"unit", func(input *calculation.PressureInput) { input.LoadUnit = "N" }, domain.ErrUnitOrDimension},
		{"dimension", func(input *calculation.PressureInput) { input.SideMM = 120 }, domain.ErrUnitOrDimension},
		{"declared peak", func(input *calculation.PressureInput) { input.DeclaredPeakKN = 450 }, domain.ErrAbnormalLoadCurve},
		{"no post peak drop", func(input *calculation.PressureInput) { input.Curve[4].LoadKN = 480 }, domain.ErrAbnormalLoadCurve},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validPressure()
			test.change(&input)
			_, err := calculation.CalculatePressure(input, frozenRule(t))
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}
