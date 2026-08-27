package httpapi_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"concrete-specimen-chain-service/internal/httpapi"
	"concrete-specimen-chain-service/internal/ingest"
	"concrete-specimen-chain-service/internal/storage"
)

func TestReadyAndEventEndpoints(t *testing.T) {
	repository := storage.NewMemoryRepository(func() time.Time { return time.Unix(1, 0).UTC() })
	server := httpapi.New(ingest.NewService(repository), repository)

	ready := httptest.NewRecorder()
	server.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if ready.Code != http.StatusOK {
		t.Fatalf("ready status = %d body=%s", ready.Code, ready.Body.String())
	}

	body := bytes.NewBufferString(`{
		"source":"device-1","specimen_id":"s1","sequence":1,
		"occurred_at":"2026-01-01T00:00:00Z","expected_version":0,
		"type":"SAMPLED","payload":{"identity":"tag-1"}
	}`)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/specimens/s1/events", body))
	if response.Code != http.StatusAccepted || len(repository.Events()) != 1 {
		t.Fatalf("event status = %d body=%s events=%d", response.Code, response.Body.String(), len(repository.Events()))
	}
}
