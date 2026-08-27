package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/evaluation"
	"concrete-specimen-chain-service/internal/httpapi"
	"concrete-specimen-chain-service/internal/ingest"
	"concrete-specimen-chain-service/internal/storage"
)

type retryableEvaluationRepository struct {
	*storage.MemoryRepository

	mu                    sync.Mutex
	state                 storage.EvaluationState
	reviewFailure         error
	reviewFailuresPending int
}

func (r *retryableEvaluationRepository) SaveInitialEvaluation(_ context.Context, snapshot evaluation.Snapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state = storage.EvaluationState{
		Snapshot: snapshot,
		Group: domain.SampleGroup{
			ID: snapshot.GroupID, Rule: snapshot.Rule, Status: domain.GroupAwaitingReview,
			Version: snapshot.GroupVersion + 1, FrozenSnapshotID: snapshot.ID,
		},
	}
	return nil
}

func (r *retryableEvaluationRepository) SaveReviewAndSeal(ctx context.Context, snapshot evaluation.Snapshot, sealedAt time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reviewFailuresPending > 0 {
		r.reviewFailuresPending--
		return r.reviewFailure
	}
	status := domain.GroupInvalid
	if snapshot.CalculatedConclusion == domain.ConclusionPassed {
		status = domain.GroupPassed
	} else if snapshot.CalculatedConclusion == domain.ConclusionFailed {
		status = domain.GroupFailed
	}
	sealedAt = sealedAt.UTC()
	r.state.Snapshot = snapshot
	r.state.Group.Status = status
	r.state.Group.Version++
	r.state.Group.FrozenSnapshotID = snapshot.ID
	r.state.Group.ReviewCount = 1
	r.state.Group.SealedConclusion = snapshot.CalculatedConclusion
	r.state.Group.SealedAt = &sealedAt
	r.state.Group.SealedDigest = snapshot.CanonicalDigest
	return nil
}

func (r *retryableEvaluationRepository) SealEvaluation(_ context.Context, _ string, snapshot evaluation.Snapshot, sealedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	sealedAt = sealedAt.UTC()
	r.state.Snapshot = snapshot
	r.state.Group.SealedConclusion = snapshot.CalculatedConclusion
	r.state.Group.SealedAt = &sealedAt
	r.state.Group.SealedDigest = snapshot.CanonicalDigest
	return nil
}

func (r *retryableEvaluationRepository) Evaluation(ctx context.Context, _ string) (storage.EvaluationState, error) {
	if err := ctx.Err(); err != nil {
		return storage.EvaluationState{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state, nil
}

func TestModel_ReviewFailureKeepsIdenticalRetryLegal(t *testing.T) {
	tests := []struct {
		name               string
		cancelFirstRequest bool
		storageFailure     error
	}{
		{name: "transient storage failure", storageFailure: errors.New("temporary database write failure")},
		{name: "canceled persistence context", cancelFirstRequest: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &retryableEvaluationRepository{
				MemoryRepository:      storage.NewMemoryRepository(nil),
				reviewFailure:         test.storageFailure,
				reviewFailuresPending: 1,
			}
			if test.cancelFirstRequest {
				repository.reviewFailuresPending = 0
			}
			server := httpapi.New(ingest.NewService(repository), repository)

			ruleCatalog := domain.DefaultInspectionRule("project-1", 1)
			ruleCatalog.RequiredSpecimens = 1
			rule, err := ruleCatalog.Freeze(30, 150)
			if err != nil {
				t.Fatal(err)
			}
			evaluateBody, err := json.Marshal(map[string]any{
				"group_version": 4,
				"rule":          rule,
				"created_at":    "2026-03-01T00:00:00Z",
				"results": []map[string]any{{
					"specimen_id": "specimen-1", "strength_mpa": 20, "validity": "VALID",
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			evaluated := httptest.NewRecorder()
			server.ServeHTTP(evaluated, httptest.NewRequest(http.MethodPost,
				"/v1/sample-groups/group-1/evaluate", bytes.NewReader(evaluateBody)))
			if evaluated.Code != http.StatusCreated {
				t.Fatalf("evaluate status=%d body=%s", evaluated.Code, evaluated.Body.String())
			}
			var original evaluation.Snapshot
			if err := json.Unmarshal(evaluated.Body.Bytes(), &original); err != nil {
				t.Fatal(err)
			}

			reviewBody, err := json.Marshal(map[string]any{
				"dispute":       evaluation.DisputeMachineCalibration,
				"evidence_refs": []string{"calibration-event-1"},
				"reviewed_at":   "2026-03-01T01:00:00Z",
				"results": []map[string]any{{
					"specimen_id": "specimen-1", "strength_mpa": 40, "validity": "VALID",
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			firstRequest := httptest.NewRequest(http.MethodPost,
				"/v1/sample-groups/group-1/review", bytes.NewReader(reviewBody))
			if test.cancelFirstRequest {
				ctx, cancel := context.WithCancel(firstRequest.Context())
				cancel()
				firstRequest = firstRequest.WithContext(ctx)
			}
			first := httptest.NewRecorder()
			server.ServeHTTP(first, firstRequest)
			if first.Code != http.StatusInternalServerError {
				t.Fatalf("first review status=%d body=%s", first.Code, first.Body.String())
			}
			var firstError domain.Error
			if err := json.Unmarshal(first.Body.Bytes(), &firstError); err != nil {
				t.Fatal(err)
			}
			if firstError.Code != "INTERNAL" {
				t.Fatalf("first review error=%+v", firstError)
			}

			pending, err := repository.Evaluation(context.Background(), "group-1")
			if err != nil {
				t.Fatal(err)
			}
			if pending.Group.Status != domain.GroupAwaitingReview || pending.Group.ReviewCount != 0 ||
				pending.Group.SealedAt != nil || pending.Snapshot.ID != original.ID ||
				pending.Snapshot.CanonicalDigest != original.CanonicalDigest {
				t.Fatalf("failed review changed persisted state: %+v", pending)
			}
			digest := httptest.NewRecorder()
			server.ServeHTTP(digest, httptest.NewRequest(http.MethodGet,
				"/v1/sample-groups/group-1/digest", nil))
			var afterFailure struct {
				CanonicalDigest string `json:"canonical_digest"`
				Sealed          bool   `json:"sealed"`
			}
			if digest.Code != http.StatusOK || json.Unmarshal(digest.Body.Bytes(), &afterFailure) != nil ||
				afterFailure.Sealed || afterFailure.CanonicalDigest != original.CanonicalDigest {
				t.Fatalf("digest after failed review status=%d body=%s", digest.Code, digest.Body.String())
			}

			retried := httptest.NewRecorder()
			server.ServeHTTP(retried, httptest.NewRequest(http.MethodPost,
				"/v1/sample-groups/group-1/review", bytes.NewReader(reviewBody)))
			if retried.Code != http.StatusOK {
				t.Fatalf("identical retry status=%d body=%s", retried.Code, retried.Body.String())
			}
			var reviewed evaluation.Snapshot
			if err := json.Unmarshal(retried.Body.Bytes(), &reviewed); err != nil {
				t.Fatal(err)
			}
			if reviewed.Kind != evaluation.SnapshotReview || reviewed.ParentSnapshotID != original.ID ||
				reviewed.CalculatedConclusion != domain.ConclusionPassed || reviewed.ID == original.ID {
				t.Fatalf("reviewed snapshot=%+v original=%+v", reviewed, original)
			}
			final, err := repository.Evaluation(context.Background(), "group-1")
			if err != nil {
				t.Fatal(err)
			}
			if final.Group.Status != domain.GroupPassed || final.Group.ReviewCount != 1 ||
				final.Group.SealedAt == nil || final.Group.SealedDigest != reviewed.CanonicalDigest ||
				final.Snapshot.ParentSnapshotID != original.ID {
				t.Fatalf("successful retry was not atomically reviewed and sealed: %+v", final)
			}

			duplicate := httptest.NewRecorder()
			server.ServeHTTP(duplicate, httptest.NewRequest(http.MethodPost,
				"/v1/sample-groups/group-1/review", bytes.NewReader(reviewBody)))
			var duplicateError domain.Error
			if duplicate.Code != http.StatusUnprocessableEntity ||
				json.Unmarshal(duplicate.Body.Bytes(), &duplicateError) != nil ||
				duplicateError.Code != domain.ErrIllegalTransition.Code {
				t.Fatalf("second successful review status=%d body=%s", duplicate.Code, duplicate.Body.String())
			}
		})
	}
}
