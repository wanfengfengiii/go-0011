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

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/evaluation"
	"concrete-specimen-chain-service/internal/httpapi"
	"concrete-specimen-chain-service/internal/ingest"
	"concrete-specimen-chain-service/internal/storage"
)

func TestModel_EvaluateUsesPersistedGroupState(t *testing.T) {
	tests := []struct {
		name         string
		groupVersion uint64
		requestRule  func(domain.FrozenRule) domain.FrozenRule
		results      []evaluation.SpecimenResult
	}{
		{
			name: "client cannot weaken the frozen C30 rule",
			requestRule: func(rule domain.FrozenRule) domain.FrozenRule {
				rule.DesignStrengthMPa = 1
				rule.RequiredSpecimens = 1
				return rule
			},
			results: []evaluation.SpecimenResult{{
				SpecimenID: "forged", StrengthMPa: 100, Validity: domain.ValidityValid,
			}},
		},
		{
			name: "client results cannot substitute for persisted pressure results",
			requestRule: func(rule domain.FrozenRule) domain.FrozenRule {
				return rule
			},
			results: []evaluation.SpecimenResult{
				{SpecimenID: "s1", StrengthMPa: 40, Validity: domain.ValidityValid},
				{SpecimenID: "s2", StrengthMPa: 40, Validity: domain.ValidityValid},
				{SpecimenID: "s3", StrengthMPa: 40, Validity: domain.ValidityValid},
			},
		},
		{
			name: "results for specimens outside the group are rejected",
			requestRule: func(rule domain.FrozenRule) domain.FrozenRule {
				return rule
			},
			results: []evaluation.SpecimenResult{
				{SpecimenID: "other-1", StrengthMPa: 40, Validity: domain.ValidityValid},
				{SpecimenID: "other-2", StrengthMPa: 40, Validity: domain.ValidityValid},
				{SpecimenID: "other-3", StrengthMPa: 40, Validity: domain.ValidityValid},
			},
		},
		{
			name:         "stale group version is rejected",
			groupVersion: 1,
			requestRule: func(rule domain.FrozenRule) domain.FrozenRule {
				return rule
			},
			results: []evaluation.SpecimenResult{
				{SpecimenID: "s1", StrengthMPa: 40, Validity: domain.ValidityValid},
				{SpecimenID: "s2", StrengthMPa: 40, Validity: domain.ValidityValid},
				{SpecimenID: "s3", StrengthMPa: 40, Validity: domain.ValidityValid},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
			repository, err := storage.OpenSQLite(ctx, filepath.Join(t.TempDir(), "service.db"), func() time.Time {
				return at
			})
			if err != nil {
				t.Fatal(err)
			}
			defer repository.Close()

			project := domain.Project{ID: "p1", Name: "project", SiteCode: "SITE", CreatedAt: at}
			if err := repository.CreateProject(ctx, project); err != nil {
				t.Fatal(err)
			}
			section := domain.PourSection{
				ID: "pour1", ProjectID: project.ID, Name: "zone", Location: "A1", PlannedPourAt: at,
			}
			if err := repository.CreatePourSection(ctx, section); err != nil {
				t.Fatal(err)
			}
			design := domain.MixDesign{
				ID: "mix1", ProjectID: project.ID, Code: "C30", MaterialRevision: "r1", DesignStrengthMPa: 30,
			}
			if err := repository.CreateMixDesign(ctx, design); err != nil {
				t.Fatal(err)
			}
			catalogRule := domain.DefaultInspectionRule(project.ID, 1)
			catalogRule.ID, catalogRule.CreatedAt = "rule1", at
			if err := repository.CreateInspectionRule(ctx, catalogRule); err != nil {
				t.Fatal(err)
			}
			frozenRule, err := catalogRule.Freeze(design.DesignStrengthMPa, 150)
			if err != nil {
				t.Fatal(err)
			}
			group := domain.SampleGroup{
				ID: "g1", PourSectionID: section.ID, MixDesignID: design.ID, Rule: frozenRule,
			}
			if err := repository.CreateSampleGroup(ctx, storage.GroupCreation{
				Group: group, ProjectID: project.ID,
				SpecimenIDs: []string{"s1", "s2", "s3"},
				SpecimenNos: []string{"S-001", "S-002", "S-003"}, NominalSideMM: 150,
			}); err != nil {
				t.Fatal(err)
			}

			server := httpapi.New(ingest.NewService(repository), repository)
			body, err := json.Marshal(map[string]any{
				"group_version": test.groupVersion,
				"rule":          test.requestRule(frozenRule),
				"results":       test.results,
				"created_at":    at.Add(time.Hour),
			})
			if err != nil {
				t.Fatal(err)
			}
			evaluated := httptest.NewRecorder()
			server.ServeHTTP(evaluated, httptest.NewRequest(
				http.MethodPost, "/v1/sample-groups/g1/evaluate", bytes.NewReader(body),
			))
			if evaluated.Code < http.StatusBadRequest || evaluated.Code >= http.StatusInternalServerError {
				t.Errorf("evaluate status=%d body=%s; unfinished persisted group must be rejected", evaluated.Code, evaluated.Body.String())
			}

			sealed := httptest.NewRecorder()
			server.ServeHTTP(sealed, httptest.NewRequest(
				http.MethodPost, "/v1/sample-groups/g1/seal",
				bytes.NewBufferString(`{"sealed_at":"2026-03-01T02:00:00Z"}`),
			))
			if sealed.Code < http.StatusBadRequest || sealed.Code >= http.StatusInternalServerError {
				t.Errorf("seal status=%d body=%s; rejected evaluation must not be sealable", sealed.Code, sealed.Body.String())
			}

			state, err := repository.Evaluation(ctx, group.ID)
			if err != nil {
				t.Fatal(err)
			}
			if state.Group.Status != domain.GroupAwaitingSampling || state.Group.Version != 0 ||
				state.Group.FrozenSnapshotID != "" || state.Group.SealedAt != nil || state.Snapshot.ID != "" {
				t.Errorf("rejected request changed persisted state: %+v", state)
			}
		})
	}
}
