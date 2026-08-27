package storage_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/evaluation"
	"concrete-specimen-chain-service/internal/ingest"
	"concrete-specimen-chain-service/internal/storage"
)

func sqlitePath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(".", fmt.Sprintf(".%s-%d.db", t.Name(), time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = os.Remove(path)
		_ = os.Remove(path + "-shm")
		_ = os.Remove(path + "-wal")
	})
	return path
}

func cataloguedRepository(t *testing.T, path string) (*storage.SQLiteRepository, domain.FrozenRule) {
	t.Helper()
	ctx := context.Background()
	repository, err := storage.OpenSQLite(ctx, path, func() time.Time {
		return time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	project := domain.Project{ID: "p1", Name: "project", SiteCode: "SITE", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	if err := repository.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	section := domain.PourSection{ID: "pour1", ProjectID: project.ID, Name: "zone", Location: "A1", PlannedPourAt: project.CreatedAt}
	if err := repository.CreatePourSection(ctx, section); err != nil {
		t.Fatal(err)
	}
	design := domain.MixDesign{ID: "mix1", ProjectID: project.ID, Code: "C30", DesignStrengthMPa: 30, MaterialRevision: "r1"}
	if err := repository.CreateMixDesign(ctx, design); err != nil {
		t.Fatal(err)
	}
	rule := domain.DefaultInspectionRule(project.ID, 1)
	rule.ID, rule.RequiredSpecimens, rule.CreatedAt = "rule1", 1, project.CreatedAt
	if err := repository.CreateInspectionRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	frozen, err := rule.Freeze(design.DesignStrengthMPa, 150)
	if err != nil {
		t.Fatal(err)
	}
	group := domain.SampleGroup{ID: "g1", PourSectionID: section.ID, MixDesignID: design.ID, Rule: frozen}
	if err := repository.CreateSampleGroup(ctx, storage.GroupCreation{Group: group, ProjectID: project.ID,
		SpecimenIDs: []string{"s1"}, SpecimenNos: []string{"S-001"}, NominalSideMM: 150}); err != nil {
		t.Fatal(err)
	}
	return repository, frozen
}

func TestSQLiteRecoversPendingEventAndIdempotency(t *testing.T) {
	path := sqlitePath(t)
	repository, _ := cataloguedRepository(t, path)
	service := ingest.NewService(repository)
	at := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	envelope := ingest.Envelope{Source: "device", SpecimenID: "s1", Sequence: 1, OccurredAt: at,
		ExpectedVersion: 0, Type: domain.EventSampled, Payload: json.RawMessage(`{"identity":"seal"}`)}
	receipt, err := service.Submit(context.Background(), envelope)
	if err != nil || receipt.Version != 1 {
		t.Fatalf("submit receipt=%+v error=%v", receipt, err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := storage.OpenSQLite(context.Background(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	recovered := ingest.NewService(restarted)
	if err := recovered.RecoverPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	ready := recovered.Advance("s1", at)
	if len(ready) != 1 || ready[0].Key() != "device/s1/1" {
		t.Fatalf("recovered ready events=%+v", ready)
	}
	duplicate, err := recovered.Submit(context.Background(), envelope)
	if err != nil || duplicate.Status != "duplicate" || duplicate.Version != 1 {
		t.Fatalf("duplicate receipt=%+v error=%v", duplicate, err)
	}
	chain, err := restarted.Chain(context.Background(), "s1")
	if err != nil || len(chain) != 1 || chain[0].AppliedStatus != "applied" {
		t.Fatalf("chain=%+v error=%v", chain, err)
	}
}

func TestSQLitePersistsFrozenEvaluationAndSeal(t *testing.T) {
	path := sqlitePath(t)
	repository, rule := cataloguedRepository(t, path)
	snapshot, err := evaluation.FreezeInitial("g1", 0, rule, []evaluation.SpecimenResult{{
		SpecimenID: "s1", StrengthMPa: 40, Validity: domain.ValidityValid,
	}}, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveInitialEvaluation(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	sealedAt := snapshot.CreatedAt.Add(time.Hour)
	if err := repository.SealEvaluation(context.Background(), "g1", snapshot, sealedAt); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := storage.OpenSQLite(context.Background(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	state, err := restarted.Evaluation(context.Background(), "g1")
	if err != nil {
		t.Fatal(err)
	}
	if state.Snapshot.ID != snapshot.ID || state.Group.SealedDigest != snapshot.CanonicalDigest ||
		state.Group.SealedConclusion != domain.ConclusionPassed || state.Group.SealedAt == nil {
		t.Fatalf("restored evaluation=%+v", state)
	}
}

func TestSQLiteDetectsInvalidCheckpointDigest(t *testing.T) {
	path := sqlitePath(t)
	repository, _ := cataloguedRepository(t, path)
	bad := storage.Checkpoint{GlobalPosition: 0, AggregateDigest: "not-the-blob-digest",
		SnapshotBlob: json.RawMessage(`{"global_position":0}`), CreatedAt: time.Now().UTC()}
	if err := repository.SaveCheckpoint(context.Background(), bad); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := storage.OpenSQLite(context.Background(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	status, err := restarted.Ready(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Ready || status.Phase != "ready_full_replay_digest_mismatch" {
		t.Fatalf("recovery status=%+v", status)
	}
}

func TestSQLiteCommitsLoadCurveAndPressureResultAtomically(t *testing.T) {
	path := sqlitePath(t)
	repository, rule := cataloguedRepository(t, path)
	defer repository.Close()
	payload, err := json.Marshal(map[string]any{
		"machine_id": "press-1", "load_unit": "kN", "side_unit": "mm", "side_mm": 150,
		"declared_peak_kn": 900, "frozen_rule": rule,
		"curve": []map[string]any{
			{"elapsed": 0, "load_kn": 0},
			{"elapsed": int64(time.Second), "load_kn": 100},
			{"elapsed": int64(2 * time.Second), "load_kn": 300},
			{"elapsed": int64(4 * time.Second), "load_kn": 900},
			{"elapsed": int64(5 * time.Second), "load_kn": 750},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	event, err := ingest.Normalize(ingest.Envelope{Source: "press", SpecimenID: "s1", Sequence: 1,
		OccurredAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), Type: domain.EventLoadCurve, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CommitEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	pressure, err := repository.PressureResult(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if pressure == nil || pressure.StrengthMPa != 40 || pressure.CurveDigest == "" {
		t.Fatalf("pressure result=%+v", pressure)
	}
	count, err := repository.EventCount(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("event count=%d error=%v", count, err)
	}
}
