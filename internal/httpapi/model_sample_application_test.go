package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"concrete-specimen-chain-service/internal/httpapi"
	"concrete-specimen-chain-service/internal/ingest"
	"concrete-specimen-chain-service/internal/storage"
)

func TestModel_AppliedSampleEventUpdatesDomainState(t *testing.T) {
	testCases := []struct {
		name     string
		identity string
	}{
		{name: "field tag", identity: "tag-field-001"},
		{name: "sealed label", identity: "seal-lab-002"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			at := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
			repository, err := storage.OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "specimens.db"), func() time.Time {
				return at.Add(time.Minute)
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = repository.Close() })
			server := httpapi.New(ingest.NewService(repository), repository)

			post := func(path string, payload any, wantStatus int) []byte {
				t.Helper()
				body, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				response := httptest.NewRecorder()
				server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)))
				if response.Code != wantStatus {
					t.Fatalf("POST %s status=%d, want=%d body=%s", path, response.Code, wantStatus, response.Body.String())
				}
				return response.Body.Bytes()
			}

			post("/v1/projects", map[string]any{
				"id": "project-1", "name": "Metro Project", "site_code": "SITE-1", "created_at": at,
			}, http.StatusCreated)
			post("/v1/pour-sections", map[string]any{
				"id": "pour-1", "project_id": "project-1", "name": "Zone A", "location": "A-01", "planned_pour_at": at,
			}, http.StatusCreated)
			post("/v1/mix-designs", map[string]any{
				"id": "mix-1", "project_id": "project-1", "code": "C30", "design_strength_mpa": 30,
				"material_revision": "rev-1",
			}, http.StatusCreated)
			post("/v1/inspection-rules", map[string]any{
				"id": "rule-1", "project_id": "project-1", "revision": 1, "target_equivalent_days": 28,
				"required_specimens": 1, "allowed_temperature_min_c": 5, "allowed_temperature_max_c": 35,
				"missing_limit_minutes": 360, "out_of_range_limit_minutes": 120,
				"dimension_factors": map[string]float64{"150": 1}, "mean_factor": 1.15, "minimum_factor": 0.95,
				"created_at": at,
			}, http.StatusCreated)
			post("/v1/sample-groups", map[string]any{
				"id": "group-1", "project_id": "project-1", "pour_section_id": "pour-1", "mix_design_id": "mix-1",
				"rule_revision": 1, "nominal_side_mm": 150, "specimen_ids": []string{"specimen-1"},
				"specimen_numbers": []string{"S-001"},
			}, http.StatusCreated)

			post("/v1/specimens/specimen-1/events", map[string]any{
				"source": "sampler-1", "specimen_id": "specimen-1", "sequence": 1, "occurred_at": at,
				"expected_version": 0, "type": "SAMPLED", "payload": map[string]any{"identity": testCase.identity},
			}, http.StatusAccepted)
			advanceBody := post("/v1/sample-groups/group-1/watermark:advance", map[string]any{
				"specimen_ids": []string{"specimen-1"}, "until": at,
			}, http.StatusOK)
			var advanced map[string][]string
			if err := json.Unmarshal(advanceBody, &advanced); err != nil {
				t.Fatal(err)
			}
			if got := advanced["specimen-1"]; len(got) != 1 || got[0] != "sampler-1/specimen-1/1" {
				t.Fatalf("advanced events=%v", got)
			}

			chainResponse := httptest.NewRecorder()
			server.ServeHTTP(chainResponse, httptest.NewRequest(http.MethodGet, "/v1/specimens/specimen-1/chain", nil))
			if chainResponse.Code != http.StatusOK {
				t.Fatalf("GET chain status=%d body=%s", chainResponse.Code, chainResponse.Body.String())
			}
			var chain struct {
				Events []struct {
					Type             string          `json:"type"`
					AppliedStatus    string          `json:"applied_status"`
					CanonicalPayload json.RawMessage `json:"canonical_payload"`
				} `json:"events"`
			}
			if err := json.Unmarshal(chainResponse.Body.Bytes(), &chain); err != nil {
				t.Fatal(err)
			}
			if len(chain.Events) != 1 || chain.Events[0].Type != "SAMPLED" || chain.Events[0].AppliedStatus != "applied" {
				t.Fatalf("chain events=%+v", chain.Events)
			}
			var appliedPayload struct {
				Identity string `json:"identity"`
			}
			if err := json.Unmarshal(chain.Events[0].CanonicalPayload, &appliedPayload); err != nil {
				t.Fatal(err)
			}
			if appliedPayload.Identity != testCase.identity {
				t.Fatalf("applied identity=%q, want=%q", appliedPayload.Identity, testCase.identity)
			}

			groupResponse := httptest.NewRecorder()
			server.ServeHTTP(groupResponse, httptest.NewRequest(http.MethodGet, "/v1/sample-groups/group-1", nil))
			if groupResponse.Code != http.StatusOK {
				t.Fatalf("GET group status=%d body=%s", groupResponse.Code, groupResponse.Body.String())
			}
			var state struct {
				Group struct {
					Status    string     `json:"status"`
					SampledAt *time.Time `json:"sampled_at"`
				} `json:"group"`
				Specimens []struct {
					ID            string    `json:"id"`
					BoundIdentity string    `json:"bound_identity"`
					Version       uint64    `json:"version"`
					LastAppliedAt time.Time `json:"last_applied_at"`
				} `json:"specimens"`
			}
			if err := json.Unmarshal(groupResponse.Body.Bytes(), &state); err != nil {
				t.Fatal(err)
			}
			if state.Group.Status != "CURING" || state.Group.SampledAt == nil || !state.Group.SampledAt.Equal(at) {
				t.Fatalf("group status=%q sampled_at=%v, want CURING at %s", state.Group.Status, state.Group.SampledAt, at)
			}
			if len(state.Specimens) != 1 || state.Specimens[0].ID != "specimen-1" ||
				state.Specimens[0].BoundIdentity != testCase.identity || state.Specimens[0].Version != 1 ||
				!state.Specimens[0].LastAppliedAt.Equal(at) {
				t.Fatalf("specimens=%+v", state.Specimens)
			}
		})
	}
}
