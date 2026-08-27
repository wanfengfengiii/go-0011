package httpapi

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/evaluation"
	"concrete-specimen-chain-service/internal/storage"
)

type evaluationRepository interface {
	SaveInitialEvaluation(context.Context, evaluation.Snapshot) error
	SaveReviewAndSeal(context.Context, evaluation.Snapshot, time.Time) error
	SealEvaluation(context.Context, string, evaluation.Snapshot, time.Time) error
	Evaluation(context.Context, string) (storage.EvaluationState, error)
}

type groupEntry struct {
	snapshot evaluation.Snapshot
	seal     *evaluation.SealState
}

type groupRegistry struct {
	mu      sync.Mutex
	entries map[string]*groupEntry
}

func newGroupRegistry() *groupRegistry { return &groupRegistry{entries: make(map[string]*groupEntry)} }

type evaluateRequest struct {
	GroupVersion uint64                      `json:"group_version"`
	Rule         domain.FrozenRule           `json:"rule"`
	Results      []evaluation.SpecimenResult `json:"results"`
	CreatedAt    time.Time                   `json:"created_at"`
}

type reviewRequest struct {
	Dispute      evaluation.DisputeType      `json:"dispute"`
	Results      []evaluation.SpecimenResult `json:"results"`
	EvidenceRefs []string                    `json:"evidence_refs"`
	ReviewedAt   time.Time                   `json:"reviewed_at"`
}

func (s *Server) groupAction(writer http.ResponseWriter, request *http.Request) {
	path := strings.TrimPrefix(request.URL.Path, "/v1/sample-groups/")
	groupID, action, found := strings.Cut(path, "/")
	if groupID == "" {
		http.NotFound(writer, request)
		return
	}
	if !found && request.Method == http.MethodGet {
		s.group(writer, request, groupID)
		return
	}
	if !found {
		http.NotFound(writer, request)
		return
	}
	switch request.Method + " " + action {
	case "POST evaluate":
		s.evaluate(writer, request, groupID)
	case "POST review":
		s.review(writer, request, groupID)
	case "POST seal":
		s.seal(writer, request, groupID)
	case "POST watermark:advance":
		s.advanceWatermark(writer, request)
	case "GET digest":
		s.digest(writer, request, groupID)
	default:
		http.NotFound(writer, request)
	}
}

