package httpapi

import (
	"net/http"
	"strings"
	"time"

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/storage"
)

func (s *Server) requireCatalog(writer http.ResponseWriter) bool {
	if s.catalog == nil {
		writeError(writer, domain.NewError("PERSISTENCE_UNAVAILABLE", "AVAILABILITY", true))
		return false
	}
	return true
}

func (s *Server) createProject(writer http.ResponseWriter, request *http.Request) {
	if !s.requireCatalog(writer) {
		return
	}
	var project domain.Project
	if decodeJSON(request, &project) != nil || project.ID == "" || project.Name == "" ||
		project.SiteCode == "" || project.CreatedAt.IsZero() {
		writeError(writer, validationError())
		return
	}
	project.CreatedAt = project.CreatedAt.UTC()
	if err := s.catalog.CreateProject(request.Context(), project); err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, project)
}

func (s *Server) createPourSection(writer http.ResponseWriter, request *http.Request) {
	if !s.requireCatalog(writer) {
		return
	}
	var section domain.PourSection
	if decodeJSON(request, &section) != nil || section.ID == "" || section.ProjectID == "" ||
		section.Name == "" || section.Location == "" || section.PlannedPourAt.IsZero() {
		writeError(writer, validationError())
		return
	}
	section.PlannedPourAt = section.PlannedPourAt.UTC()
	if err := s.catalog.CreatePourSection(request.Context(), section); err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, section)
}

func (s *Server) createMixDesign(writer http.ResponseWriter, request *http.Request) {
	if !s.requireCatalog(writer) {
		return
	}
	var design domain.MixDesign
	if decodeJSON(request, &design) != nil || design.ID == "" || design.ProjectID == "" ||
		design.Code == "" || design.MaterialRevision == "" || design.DesignStrengthMPa <= 0 {
		writeError(writer, validationError())
		return
	}
	if err := s.catalog.CreateMixDesign(request.Context(), design); err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, design)
}

func (s *Server) createInspectionRule(writer http.ResponseWriter, request *http.Request) {
	if !s.requireCatalog(writer) {
		return
	}
	var rule domain.InspectionRule
	if decodeJSON(request, &rule) != nil || rule.ID == "" || rule.ProjectID == "" ||
		rule.Revision <= 0 || rule.CreatedAt.IsZero() {
		writeError(writer, validationError())
		return
	}
	if rule.TargetEquivalentDays == 0 {
		defaults := domain.DefaultInspectionRule(rule.ProjectID, rule.Revision)
		defaults.ID, defaults.CreatedAt = rule.ID, rule.CreatedAt
		rule = defaults
	}
	if err := s.catalog.CreateInspectionRule(request.Context(), rule); err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, rule)
}

type createGroupRequest struct {
	ID              string   `json:"id"`
	ProjectID       string   `json:"project_id"`
	PourSectionID   string   `json:"pour_section_id"`
	MixDesignID     string   `json:"mix_design_id"`
	RuleRevision    int      `json:"rule_revision"`
	NominalSideMM   int      `json:"nominal_side_mm"`
	SpecimenIDs     []string `json:"specimen_ids"`
	SpecimenNumbers []string `json:"specimen_numbers"`
}

func (s *Server) createSampleGroup(writer http.ResponseWriter, request *http.Request) {
	if !s.requireCatalog(writer) {
		return
	}
	var input createGroupRequest
	if decodeJSON(request, &input) != nil || input.ID == "" || input.ProjectID == "" ||
		input.PourSectionID == "" || input.MixDesignID == "" || input.RuleRevision <= 0 ||
		input.NominalSideMM <= 0 || len(input.SpecimenIDs) == 0 {
		writeError(writer, validationError())
		return
	}
	if len(input.SpecimenNumbers) == 0 {
		input.SpecimenNumbers = append([]string(nil), input.SpecimenIDs...)
	}
	section, err := s.catalog.PourSection(request.Context(), input.PourSectionID)
	if err != nil {
		writeError(writer, err)
		return
	}
	design, err := s.catalog.MixDesign(request.Context(), input.MixDesignID)
	if err != nil {
		writeError(writer, err)
		return
	}
	if section.ProjectID != input.ProjectID || design.ProjectID != input.ProjectID {
		writeError(writer, domain.NewError("CROSS_PROJECT_REFERENCE", "IDENTITY", false))
		return
	}
	rule, err := s.catalog.InspectionRule(request.Context(), input.ProjectID, input.RuleRevision)
	if err != nil {
		writeError(writer, err)
		return
	}
	frozen, err := rule.Freeze(design.DesignStrengthMPa, input.NominalSideMM)
	if err != nil {
		writeError(writer, err)
		return
	}
	group := domain.SampleGroup{ID: input.ID, PourSectionID: input.PourSectionID,
		MixDesignID: input.MixDesignID, Rule: frozen, Status: domain.GroupAwaitingSampling}
	creation := storage.GroupCreation{Group: group, ProjectID: input.ProjectID,
		SpecimenIDs: input.SpecimenIDs, SpecimenNos: input.SpecimenNumbers, NominalSideMM: input.NominalSideMM}
	if err := s.catalog.CreateSampleGroup(request.Context(), creation); err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{
		"group": group, "specimen_ids": input.SpecimenIDs, "frozen_rule_digest": digestJSON(frozen),
	})
}

func (s *Server) specimenChain(writer http.ResponseWriter, request *http.Request) {
	if !s.requireCatalog(writer) {
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/v1/specimens/")
	specimenID, suffix, found := strings.Cut(path, "/")
	if !found || suffix != "chain" || specimenID == "" {
		http.NotFound(writer, request)
		return
	}
	records, err := s.catalog.Chain(request.Context(), specimenID)
	if err != nil {
		writeError(writer, err)
		return
	}
	pressure, err := s.catalog.PressureResult(request.Context(), specimenID)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"specimen_id": specimenID, "events": records, "event_count": len(records),
		"chain_digest": digestJSON(records), "pressure_result": pressure, "generated_at": time.Time{},
	})
}

func digestJSON(value any) string {
	digest, err := storage.Digest(value)
	if err != nil {
		return ""
	}
	return digest
}
