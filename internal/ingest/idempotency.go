package ingest

import (
	"sync"
	"time"

	"concrete-specimen-chain-service/internal/domain"
)

type Receipt struct {
	Status    string    `json:"status"`
	Version   uint64    `json:"version"`
	Watermark time.Time `json:"watermark"`
}

type recordedReceipt struct {
	digest  string
	receipt Receipt
}

// IdempotencyBook provides the source/specimen/sequence identity boundary.
type IdempotencyBook struct {
	mu      sync.Mutex
	entries map[string]recordedReceipt
}

func NewIdempotencyBook() *IdempotencyBook {
	return &IdempotencyBook{entries: make(map[string]recordedReceipt)}
}

// Remember returns the original receipt for an identical duplicate.
func (b *IdempotencyBook) Remember(event Event, receipt Receipt) (Receipt, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := event.Key()
	if prior, exists := b.entries[key]; exists {
		if prior.digest != event.PayloadDigest {
			return Receipt{}, false, domain.ErrIdentityConflict
		}
		prior.receipt.Status = "duplicate"
		return prior.receipt, true, nil
	}
	b.entries[key] = recordedReceipt{digest: event.PayloadDigest, receipt: receipt}
	return receipt, false, nil
}
