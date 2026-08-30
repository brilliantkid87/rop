package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// scanAttemptRows scans one attempt row from an open rows cursor.
func scanAttemptRows(rows *sql.Rows) (AttemptRow, error) {
	var a AttemptRow
	var requested string
	var observed, providerRef, errMsg, retryClass, verification sql.NullString
	var concluded sql.NullString
	if err := rows.Scan(&a.AttemptID, &a.ActionID, &requested, &a.ExecutionState,
		&observed, &providerRef, &errMsg, &retryClass, &verification, &concluded); err != nil {
		return AttemptRow{}, fmt.Errorf("store: scan attempt: %w", err)
	}
	var err error
	if a.RequestedAt, err = time.Parse(timeFormat, requested); err != nil {
		return AttemptRow{}, fmt.Errorf("store: parse requested_at: %w", err)
	}
	if observed.Valid {
		v := observed.String
		a.ObservedResult = &v
	}
	if providerRef.Valid {
		v := providerRef.String
		a.ProviderRef = &v
	}
	if errMsg.Valid {
		v := errMsg.String
		a.Error = &v
	}
	if retryClass.Valid {
		v := retryClass.String
		a.RetryClass = &v
	}
	if verification.Valid {
		v := verification.String
		a.VerificationStatus = &v
	}
	if concluded.Valid {
		t, err := time.Parse(timeFormat, concluded.String)
		if err != nil {
			return AttemptRow{}, fmt.Errorf("store: parse concluded_at: %w", err)
		}
		a.ConcludedAt = &t
	}
	return a, nil
}

// Reconciliation observation evidence classes (CHECK-constrained in the
// schema). PROVEN_* means the provider adapter's contract guarantees the
// conclusion; INCONCLUSIVE preserves uncertainty (invariant: negative
// lookups are not proof unless the contract says so).
const (
	EvidenceProvenReversed    = "PROVEN_REVERSED"
	EvidenceProvenNotReversed = "PROVEN_NOT_REVERSED"
	EvidenceInconclusive      = "INCONCLUSIVE"
)

// ObservationRow is one append-only reconciliation observation (Master
// Prompt §38): the durable record of a provider lookup against an uncertain
// attempt. Uncertainty history is never overwritten.
type ObservationRow struct {
	Seq        int64
	AttemptID  string
	ObservedAt time.Time
	Evidence   string
	Detail     string
}

// RecordObservation appends one reconciliation observation.
func (s *Store) RecordObservation(ctx context.Context, q DBTX, r ObservationRow) error {
	_, err := q.ExecContext(ctx, `INSERT INTO reconciliation_observations
		(attempt_id, observed_at, evidence, detail) VALUES (?, ?, ?, ?)`,
		r.AttemptID, r.ObservedAt.UTC().Format(timeFormat), r.Evidence, r.Detail)
	if err != nil {
		return fmt.Errorf("store: record observation: %w", err)
	}
	return nil
}

// ObservationsForAttempt returns the full observation history of an attempt,
// oldest first.
func (s *Store) ObservationsForAttempt(ctx context.Context, q DBTX, attemptID string) ([]ObservationRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT seq, attempt_id, observed_at, evidence, detail
		FROM reconciliation_observations WHERE attempt_id = ? ORDER BY seq`, attemptID)
	if err != nil {
		return nil, fmt.Errorf("store: observations: %w", err)
	}
	defer rows.Close()
	var out []ObservationRow
	for rows.Next() {
		var r ObservationRow
		var observed string
		if err := rows.Scan(&r.Seq, &r.AttemptID, &observed, &r.Evidence, &r.Detail); err != nil {
			return nil, fmt.Errorf("store: scan observation: %w", err)
		}
		if r.ObservedAt, err = time.Parse(timeFormat, observed); err != nil {
			return nil, fmt.Errorf("store: parse observed_at: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
