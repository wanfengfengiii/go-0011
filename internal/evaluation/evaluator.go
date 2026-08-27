package evaluation

import (
	"math"
	"sort"
	"time"

	"concrete-specimen-chain-service/internal/domain"
)

// FreezeInitial copies all evaluation inputs before calculating the conclusion.
func FreezeInitial(groupID string, groupVersion uint64, rule domain.FrozenRule, results []SpecimenResult, createdAt time.Time) (Snapshot, error) {
	return freeze(Snapshot{
		GroupID: groupID, Kind: SnapshotInitial, GroupVersion: groupVersion,
		Rule: rule, SpecimenResults: append([]SpecimenResult(nil), results...), CreatedAt: createdAt.UTC(),
	})
}

func freeze(snapshot Snapshot) (Snapshot, error) {
	sort.Slice(snapshot.SpecimenResults, func(i, j int) bool {
		return snapshot.SpecimenResults[i].SpecimenID < snapshot.SpecimenResults[j].SpecimenID
	})
	validCount, sum, minimum := 0, 0.0, math.Inf(1)
	for _, result := range snapshot.SpecimenResults {
		if result.Validity != domain.ValidityValid {
			continue
		}
		validCount++
		sum += result.StrengthMPa
		minimum = math.Min(minimum, result.StrengthMPa)
	}
	if validCount < snapshot.Rule.RequiredSpecimens || validCount != len(snapshot.SpecimenResults) {
		snapshot.CalculatedConclusion = domain.ConclusionInvalid
		snapshot.MinimumStrengthMPa = 0
	} else {
		snapshot.MeanStrengthMPa = math.Round(sum/float64(validCount)*10) / 10
		snapshot.MinimumStrengthMPa = minimum
		if snapshot.MeanStrengthMPa >= snapshot.Rule.DesignStrengthMPa*snapshot.Rule.MeanFactor &&
			minimum >= snapshot.Rule.DesignStrengthMPa*snapshot.Rule.MinimumFactor {
			snapshot.CalculatedConclusion = domain.ConclusionPassed
		} else {
			snapshot.CalculatedConclusion = domain.ConclusionFailed
		}
	}
	return sealDigest(snapshot)
}
