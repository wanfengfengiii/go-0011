package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/httpapi"
	"concrete-specimen-chain-service/internal/ingest"
	"concrete-specimen-chain-service/internal/storage"
)

func TestModel_BatchEventsIsolatesMalformedElements(t *testing.T) {
	testCases := []struct {
		name          string
		body          string
		wantSequences []uint64
		wantVersions  []uint64
		wantKinds     []string
	}{
		{
			name: "malformed occurred_at between valid events",
			body: `[
				{"source":"press-1","specimen_id":"s1","sequence":10,"occurred_at":"2026-01-01T00:00:00Z","expected_version":0,"type":"SAMPLED","payload":{"identity":"tag-1"}},
				{"source":"press-1","specimen_id":"s1","sequence":11,"occurred_at":"damaged-old-timestamp","expected_version":1,"type":"TRANSPORTED","payload":{"location":"lab"}},
				{"source":"press-1","specimen_id":"s1","sequence":12,"occurred_at":"2026-01-01T00:02:00Z","expected_version":1,"type":"TRANSPORTED","payload":{"location":"lab"}}
			]`,
			wantSequences: []uint64{10, 12},
			wantVersions:  []uint64{1, 0, 2},
			wantKinds:     []string{"receipt", "error", "receipt"},
		},
		{
			name: "malformed first element does not stop later event",
			body: `[
				{"source":"press-2","specimen_id":"s2","sequence":20,"occurred_at":"not-a-time","expected_version":0,"type":"SAMPLED","payload":{"identity":"old-tag"}},
				{"source":"press-2","specimen_id":"s2","sequence":21,"occurred_at":"2026-01-01T01:00:00Z","expected_version":0,"type":"SAMPLED","payload":{"identity":"new-tag"}}
			]`,
			wantSequences: []uint64{21},
			wantVersions:  []uint64{0, 1},
			wantKinds:     []string{"error", "receipt"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repository := storage.NewMemoryRepository(func() time.Time { return time.Unix(1, 0).UTC() })
			server := httpapi.New(ingest.NewService(repository), repository)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/v1/events:batch", bytes.NewBufferString(testCase.body))

			server.ServeHTTP(response, request)

			if response.Code != http.StatusMultiStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusMultiStatus, response.Body.String())
			}
			var results []struct {
				Receipt *ingest.Receipt `json:"receipt"`
				Error   *domain.Error   `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &results); err != nil {
				t.Fatalf("decode response: %v; body=%s", err, response.Body.String())
			}
			if len(results) != len(testCase.wantKinds) {
				t.Fatalf("result count = %d, want %d; body=%s", len(results), len(testCase.wantKinds), response.Body.String())
			}
			for index, wantKind := range testCase.wantKinds {
				switch wantKind {
				case "receipt":
					if results[index].Receipt == nil || results[index].Error != nil {
						t.Errorf("result[%d] = %+v, want receipt", index, results[index])
						continue
					}
					if results[index].Receipt.Status != "buffered" || results[index].Receipt.Version != testCase.wantVersions[index] {
						t.Errorf("result[%d] receipt = %+v, want buffered version %d", index, results[index].Receipt, testCase.wantVersions[index])
					}
				case "error":
					if results[index].Error == nil || results[index].Receipt != nil {
						t.Errorf("result[%d] = %+v, want validation error", index, results[index])
						continue
					}
					if results[index].Error.Code != "VALIDATION" || results[index].Error.Category != "VALIDATION" || results[index].Error.Retryable {
						t.Errorf("result[%d] error = %+v, want stable non-retryable VALIDATION", index, results[index].Error)
					}
				}
			}

			events := repository.Events()
			if len(events) != len(testCase.wantSequences) {
				t.Fatalf("stored event count = %d, want %d", len(events), len(testCase.wantSequences))
			}
			for index, wantSequence := range testCase.wantSequences {
				if events[index].Sequence != wantSequence {
					t.Errorf("stored event[%d] sequence = %d, want %d", index, events[index].Sequence, wantSequence)
				}
			}
		})
	}
}
