package domain

// GroupStatus is the legal main state of a sample group.
type GroupStatus string

const (
	GroupAwaitingSampling GroupStatus = "AWAITING_SAMPLING"
	GroupCuring           GroupStatus = "CURING"
	GroupAwaitingTest     GroupStatus = "AWAITING_TEST"
	GroupTesting          GroupStatus = "TESTING"
	GroupAwaitingReview   GroupStatus = "AWAITING_REVIEW"
	GroupPassed           GroupStatus = "PASSED"
	GroupFailed           GroupStatus = "FAILED"
	GroupInvalid          GroupStatus = "INVALID"
)

func (s GroupStatus) IsSealed() bool {
	return s == GroupPassed || s == GroupFailed || s == GroupInvalid
}

// CanTransition enforces the documented group state machine. Cancellation is
// represented by allowTestCancellation and is valid only from TESTING.
func CanTransition(from, to GroupStatus, allowTestCancellation bool) bool {
	if from == GroupTesting && to == GroupAwaitingTest {
		return allowTestCancellation
	}
	allowed := map[GroupStatus][]GroupStatus{
		GroupAwaitingSampling: {GroupCuring},
		GroupCuring:           {GroupAwaitingTest},
		GroupAwaitingTest:     {GroupTesting},
		GroupTesting:          {GroupAwaitingReview},
		GroupAwaitingReview:   {GroupPassed, GroupFailed, GroupInvalid},
	}
	for _, candidate := range allowed[from] {
		if candidate == to {
			return true
		}
	}
	return false
}
