package calculation

import "time"

// Clock is the explicit time seam used by orchestration and deterministic tests.
type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }
