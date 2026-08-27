package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/httpapi"
	"concrete-specimen-chain-service/internal/ingest"
	"concrete-specimen-chain-service/internal/storage"
)

func TestEvaluationAndSealScript(t *testing.T) {
	repository := storage.NewMemoryRepository(nil)
	server := httpapi.New(ingest.NewService(repository), repository)
	catalog := domain.DefaultInspectionRule("p1", 1)
	catalog.RequiredSpecimens = 1
	rule, err := catalog.Freeze(30, 150)
	if err != nil {
		t.Fatal(err)
	}
	evaluateBody, _ := json.Marshal(map[string]any{
		"group_version": 4, "rule": rule, "created_at": "2026-03-01T00:00:00Z",
		"results": []map[string]any{{"specimen_id": "s1", "strength_mpa": 40, "validity": "VALID"}},
	})
	evaluated := httptest.NewRecorder()
	server.ServeHTTP(evaluated, httptest.NewRequest(http.MethodPost, "/v1/sample-groups/g1/evaluate", bytes.NewReader(evaluateBody)))
	if evaluated.Code != http.StatusCreated {
		t.Fatalf("evaluate status=%d body=%s", evaluated.Code, evaluated.Body.String())
	}
	sealed := httptest.NewRecorder()
	server.ServeHTTP(sealed, httptest.NewRequest(http.MethodPost, "/v1/sample-groups/g1/seal", bytes.NewBufferString(`{"sealed_at":"2026-03-01T01:00:00Z"}`)))
	if sealed.Code != http.StatusOK {
		t.Fatalf("seal status=%d body=%s", sealed.Code, sealed.Body.String())
	}
	var digest struct {
		Sealed           bool   `json:"sealed"`
		SealedConclusion string `json:"sealed_conclusion"`
		SealedDigest     string `json:"sealed_digest"`
	}
	if err := json.Unmarshal(sealed.Body.Bytes(), &digest); err != nil {
		t.Fatal(err)
	}
	if !digest.Sealed || digest.SealedConclusion != "PASSED" || digest.SealedDigest == "" {
		t.Fatalf("digest response=%+v", digest)
	}
}
