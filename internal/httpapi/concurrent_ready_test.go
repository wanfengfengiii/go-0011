package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/httpapi"
	"concrete-specimen-chain-service/internal/ingest"
	"concrete-specimen-chain-service/internal/storage"
)

func TestModel_ConcurrentReadyTracksCommittedPosition(t *testing.T) {
	cases := []struct {
		name    string
		through string
	}{
		{name: "repository Ready", through: "repository"},
		{name: "GET health ready", through: "http"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repository, err := storage.OpenSQLite(ctx, filepath.Join(t.TempDir(), "events.db"), func() time.Time {
				return time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
			})
			if err != nil {
				t.Fatal(err)
			}
			defer repository.Close()
			server := httpapi.New(ingest.NewService(repository), repository)

			const submissions = 96
			start := make(chan struct{})
			stop := make(chan struct{})
			pollResult := make(chan error, 1)
			go func() {
				<-start
				var previous uint64
				for {
					var status ingest.RecoveryStatus
					if tc.through == "http" {
						response := httptest.NewRecorder()
						server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
						if response.Code != http.StatusOK {
							pollResult <- fmt.Errorf("readiness HTTP status=%d body=%s", response.Code, response.Body.String())
							return
						}
						if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
							pollResult <- fmt.Errorf("decode readiness: %w", err)
							return
						}
					} else {
						var err error
						status, err = repository.Ready(ctx)
						if err != nil {
							pollResult <- fmt.Errorf("readiness: %w", err)
							return
						}
					}
					if !status.Ready || status.Phase != "ready" {
						pollResult <- fmt.Errorf("readiness semantics changed: %+v", status)
						return
					}
					if status.LastGlobalPosition < previous {
						pollResult <- fmt.Errorf("last_global_position regressed from %d to %d", previous, status.LastGlobalPosition)
						return
					}
					previous = status.LastGlobalPosition
					select {
					case <-stop:
						pollResult <- nil
						return
					default:
						runtime.Gosched()
					}
				}
			}()

			results := make(chan error, submissions)
			var writers sync.WaitGroup
			writers.Add(submissions)
			for i := 0; i < submissions; i++ {
				go func(index int) {
					defer writers.Done()
					<-start
					specimenID := fmt.Sprintf("s-%03d", index)
					envelope := ingest.Envelope{
						Source: "device", SpecimenID: specimenID, Sequence: 1,
						OccurredAt:      time.Date(2026, 8, 27, 1, 0, index, 0, time.UTC),
						ExpectedVersion: 0, Type: domain.EventSampled,
						Payload: json.RawMessage(fmt.Sprintf(`{"identity":"tag-%03d"}`, index)),
					}
					body, err := json.Marshal(envelope)
					if err != nil {
						results <- err
						return
					}
					response := httptest.NewRecorder()
					request := httptest.NewRequest(http.MethodPost, "/v1/specimens/"+specimenID+"/events", bytes.NewReader(body))
					server.ServeHTTP(response, request)
					if response.Code != http.StatusAccepted {
						results <- fmt.Errorf("specimen %s status=%d body=%s", specimenID, response.Code, response.Body.String())
						return
					}
					results <- nil
				}(i)
			}

			close(start)
			writers.Wait()
			close(stop)
			for i := 0; i < submissions; i++ {
				if err := <-results; err != nil {
					t.Error(err)
				}
			}
			if err := <-pollResult; err != nil {
				t.Error(err)
			}

			status, err := repository.Ready(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !status.Ready || status.Phase != "ready" || status.LastGlobalPosition != submissions {
				t.Fatalf("final readiness=%+v, want ready phase at position %d", status, submissions)
			}
		})
	}
}
