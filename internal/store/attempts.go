package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func jsonMarshal(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func jsonUnmarshal(s string) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// AttemptRow is one Reversal Attempt (Master Prompt §32): a first-class
// entity, never mutable fields on Action. ProviderRef is the durable, stable
// provider execution identity, pre-assigned before any provider call (M4).
// RetryClass is the internal failure classification (Master Prompt §37); nil
// for attempts that concluded without a classified failure.
type AttemptRow struct {
	AttemptID          string
	ActionID           string
	RequestedAt        time.Time
	ExecutionState     string  // PENDING | RUNNING | AWAITING_RECONCILIATION | CONCLUDED
	ObservedResult     *string // REVERSED | PARTIALLY_REVERSED | REVERSE_FAILED | CONFLICT
	ProviderRef        *string
	Error              *string
	RetryClass         *string
	VerificationStatus *string
	ConcludedAt        *time.Time
}

// Attempt execution states (CHECK-constrained in the schema).
const (
	AttemptPending                = "PENDING"
	AttemptRunning                = "RUNNING"
	AttemptAwaitingReconciliation = "AWAITING_RECONCILIATION"
	AttemptConcluded              = "CONCLUDED"
)

// Attempt observed results (CHECK-constrained in the schema). There is
// deliberately no OUTCOME_UNKNOWN observed result: an unknown outcome keeps
// the attempt non-concluded with state AWAITING_RECONCILIATION (invariant I-5).
const (
	ObservedReversed          = "REVERSED"
	ObservedPartiallyReversed = "PARTIALLY_REVERSED"
	ObservedReverseFailed     = "REVERSE_FAILED"
	ObservedConflict          = "CONFLICT"
)

// CreateAttempt persists a new attempt in state RUNNING. The partial unique
// index on non-concluded attempts rejects a second concurrent attempt with
// ErrAttemptInProgress (architecture §10). M1 records no idempotency keys
// (Milestone 3); duplicates are refused, not silently deduped.
func (s *Store) CreateAttempt(ctx context.Context, q DBTX, a AttemptRow) error {
	_, err := q.ExecContext(ctx, `INSERT INTO reversal_attempts
		(attempt_id, action_id, requested_at, execution_state, provider_ref)
		VALUES (?, ?, ?, ?, ?)`,
		a.AttemptID, a.ActionID, a.RequestedAt.UTC().Format(timeFormat), AttemptRunning, a.ProviderRef)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return ErrAttemptInProgress
		}
		return fmt.Errorf("store: create attempt: %w", err)
	}
	return nil
}

// GetOpenAttempt returns the non-concluded attempt for an Action, if any.
func (s *Store) GetOpenAttempt(ctx context.Context, q DBTX, actionID string) (AttemptRow, bool, error) {
	row := q.QueryRowContext(ctx, `SELECT attempt_id, action_id, requested_at,
		execution_state, observed_result, provider_ref, error, retry_class, verification_status, concluded_at
		FROM reversal_attempts WHERE action_id = ? AND execution_state != ?`,
		actionID, AttemptConcluded)
	return scanAttempt(row)
}

// GetAttempt returns one attempt by ID.
func (s *Store) GetAttempt(ctx context.Context, q DBTX, attemptID string) (AttemptRow, bool, error) {
	row := q.QueryRowContext(ctx, `SELECT attempt_id, action_id, requested_at,
		execution_state, observed_result, provider_ref, error, retry_class, verification_status, concluded_at
		FROM reversal_attempts WHERE attempt_id = ?`, attemptID)
	return scanAttempt(row)
}

func scanAttempt(row *sql.Row) (AttemptRow, bool, error) {
	var a AttemptRow
	var requested string
	var observed, providerRef, errMsg, retryClass, verification sql.NullString
	var concluded sql.NullString
	if err := row.Scan(&a.AttemptID, &a.ActionID, &requested, &a.ExecutionState,
		&observed, &providerRef, &errMsg, &retryClass, &verification, &concluded); err != nil {
		if err == sql.ErrNoRows {
			return AttemptRow{}, false, nil
		}
		return AttemptRow{}, false, fmt.Errorf("store: scan attempt: %w", err)
	}
	var err error
	if a.RequestedAt, err = time.Parse(timeFormat, requested); err != nil {
		return AttemptRow{}, false, fmt.Errorf("store: parse requested_at: %w", err)
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
			return AttemptRow{}, false, fmt.Errorf("store: parse concluded_at: %w", err)
		}
		a.ConcludedAt = &t
	}
	return a, true, nil
}