func (s *Server) advanceWatermark(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		SpecimenIDs []string  `json:"specimen_ids"`
		Until       time.Time `json:"until"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Until.IsZero() || len(input.SpecimenIDs) == 0 {
		writeError(writer, validationError())
		return
	}
	result := make(map[string][]string, len(input.SpecimenIDs))
	for _, specimenID := range input.SpecimenIDs {
		for _, event := range s.events.Advance(specimenID, input.Until) {
			result[specimenID] = append(result[specimenID], event.Key())
		}
	}
	writeJSON(writer, http.StatusOK, result)
}

func (s *Server) evaluate(writer http.ResponseWriter, request *http.Request, groupID string) {
	var input evaluateRequest
	if err := decodeJSON(request, &input); err != nil || input.CreatedAt.IsZero() {
		writeError(writer, validationError())
		return
	}
	snapshot, err := evaluation.FreezeInitial(groupID, input.GroupVersion, input.Rule, input.Results, input.CreatedAt)
	if err != nil {
		writeError(writer, err)
		return
	}
	if s.evaluations != nil {
		if err := s.evaluations.SaveInitialEvaluation(request.Context(), snapshot); err != nil {
			writeError(writer, err)
			return
		}
	}
	s.groups.mu.Lock()
	if _, exists := s.groups.entries[groupID]; exists {
		s.groups.mu.Unlock()
		writeError(writer, domain.ErrVersionConflict)
		return
	}
	s.groups.entries[groupID] = &groupEntry{snapshot: snapshot, seal: &evaluation.SealState{}}
	s.groups.mu.Unlock()
	writeJSON(writer, http.StatusCreated, snapshot)
}

func (s *Server) review(writer http.ResponseWriter, request *http.Request, groupID string) {
	var input reviewRequest
	if err := decodeJSON(request, &input); err != nil || input.ReviewedAt.IsZero() {
		writeError(writer, validationError())
		return
	}
	s.groups.mu.Lock()
	entry := s.groups.entries[groupID]
	var original evaluation.Snapshot
	if entry != nil {
		original = entry.snapshot
	}
	s.groups.mu.Unlock()
	if entry == nil && s.evaluations != nil {
		state, loadErr := s.evaluations.Evaluation(request.Context(), groupID)
		if loadErr != nil {
			writeError(writer, loadErr)
			return
		}
		if state.Snapshot.ID != "" && state.Group.SealedAt == nil {
			entry = &groupEntry{snapshot: state.Snapshot, seal: &evaluation.SealState{}}
			original = state.Snapshot
		}
	}
	if entry == nil {
		http.NotFound(writer, request)
		return
	}
	snapshot, err := entry.seal.Review(original, input.Dispute, input.Results, input.EvidenceRefs, input.ReviewedAt)
	if err != nil {
		writeError(writer, err)
		return
	}
	if s.evaluations != nil {
		if err := s.evaluations.SaveReviewAndSeal(request.Context(), snapshot, input.ReviewedAt); err != nil {
			writeError(writer, err)
			return
		}
	}
	s.groups.mu.Lock()
	entry.snapshot = snapshot
	s.groups.mu.Unlock()
	writeJSON(writer, http.StatusOK, snapshot)
}

func (s *Server) seal(writer http.ResponseWriter, request *http.Request, groupID string) {
	var input struct {
		SealedAt time.Time `json:"sealed_at"`
	}
	if err := decodeJSON(request, &input); err != nil || input.SealedAt.IsZero() {
		writeError(writer, validationError())
		return
	}
	s.groups.mu.Lock()
	entry := s.groups.entries[groupID]
	var snapshot evaluation.Snapshot
	if entry != nil {
		snapshot = entry.snapshot
	}
	s.groups.mu.Unlock()
	if entry == nil && s.evaluations != nil {
		state, loadErr := s.evaluations.Evaluation(request.Context(), groupID)
		if loadErr != nil {
			writeError(writer, loadErr)
			return
		}
		if state.Snapshot.ID != "" && state.Group.SealedAt == nil {
			entry = &groupEntry{snapshot: state.Snapshot, seal: &evaluation.SealState{}}
			snapshot = state.Snapshot
		}
	}
	if entry == nil {
		http.NotFound(writer, request)
		return
	}
	if s.evaluations != nil {
		if err := s.evaluations.SealEvaluation(request.Context(), groupID, snapshot, input.SealedAt); err != nil {
			writeError(writer, err)
			return
		}
	}
	if err := entry.seal.Seal(snapshot, input.SealedAt); err != nil {
		writeError(writer, err)
		return
	}
	s.digest(writer, request, groupID)
}

func (s *Server) digest(writer http.ResponseWriter, request *http.Request, groupID string) {
	if s.evaluations != nil {
		state, err := s.evaluations.Evaluation(request.Context(), groupID)
		if err != nil {
			writeError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{
			"canonical_digest": state.Snapshot.CanonicalDigest,
			"global_position":  s.readyPosition(request),
			"sealed":           state.Group.SealedAt != nil, "sealed_conclusion": state.Group.SealedConclusion,
			"sealed_at": state.Group.SealedAt, "sealed_digest": state.Group.SealedDigest,
		})
		return
	}
	s.groups.mu.Lock()
	entry := s.groups.entries[groupID]
	var snapshot evaluation.Snapshot
	if entry != nil {
		snapshot = entry.snapshot
	}
	s.groups.mu.Unlock()
	if entry == nil {
		http.NotFound(writer, request)
		return
	}
	conclusion, sealedAt, digest, sealed := entry.seal.Sealed()
	writeJSON(writer, http.StatusOK, map[string]any{
		"canonical_digest": snapshot.CanonicalDigest, "sealed": sealed,
		"sealed_conclusion": conclusion, "sealed_at": sealedAt, "sealed_digest": digest,
	})
}

func (s *Server) group(writer http.ResponseWriter, request *http.Request, groupID string) {
	if s.evaluations != nil {
		state, err := s.evaluations.Evaluation(request.Context(), groupID)
		if err != nil {
			writeError(writer, err)
			return
		}
		_, specimens, err := s.catalog.SampleGroup(request.Context(), groupID)
		if err != nil {
			writeError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{
			"group": state.Group, "specimens": specimens, "snapshot": state.Snapshot,
		})
		return
	}
	s.groups.mu.Lock()
	entry := s.groups.entries[groupID]
	var snapshot evaluation.Snapshot
	if entry != nil {
		snapshot = entry.snapshot
	}
	s.groups.mu.Unlock()
	if entry == nil {
		http.NotFound(writer, request)
		return
	}
	conclusion, sealedAt, digest, sealed := entry.seal.Sealed()
	writeJSON(writer, http.StatusOK, map[string]any{
		"snapshot": snapshot, "sealed": sealed, "sealed_conclusion": conclusion,
		"sealed_at": sealedAt, "sealed_digest": digest,
	})
}

func (s *Server) readyPosition(request *http.Request) uint64 {
	status, err := s.repository.Ready(request.Context())
	if err != nil {
		return 0
	}
	return status.LastGlobalPosition
}
