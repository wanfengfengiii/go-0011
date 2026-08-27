package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"concrete-specimen-chain-service/internal/domain"
)

// GroupCreation is the atomic frozen input for a new inspection group.
type GroupCreation struct {
	Group         domain.SampleGroup
	ProjectID     string
	SpecimenIDs   []string
	SpecimenNos   []string
	NominalSideMM int
}

func (r *SQLiteRepository) CreateProject(ctx context.Context, project domain.Project) error {
	if project.ID == "" || project.Name == "" || project.SiteCode == "" || project.CreatedAt.IsZero() {
		return domain.NewError("VALIDATION", "VALIDATION", false)
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO projects(id, name, site_code, created_at) VALUES (?, ?, ?, ?)`,
		project.ID, project.Name, project.SiteCode, project.CreatedAt.UTC().Format(time.RFC3339Nano))
	return catalogError(err)
}

func (r *SQLiteRepository) CreatePourSection(ctx context.Context, section domain.PourSection) error {
	if section.ID == "" || section.ProjectID == "" || section.Name == "" || section.Location == "" || section.PlannedPourAt.IsZero() {
		return domain.NewError("VALIDATION", "VALIDATION", false)
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO pour_sections(id, project_id, name, location, planned_pour_at)
        VALUES (?, ?, ?, ?, ?)`, section.ID, section.ProjectID, section.Name, section.Location,
		section.PlannedPourAt.UTC().Format(time.RFC3339Nano))
	return catalogError(err)
}

func (r *SQLiteRepository) CreateMixDesign(ctx context.Context, design domain.MixDesign) error {
	if design.ID == "" || design.ProjectID == "" || design.Code == "" || design.MaterialRevision == "" || design.DesignStrengthMPa <= 0 {
		return domain.NewError("VALIDATION", "VALIDATION", false)
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO mix_designs
        (id, project_id, code, design_strength_mpa, material_revision) VALUES (?, ?, ?, ?, ?)`,
		design.ID, design.ProjectID, design.Code, design.DesignStrengthMPa, design.MaterialRevision)
	return catalogError(err)
}

func (r *SQLiteRepository) CreateInspectionRule(ctx context.Context, rule domain.InspectionRule) error {
	if err := validateRule(rule); err != nil {
		return err
	}
	canonical, err := json.Marshal(rule)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO inspection_rules
        (id, project_id, revision, rule_json, created_at) VALUES (?, ?, ?, ?, ?)`,
		rule.ID, rule.ProjectID, rule.Revision, canonical, rule.CreatedAt.UTC().Format(time.RFC3339Nano))
	return catalogError(err)
}

func validateRule(rule domain.InspectionRule) error {
	if rule.ID == "" || rule.ProjectID == "" || rule.Revision <= 0 || rule.TargetEquivalentDays <= 0 ||
		rule.RequiredSpecimens <= 0 || rule.AllowedTemperatureMinC >= rule.AllowedTemperatureMaxC ||
		rule.MissingLimitMinutes <= 0 || rule.OutOfRangeLimitMinutes <= 0 || rule.MeanFactor <= 0 ||
		rule.MinimumFactor <= 0 || rule.CreatedAt.IsZero() || len(rule.DimensionFactors) == 0 {
		return domain.NewError("VALIDATION", "VALIDATION", false)
	}
	return nil
}

func (r *SQLiteRepository) InspectionRule(ctx context.Context, projectID string, revision int) (domain.InspectionRule, error) {
	var encoded []byte
	err := r.db.QueryRowContext(ctx, `SELECT rule_json FROM inspection_rules WHERE project_id = ? AND revision = ?`,
		projectID, revision).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.InspectionRule{}, domain.NewError("RULE_NOT_FOUND", "NOT_FOUND", false)
	}
	if err != nil {
		return domain.InspectionRule{}, err
	}
	var rule domain.InspectionRule
	if err := json.Unmarshal(encoded, &rule); err != nil {
		return domain.InspectionRule{}, err
	}
	return rule, nil
}

func (r *SQLiteRepository) MixDesign(ctx context.Context, id string) (domain.MixDesign, error) {
	var design domain.MixDesign
	err := r.db.QueryRowContext(ctx, `SELECT id, project_id, code, design_strength_mpa, material_revision
        FROM mix_designs WHERE id = ?`, id).Scan(&design.ID, &design.ProjectID, &design.Code,
		&design.DesignStrengthMPa, &design.MaterialRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.MixDesign{}, domain.NewError("MIX_DESIGN_NOT_FOUND", "NOT_FOUND", false)
	}
	return design, err
}

func (r *SQLiteRepository) PourSection(ctx context.Context, id string) (domain.PourSection, error) {
	var section domain.PourSection
	var planned string
	err := r.db.QueryRowContext(ctx, `SELECT id, project_id, name, location, planned_pour_at
        FROM pour_sections WHERE id = ?`, id).Scan(&section.ID, &section.ProjectID, &section.Name, &section.Location, &planned)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.PourSection{}, domain.NewError("POUR_SECTION_NOT_FOUND", "NOT_FOUND", false)
	}
	if err == nil {
		section.PlannedPourAt, _ = time.Parse(time.RFC3339Nano, planned)
	}
	return section, err
}