// ConcludeAttempt records the observed result of an attempt. For
// OUTCOME_UNKNOWN outcomes use MarkAwaitingReconciliation instead: the attempt
// must remain open for reconciliation (invariant I-5, architecture §6).
// ConcludeAttempt records the observed result of an attempt, optionally with
// the internal failure classification (retryClass may be nil). The
// pre-assigned provider_ref is never overwritten: it is the stable execution
// identity that reconciliation looks up (M4). For OUTCOME_UNKNOWN outcomes
// use MarkAwaitingReconciliation instead (invariant I-5).
func (s *Store) ConcludeAttempt(ctx context.Context, q DBTX, attemptID, observedResult string, errMsg, retryClass *string, at time.Time) error {
	_, err := q.ExecContext(ctx, `UPDATE reversal_attempts SET
		execution_state = ?, observed_result = ?,
		error = COALESCE(?, error), retry_class = COALESCE(?, retry_class), concluded_at = ?
		WHERE attempt_id = ? AND execution_state != ?`,
		AttemptConcluded, observedResult, errMsg, retryClass,
		at.UTC().Format(timeFormat), attemptID, AttemptConcluded)
	if err != nil {
		return fmt.Errorf("store: conclude attempt: %w", err)
	}
	return nil
}

// MarkAwaitingReconciliation moves a RUNNING attempt to AWAITING_RECONCILIATION
// without concluding it (outcome unobserved). The open-attempt index keeps
// blocking further reversals until evidence arrives.
func (s *Store) MarkAwaitingReconciliation(ctx context.Context, q DBTX, attemptID string, errMsg, retryClass *string, at time.Time) error {
	_, err := q.ExecContext(ctx, `UPDATE reversal_attempts SET
		execution_state = ?, error = ?, retry_class = ?
		WHERE attempt_id = ? AND execution_state = ?`,
		AttemptAwaitingReconciliation, errMsg, retryClass,
		attemptID, AttemptRunning)
	if err != nil {
		return fmt.Errorf("store: mark awaiting reconciliation: %w", err)
	}
	return nil
}

// SetAttemptVerificationStatus records a verification status on an attempt
// (kept even after conclusion, for audit).
func (s *Store) SetAttemptVerificationStatus(ctx context.Context, q DBTX, attemptID, status string) error {
	_, err := q.ExecContext(ctx, `UPDATE reversal_attempts SET verification_status = ?
		WHERE attempt_id = ?`, status, attemptID)
	if err != nil {
		return fmt.Errorf("store: set attempt verification: %w", err)
	}
	return nil
}

// isUniqueConstraintErr reports whether err is a SQLite UNIQUE violation
// (modernc driver surfaces constraint failures as
// "constraint failed: UNIQUE constraint failed: ..."). FK and CHECK
// violations must NOT match: they are data-integrity bugs, not replay races.
func isUniqueConstraintErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// GetLatestAttempt returns the most recent attempt for an Action (any state).
func (s *Store) GetLatestAttempt(ctx context.Context, q DBTX, actionID string) (AttemptRow, bool, error) {
	row := q.QueryRowContext(ctx, `SELECT attempt_id, action_id, requested_at,
		execution_state, observed_result, provider_ref, error, retry_class, verification_status, concluded_at
		FROM reversal_attempts WHERE action_id = ?
		ORDER BY requested_at DESC, attempt_id DESC LIMIT 1`, actionID)
	return scanAttempt(row)
}

// GetRunningAttempts returns every attempt still in state RUNNING across all
// scopes — the recovery candidate set after a process crash (Master Prompt
// §60). Recovery must inspect durable evidence, never assume failure (I-11).
func (s *Store) GetRunningAttempts(ctx context.Context, q DBTX) ([]AttemptRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT attempt_id, action_id, requested_at,
		execution_state, observed_result, provider_ref, error, retry_class, verification_status, concluded_at
		FROM reversal_attempts WHERE execution_state = ?`, AttemptRunning)
	if err != nil {
		return nil, fmt.Errorf("store: get running attempts: %w", err)
	}
	defer rows.Close()
	var out []AttemptRow
	for rows.Next() {
		a, err := scanAttemptRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
