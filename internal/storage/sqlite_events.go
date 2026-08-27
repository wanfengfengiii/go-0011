package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"concrete-specimen-chain-service/internal/calculation"
	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/ingest"
)

func (r *SQLiteRepository) Receipt(ctx context.Context, key, digest string) (ingest.Receipt, bool, error) {
	var storedDigest, status, watermark string
	var version uint64
	err := r.db.QueryRowContext(ctx, `SELECT payload_digest, status, specimen_version, watermark
        FROM idempotency_receipts WHERE identity_key = ?`, key).Scan(&storedDigest, &status, &version, &watermark)
	if errors.Is(err, sql.ErrNoRows) {
		return ingest.Receipt{}, false, nil
	}
	if err != nil {
		return ingest.Receipt{}, false, err
	}
	if storedDigest != digest {
		return ingest.Receipt{}, false, domain.ErrIdentityConflict
	}
	receipt := ingest.Receipt{Status: status, Version: version}
	if watermark != "" {
		receipt.Watermark, _ = time.Parse(time.RFC3339Nano, watermark)
	}
	return receipt, true, nil
}

// CommitEvent atomically compares the specimen version, appends the fact,
// records its idempotent receipt and persists the pending sort entry.
func (r *SQLiteRepository) CommitEvent(ctx context.Context, event ingest.Event) (ingest.Receipt, error) {
	pressure, hasPressure, err := calculation.PressureFromEvent(event)
	if err != nil {
		return ingest.Receipt{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ingest.Receipt{}, err
	}
	defer tx.Commit()
	if prior, found, err := receiptTx(ctx, tx, event.Key(), event.PayloadDigest); err != nil || found {
		return prior, err
	}
	var version uint64
	err = tx.QueryRowContext(ctx, `SELECT version FROM specimens WHERE id = ?`, event.SpecimenID).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		// Device-first ingestion is supported for operational recovery; catalogued
		// specimens use the same optimistic version path.
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(expected_version + 1), 0)
            FROM specimen_events WHERE specimen_id = ?`, event.SpecimenID).Scan(&version)
	}
	if err != nil {
		return ingest.Receipt{}, err
	}
	if version != event.ExpectedVersion {
		return ingest.Receipt{}, domain.ErrVersionConflict
	}
	next := version + 1
	eventID := event.Key()
	receivedAt := r.clock().UTC()
	result, err := tx.ExecContext(ctx, `INSERT INTO specimen_events
        (event_id, source, specimen_id, sequence, occurred_at, received_at, expected_version,
         event_type, canonical_payload, payload_digest, applied_status)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'buffered')`, eventID, event.Source,
		event.SpecimenID, event.Sequence, event.OccurredAt.UTC().Format(time.RFC3339Nano),
		receivedAt.Format(time.RFC3339Nano), event.ExpectedVersion, event.Type,
		[]byte(event.CanonicalPayload), event.PayloadDigest)
	if err != nil {
		return ingest.Receipt{}, classifySQLiteConflict(err)
	}
	if _, err = result.LastInsertId(); err != nil {
		return ingest.Receipt{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pending_events
        (event_id, specimen_id, sort_time, business_priority, source, sequence) VALUES (?, ?, ?, ?, ?, ?)`,
		eventID, event.SpecimenID, event.OccurredAt.UTC().Format(time.RFC3339Nano), event.Type.Priority(), event.Source, event.Sequence); err != nil {
		return ingest.Receipt{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE specimens SET version = ?, max_seen_at =
        CASE WHEN max_seen_at IS NULL OR max_seen_at < ? THEN ? ELSE max_seen_at END WHERE id = ?`,
		next, event.OccurredAt.UTC().Format(time.RFC3339Nano), event.OccurredAt.UTC().Format(time.RFC3339Nano), event.SpecimenID); err != nil {
		return ingest.Receipt{}, err
	}
	if hasPressure {
		_, err = tx.ExecContext(ctx, `INSERT INTO pressure_results
            (specimen_id, machine_id, curve_digest, peak_load_kn, side_mm, factor, strength_mpa, validity)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(specimen_id) DO UPDATE SET
            machine_id=excluded.machine_id, curve_digest=excluded.curve_digest,
            peak_load_kn=excluded.peak_load_kn, side_mm=excluded.side_mm, factor=excluded.factor,
            strength_mpa=excluded.strength_mpa, validity=excluded.validity`, pressure.SpecimenID,
			pressure.MachineID, pressure.CurveDigest, pressure.PeakLoadKN, pressure.SideMM,
			pressure.Factor, pressure.StrengthMPa, pressure.Validity)
		if err != nil {
			return ingest.Receipt{}, err
		}
	}
	if event.Type == domain.EventTestCompleted {
		var resultCount int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pressure_results WHERE specimen_id = ?`,
			event.SpecimenID).Scan(&resultCount); err != nil {
			return ingest.Receipt{}, err
		}
		if resultCount == 0 {
			return ingest.Receipt{}, domain.ErrIllegalTransition
		}
	}
	receipt := ingest.Receipt{Status: "buffered", Version: next}
	if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_receipts
        (identity_key, payload_digest, status, specimen_version) VALUES (?, ?, ?, ?)`,
		eventID, event.PayloadDigest, receipt.Status, receipt.Version); err != nil {
		return ingest.Receipt{}, err
	}
	if err = tx.Commit(); err != nil {
		return ingest.Receipt{}, err
	}
	position, _ := r.lastPosition(ctx)
	r.status.LastGlobalPosition = position
	return receipt, nil
}

func receiptTx(ctx context.Context, tx *sql.Tx, key, digest string) (ingest.Receipt, bool, error) {
	var storedDigest, status, watermark string
	var version uint64
	err := tx.QueryRowContext(ctx, `SELECT payload_digest, status, specimen_version, watermark
        FROM idempotency_receipts WHERE identity_key = ?`, key).Scan(&storedDigest, &status, &version, &watermark)
	if errors.Is(err, sql.ErrNoRows) {
		return ingest.Receipt{}, false, nil
	}
	if err != nil {
		return ingest.Receipt{}, false, err
	}
	if storedDigest != digest {
		return ingest.Receipt{}, false, domain.ErrIdentityConflict
	}
	receipt := ingest.Receipt{Status: "duplicate", Version: version}
	if watermark != "" {
		receipt.Watermark, _ = time.Parse(time.RFC3339Nano, watermark)
	}
	return receipt, true, nil
}

func classifySQLiteConflict(err error) error {
	message := err.Error()
	if contains(message, "UNIQUE constraint failed") {
		return domain.ErrIdentityConflict
	}
	return err
}

func contains(value, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}

// PendingEvents restores persisted events into the deterministic in-process
// watermark buffers after a process restart.
func (r *SQLiteRepository) PendingEvents(ctx context.Context) ([]ingest.Event, map[string]time.Time, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT e.source, e.specimen_id, e.sequence, e.occurred_at,
        e.expected_version, e.event_type, e.canonical_payload, e.payload_digest
        FROM pending_events p JOIN specimen_events e ON e.event_id = p.event_id
        ORDER BY p.specimen_id, p.sort_time, p.business_priority, p.source, p.sequence`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var events []ingest.Event
	for rows.Next() {
		var event ingest.Event
		var occurred, eventType string
		if err := rows.Scan(&event.Source, &event.SpecimenID, &event.Sequence, &occurred,
			&event.ExpectedVersion, &eventType, &event.CanonicalPayload, &event.PayloadDigest); err != nil {
			return nil, nil, err
		}
		event.Type = domain.EventType(eventType)
		event.Payload = append(json.RawMessage(nil), event.CanonicalPayload...)
		event.OccurredAt, _ = time.Parse(time.RFC3339Nano, occurred)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	watermarks := make(map[string]time.Time)
	watermarkRows, err := r.db.QueryContext(ctx, `SELECT specimen_id, closed_until FROM specimen_watermarks`)
	if err != nil {
		return nil, nil, err
	}
	defer watermarkRows.Close()
	for watermarkRows.Next() {
		var specimenID, encoded string
		if err := watermarkRows.Scan(&specimenID, &encoded); err != nil {
			return nil, nil, err
		}
		watermarks[specimenID], _ = time.Parse(time.RFC3339Nano, encoded)
	}
	return events, watermarks, watermarkRows.Err()
}