// CreateSampleGroup verifies that section and mix belong to one project and
// inserts the group plus every project-unique specimen in one transaction.
func (r *SQLiteRepository) CreateSampleGroup(ctx context.Context, creation GroupCreation) error {
	group := creation.Group
	if group.ID == "" || group.PourSectionID == "" || group.MixDesignID == "" || creation.ProjectID == "" ||
		creation.NominalSideMM <= 0 || len(creation.SpecimenIDs) == 0 || len(creation.SpecimenIDs) != len(creation.SpecimenNos) ||
		len(creation.SpecimenIDs) < group.Rule.RequiredSpecimens {
		return domain.NewError("VALIDATION", "VALIDATION", false)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var sectionProject, mixProject string
	if err := tx.QueryRowContext(ctx, `SELECT project_id FROM pour_sections WHERE id = ?`, group.PourSectionID).Scan(&sectionProject); err != nil {
		return catalogError(err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT project_id FROM mix_designs WHERE id = ?`, group.MixDesignID).Scan(&mixProject); err != nil {
		return catalogError(err)
	}
	if sectionProject != creation.ProjectID || mixProject != creation.ProjectID {
		return domain.NewError("CROSS_PROJECT_REFERENCE", "IDENTITY", false)
	}
	ruleJSON, err := json.Marshal(group.Rule)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sample_groups
        (id, pour_section_id, mix_design_id, rule_json, status, version) VALUES (?, ?, ?, ?, ?, ?)`,
		group.ID, group.PourSectionID, group.MixDesignID, ruleJSON, domain.GroupAwaitingSampling, group.Version); err != nil {
		return catalogError(err)
	}
	seen := make(map[string]bool, len(creation.SpecimenIDs))
	for index, specimenID := range creation.SpecimenIDs {
		if specimenID == "" || creation.SpecimenNos[index] == "" || seen[specimenID] {
			return domain.NewError("VALIDATION", "VALIDATION", false)
		}
		seen[specimenID] = true
		_, err := tx.ExecContext(ctx, `INSERT INTO specimens
            (id, group_id, specimen_no, project_id, nominal_side_mm, validity)
            VALUES (?, ?, ?, ?, ?, ?)`, specimenID, group.ID, creation.SpecimenNos[index], creation.ProjectID,
			creation.NominalSideMM, domain.ValidityValid)
		if err != nil {
			return catalogError(err)
		}
	}
	return tx.Commit()
}

func (r *SQLiteRepository) SampleGroup(ctx context.Context, id string) (domain.SampleGroup, []domain.Specimen, error) {
	var group domain.SampleGroup
	var ruleJSON []byte
	var status, sealedConclusion, sealedAt string
	err := r.db.QueryRowContext(ctx, `SELECT id, pour_section_id, mix_design_id, rule_json, status,
        version, frozen_snapshot_id, review_count, sealed_conclusion, COALESCE(sealed_at, ''), sealed_digest
        FROM sample_groups WHERE id = ?`, id).Scan(&group.ID, &group.PourSectionID, &group.MixDesignID,
		&ruleJSON, &status, &group.Version, &group.FrozenSnapshotID, &group.ReviewCount,
		&sealedConclusion, &sealedAt, &group.SealedDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SampleGroup{}, nil, domain.NewError("GROUP_NOT_FOUND", "NOT_FOUND", false)
	}
	if err != nil {
		return domain.SampleGroup{}, nil, err
	}
	if err := json.Unmarshal(ruleJSON, &group.Rule); err != nil {
		return domain.SampleGroup{}, nil, err
	}
	group.Status = domain.GroupStatus(status)
	group.SealedConclusion = domain.Conclusion(sealedConclusion)
	if sealedAt != "" {
		parsed, _ := time.Parse(time.RFC3339Nano, sealedAt)
		group.SealedAt = &parsed
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, group_id, specimen_no, bound_identity,
        nominal_side_mm, version, COALESCE(last_applied_at, ''), COALESCE(max_seen_at, ''),
        effective_age_minutes, validity, current_location FROM specimens WHERE group_id = ? ORDER BY specimen_no`, id)
	if err != nil {
		return domain.SampleGroup{}, nil, err
	}
	defer rows.Close()
	var specimens []domain.Specimen
	for rows.Next() {
		var specimen domain.Specimen
		var lastApplied, maxSeen, validity string
		if err := rows.Scan(&specimen.ID, &specimen.GroupID, &specimen.SpecimenNo, &specimen.BoundIdentity,
			&specimen.NominalSideMM, &specimen.Version, &lastApplied, &maxSeen,
			&specimen.EffectiveAgeMinutes, &validity, &specimen.CurrentLocation); err != nil {
			return domain.SampleGroup{}, nil, err
		}
		specimen.Validity = domain.Validity(validity)
		if lastApplied != "" {
			specimen.LastAppliedAt, _ = time.Parse(time.RFC3339Nano, lastApplied)
		}
		if maxSeen != "" {
			specimen.MaxSeenAt, _ = time.Parse(time.RFC3339Nano, maxSeen)
		}
		specimens = append(specimens, specimen)
	}
	return group, specimens, rows.Err()
}

func catalogError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.NewError("REFERENCE_NOT_FOUND", "NOT_FOUND", false)
	}
	if contains(err.Error(), "UNIQUE constraint failed") {
		return domain.NewError("IDENTITY_CONFLICT", "CONFLICT", false)
	}
	if contains(err.Error(), "FOREIGN KEY constraint failed") {
		return domain.NewError("REFERENCE_NOT_FOUND", "NOT_FOUND", false)
	}
	return fmt.Errorf("catalog storage: %w", err)
}
