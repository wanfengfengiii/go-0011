package ingest

import (
	"context"
	"sync"
	"time"

	"concrete-specimen-chain-service/internal/domain"
)

// Repository is the atomic event/idempotency/version storage boundary.
type Repository interface {
	CommitEvent(context.Context, Event) (Receipt, error)
	Receipt(context.Context, string, string) (Receipt, bool, error)
	Ready(context.Context) (RecoveryStatus, error)
}

type RecoveryStatus struct {
	Ready              bool   `json:"ready"`
	Phase              string `json:"phase"`
	LastGlobalPosition uint64 `json:"last_global_position"`
}

// Service is the application entry point for event ingestion.
type Service struct {
	repository Repository
	mu         sync.Mutex
	submitMu   sync.Mutex
	buffers    map[string]*Buffer
	validators []EventValidator
}

type EventValidator func(Event) error

type pendingRepository interface {
	PendingEvents(context.Context) ([]Event, map[string]time.Time, error)
	MarkApplied(context.Context, string, []Event, time.Time) error
}

func NewService(repository Repository, validators ...EventValidator) *Service {
	return &Service{repository: repository, buffers: make(map[string]*Buffer), validators: validators}
}

// RecoverPending rehydrates the durable sorting window. It is safe to invoke
// once before serving writes; repositories without persistent pending state do
// not require recovery.
func (s *Service) RecoverPending(ctx context.Context) error {
	persistent, ok := s.repository.(pendingRepository)
	if !ok {
		return nil
	}
	events, watermarks, err := persistent.PendingEvents(ctx)
	if err != nil {
		return err
	}
	for specimenID, until := range watermarks {
		s.buffer(specimenID).RestoreClosedUntil(until)
	}
	for _, event := range events {
		if _, err := s.buffer(event.SpecimenID).Add(event); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Submit(ctx context.Context, envelope Envelope) (Receipt, error) {
	event, err := Normalize(envelope)
	if err != nil {
		return Receipt{}, err
	}
	for _, validate := range s.validators {
		if err := validate(event); err != nil {
			return Receipt{}, err
		}
	}
	status, err := s.repository.Ready(ctx)
	if err != nil {
		return Receipt{}, err
	}
	if !status.Ready {
		return Receipt{}, domain.NewError("RECOVERY_IN_PROGRESS", "AVAILABILITY", true)
	}
	if prior, found, err := s.repository.Receipt(ctx, event.Key(), event.PayloadDigest); err != nil {
		return Receipt{}, err
	} else if found {
		prior.Status = "duplicate"
		return prior, nil
	}
	s.submitMu.Lock()
	defer s.submitMu.Unlock()
	buffer := s.buffer(event.SpecimenID)
	if buffer.Rejects(event.OccurredAt) {
		return Receipt{}, domain.ErrLateEvent
	}
	receipt, err := s.repository.CommitEvent(ctx, event)
	if err != nil {
		return Receipt{}, err
	}
	if receipt.Status == "duplicate" {
		return receipt, nil
	}
	watermark, err := buffer.Add(event)
	if err != nil {
		return Receipt{}, err
	}
	receipt.Watermark = watermark
	return receipt, nil
}

func (s *Service) Advance(specimenID string, until time.Time) []Event {
	s.submitMu.Lock()
	defer s.submitMu.Unlock()
	ready := s.buffer(specimenID).Advance(until)
	if persistent, ok := s.repository.(pendingRepository); ok {
		// The API deliberately has no ambient clock or request dependency. A
		// failed durable update leaves facts intact and recoverable on restart.
		if err := persistent.MarkApplied(context.Background(), specimenID, ready, until); err != nil {
			return nil
		}
	}
	return ready
}

func (s *Service) buffer(specimenID string) *Buffer {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buffers[specimenID] == nil {
		s.buffers[specimenID] = &Buffer{}
	}
	return s.buffers[specimenID]
}