// MarkApplied removes sorted events and advances the durable closed watermark.
func (r *SQLiteRepository) MarkApplied(ctx context.Context, specimenID string, ready []ingest.Event, until time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, event := range ready {
		if _, err := tx.ExecContext(ctx, `UPDATE specimen_events SET applied_status = 'applied'
            WHERE event_id = ?`, event.Key()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM pending_events WHERE event_id = ?`, event.Key()); err != nil {
			return err
		}
	}
	encoded := until.UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO specimen_watermarks(specimen_id, closed_until) VALUES (?, ?)
        ON CONFLICT(specimen_id) DO UPDATE SET closed_until = CASE
        WHEN excluded.closed_until > closed_until THEN excluded.closed_until ELSE closed_until END`, specimenID, encoded)
	if err != nil {
		return err
	}
	if len(ready) > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE specimens SET last_applied_at = ? WHERE id = ?`,
			ready[len(ready)-1].OccurredAt.UTC().Format(time.RFC3339Nano), specimenID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *SQLiteRepository) Chain(ctx context.Context, specimenID string) ([]domain.EventRecord, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT global_position, event_id, source, specimen_id, sequence,
        occurred_at, received_at, expected_version, event_type, canonical_payload, payload_digest,
        applied_status, COALESCE(classified_error, '') FROM specimen_events
        WHERE specimen_id = ? ORDER BY occurred_at, CASE event_type
        WHEN 'SAMPLED' THEN 1 WHEN 'MOLDED' THEN 2 WHEN 'TEMPERATURE' THEN 3 WHEN 'DEMOLDED' THEN 4
        WHEN 'TRANSPORTED' THEN 5 WHEN 'TEST_STARTED' THEN 6 WHEN 'LOAD_CURVE' THEN 7 ELSE 8 END,
        source, sequence`, specimenID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []domain.EventRecord
	for rows.Next() {
		var record domain.EventRecord
		var occurred, received, eventType string
		if err := rows.Scan(&record.GlobalPosition, &record.EventID, &record.Source, &record.SpecimenID,
			&record.Sequence, &occurred, &received, &record.ExpectedVersion, &eventType,
			&record.CanonicalPayload, &record.PayloadDigest, &record.AppliedStatus, &record.ClassifiedError); err != nil {
			return nil, err
		}
		record.Type = domain.EventType(eventType)
		record.OccurredAt, _ = time.Parse(time.RFC3339Nano, occurred)
		record.ReceivedAt, _ = time.Parse(time.RFC3339Nano, received)
		records = append(records, record)
	}
	return records, rows.Err()
}

func (r *SQLiteRepository) LoadEventsAfter(ctx context.Context, after uint64) ([]domain.EventRecord, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT global_position, event_id, source, specimen_id, sequence,
        occurred_at, received_at, expected_version, event_type, canonical_payload, payload_digest,
        applied_status, COALESCE(classified_error, '') FROM specimen_events WHERE global_position > ?
        ORDER BY global_position`, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []domain.EventRecord
	for rows.Next() {
		var record domain.EventRecord
		var occurred, received, eventType string
		if err := rows.Scan(&record.GlobalPosition, &record.EventID, &record.Source, &record.SpecimenID,
			&record.Sequence, &occurred, &received, &record.ExpectedVersion, &eventType,
			&record.CanonicalPayload, &record.PayloadDigest, &record.AppliedStatus, &record.ClassifiedError); err != nil {
			return nil, err
		}
		record.Type = domain.EventType(eventType)
		record.OccurredAt, _ = time.Parse(time.RFC3339Nano, occurred)
		record.ReceivedAt, _ = time.Parse(time.RFC3339Nano, received)
		records = append(records, record)
	}
	return records, rows.Err()
}

func (r *SQLiteRepository) EventCount(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM specimen_events`).Scan(&count)
	return count, err
}

func (r *SQLiteRepository) PressureResult(ctx context.Context, specimenID string) (*domain.PressureResult, error) {
	var result domain.PressureResult
	var validity string
	err := r.db.QueryRowContext(ctx, `SELECT specimen_id, machine_id, curve_digest, peak_load_kn,
        side_mm, factor, strength_mpa, validity FROM pressure_results WHERE specimen_id = ?`, specimenID).
		Scan(&result.SpecimenID, &result.MachineID, &result.CurveDigest, &result.PeakLoadKN,
			&result.SideMM, &result.Factor, &result.StrengthMPa, &validity)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result.Validity = domain.Validity(validity)
	return &result, nil
}
