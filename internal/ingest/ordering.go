package ingest

import (
	"sort"
	"sync"
	"time"

	"concrete-specimen-chain-service/internal/domain"
)

const OutOfOrderWindow = 10 * time.Minute

// Buffer maintains the fixed ten-minute specimen watermark and stable sort key.
type Buffer struct {
	mu          sync.Mutex
	pending     []Event
	maxSeen     time.Time
	closedUntil time.Time
}

func (b *Buffer) Rejects(occurredAt time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.closedUntil.IsZero() && occurredAt.Before(b.closedUntil)
}

func (b *Buffer) Add(event Event) (time.Time, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closedUntil.IsZero() && event.OccurredAt.Before(b.closedUntil) {
		return b.closedUntil, domain.ErrLateEvent
	}
	if event.OccurredAt.After(b.maxSeen) {
		b.maxSeen = event.OccurredAt
	}
	b.pending = append(b.pending, event)
	return b.watermark(), nil
}

func (b *Buffer) Watermark() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.watermark()
}

func (b *Buffer) watermark() time.Time {
	if b.maxSeen.IsZero() {
		return time.Time{}
	}
	return b.maxSeen.Add(-OutOfOrderWindow)
}

// Advance uses an explicit business time and never reads the system clock.
func (b *Buffer) Advance(until time.Time) []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	if until.After(b.closedUntil) {
		b.closedUntil = until
	}
	ready := make([]Event, 0, len(b.pending))
	remaining := b.pending[:0]
	for _, event := range b.pending {
		if !event.OccurredAt.After(until) {
			ready = append(ready, event)
		} else {
			remaining = append(remaining, event)
		}
	}
	b.pending = remaining
	sort.SliceStable(ready, func(i, j int) bool {
		left, right := ready[i], ready[j]
		if !left.OccurredAt.Equal(right.OccurredAt) {
			return left.OccurredAt.Before(right.OccurredAt)
		}
		if left.Type.Priority() != right.Type.Priority() {
			return left.Type.Priority() < right.Type.Priority()
		}
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		return left.Sequence < right.Sequence
	})
	return ready
}

// RestoreClosedUntil rehydrates the last durable watermark before pending
// records are added during startup recovery.
func (b *Buffer) RestoreClosedUntil(until time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if until.After(b.closedUntil) {
		b.closedUntil = until
	}
}

// Pending returns a copy for diagnostics and checkpoint creation.
func (b *Buffer) Pending() []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]Event(nil), b.pending...)
}
