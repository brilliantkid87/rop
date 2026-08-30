package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/brilliantkid87/rop/internal/action"
)

const timeFormat = "2006-01-02T15:04:05.000000000Z07:00" // fixed-width RFC 3339: lexicographic == chronological

// NewID returns a high-entropy identifier. Unguessability is privacy hygiene
// only, never authorization (invariant I-2).
func NewID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("store: crypto/rand unavailable: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

// OperationRow is Operation-level capability metadata (reusable definition).
type OperationRow struct {
	OperationID        string
	Reversibility      string
	Guarantee          string
	TTLSeconds         *int64 // nil = no eligibility window
	ReverseOperationID *string
}

// UpsertOperation persists Operation metadata.
func (s *Store) UpsertOperation(ctx context.Context, q DBTX, o OperationRow) error {
	_, err := q.ExecContext(ctx, `INSERT INTO operations
		(operation_id, reversibility, guarantee, ttl_seconds, reverse_operation_id)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(operation_id) DO UPDATE SET
		reversibility=excluded.reversibility, guarantee=excluded.guarantee,
		ttl_seconds=excluded.ttl_seconds, reverse_operation_id=excluded.reverse_operation_id`,
		o.OperationID, o.Reversibility, o.Guarantee, o.TTLSeconds, o.ReverseOperationID)
	if err != nil {
		return fmt.Errorf("store: upsert operation: %w", err)
	}
	return nil
}

// GetOperation reads Operation metadata.
func (s *Store) GetOperation(ctx context.Context, q DBTX, id string) (OperationRow, bool, error) {
	row := q.QueryRowContext(ctx, `SELECT operation_id, reversibility, guarantee,
		ttl_seconds, reverse_operation_id FROM operations WHERE operation_id = ?`, id)
	var o OperationRow
	var ttl sql.NullInt64
	var revOp sql.NullString
	if err := row.Scan(&o.OperationID, &o.Reversibility, &o.Guarantee, &ttl, &revOp); err != nil {
		if err == sql.ErrNoRows {
			return OperationRow{}, false, nil
		}
		return OperationRow{}, false, fmt.Errorf("store: get operation: %w", err)
	}
	if ttl.Valid {
		v := ttl.Int64
		o.TTLSeconds = &v
	}
	if revOp.Valid {
		v := revOp.String
		o.ReverseOperationID = &v
	}
	return o, true, nil
}

// ActionRow is one concrete execution of an Operation. Reversibility and
// Guarantee are Action-time metadata: current Operation metadata never
// rewrites them (invariant I-15).
type ActionRow struct {
	ActionID      string
	Scope         string
	OperationID   string
	Status        string
	Reversibility string
	Guarantee     string
	ResourceType  string
	ResourceID    string
	CreatedAt     time.Time
	ExpiresAt     *time.Time // nil = no eligibility window
	Residue       string     // JSON array, provider-declared (OQ-3: free-form in v0.1)
}

// CreateAction inserts an Action and its initial status-history row inside
// the caller's transaction (so business state and journal commit atomically,
// architecture §9). material may be nil; when present it is persisted as
// private reversal material (never serialized on public paths, I-14).
func (s *Store) CreateAction(ctx context.Context, q DBTX, a ActionRow, material map[string]any) error {
	// No eligibility window is stored as SQL NULL (not '') so that
	// `expires_at <= ?` comparisons in SweepExpiry cannot match it.
	var expiresAt any
	if a.ExpiresAt != nil {
		expiresAt = a.ExpiresAt.UTC().Format(timeFormat)
	}
	_, err := q.ExecContext(ctx, `INSERT INTO actions
		(action_id, scope, operation_id, status, reversibility, guarantee,
		 resource_type, resource_id, created_at, expires_at, residue_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ActionID, a.Scope, a.OperationID, a.Status, a.Reversibility, a.Guarantee,
		a.ResourceType, a.ResourceID, a.CreatedAt.UTC().Format(timeFormat),
		expiresAt, a.Residue)
	if err != nil {
		return fmt.Errorf("store: create action: %w", err)
	}
	if _, err := q.ExecContext(ctx, `INSERT INTO action_status_history
		(action_id, from_status, to_status, changed_at) VALUES (?, NULL, ?, ?)`,
		a.ActionID, a.Status, a.CreatedAt.UTC().Format(timeFormat)); err != nil {
		return fmt.Errorf("store: initial history row: %w", err)
	}
	if material != nil {
		mat, err := jsonMarshal(material)
		if err != nil {
			return fmt.Errorf("store: material marshal: %w", err)
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO reversal_material
			(action_id, material_json) VALUES (?, ?)`, a.ActionID, mat); err != nil {
			return fmt.Errorf("store: create reversal material: %w", err)
		}
	}
	return nil
}

// GetAction reads one Action. All lookups are scope-filtered: a cross-scope
// ID is indistinguishable from a nonexistent one (invariant I-13).
func (s *Store) GetAction(ctx context.Context, q DBTX, scope, actionID string) (ActionRow, bool, error) {
	row := q.QueryRowContext(ctx, `SELECT action_id, scope, operation_id, status,
		reversibility, guarantee, resource_type, resource_id, created_at,
		expires_at, residue_json FROM actions
		WHERE scope = ? AND action_id = ?`, scope, actionID)
	var a ActionRow
	var createdAt string
	var expiresAt sql.NullString
	if err := row.Scan(&a.ActionID, &a.Scope, &a.OperationID, &a.Status,
		&a.Reversibility, &a.Guarantee, &a.ResourceType, &a.ResourceID,
		&createdAt, &expiresAt, &a.Residue); err != nil {
		if err == sql.ErrNoRows {
			return ActionRow{}, false, nil
		}
		return ActionRow{}, false, fmt.Errorf("store: get action: %w", err)
	}
	var err error
	if a.CreatedAt, err = time.Parse(timeFormat, createdAt); err != nil {
		return ActionRow{}, false, fmt.Errorf("store: parse created_at: %w", err)
	}
	if expiresAt.Valid {
		t, err := time.Parse(timeFormat, expiresAt.String)
		if err != nil {
			return ActionRow{}, false, fmt.Errorf("store: parse expires_at: %w", err)
		}
		a.ExpiresAt = &t
	}
	return a, true, nil
}

// GetActionByID reads an Action without a scope filter. Internal use only
// (restart recovery sweeps all scopes); public paths MUST stay scope-filtered
// (invariant I-13).
func (s *Store) GetActionByID(ctx context.Context, q DBTX, actionID string) (ActionRow, bool, error) {
	row := q.QueryRowContext(ctx, `SELECT action_id, scope, operation_id, status,
		reversibility, guarantee, resource_type, resource_id, created_at,
		expires_at, residue_json FROM actions WHERE action_id = ?`, actionID)
	var a ActionRow
	var createdAt string
	var expiresAt, scope sql.NullString
	if err := row.Scan(&a.ActionID, &scope, &a.OperationID, &a.Status,
		&a.Reversibility, &a.Guarantee, &a.ResourceType, &a.ResourceID,
		&createdAt, &expiresAt, &a.Residue); err != nil {
		if err == sql.ErrNoRows {
			return ActionRow{}, false, nil
		}
		return ActionRow{}, false, fmt.Errorf("store: get action by id: %w", err)
	}
	a.Scope = scope.String
	var err error
	if a.CreatedAt, err = time.Parse(timeFormat, createdAt); err != nil {
		return ActionRow{}, false, fmt.Errorf("store: parse created_at: %w", err)
	}
	if expiresAt.Valid {
		t, err := time.Parse(timeFormat, expiresAt.String)
		if err != nil {
			return ActionRow{}, false, fmt.Errorf("store: parse expires_at: %w", err)
		}
		a.ExpiresAt = &t
	}
	return a, true, nil
}

// UpdateStatus applies one validated state transition durably: it updates the
// Action row and appends a history row (invariant I-1). An illegal transition
// is a programmer error and is refused.
func (s *Store) UpdateStatus(ctx context.Context, q DBTX, scope, actionID, from, to, note string, at time.Time) error {
	if !action.CanTransition(from, to) {
		return action.TransitionError{From: from, To: to}
	}
	res, err := q.ExecContext(ctx, `UPDATE actions SET status = ?
		WHERE scope = ? AND action_id = ? AND status = ?`,
		to, scope, actionID, from)
	if err != nil {
		return fmt.Errorf("store: update status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update status: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("store: action %s not in status %s", actionID, from)
	}
	if _, err := q.ExecContext(ctx, `INSERT INTO action_status_history
		(action_id, from_status, to_status, changed_at, note) VALUES (?, ?, ?, ?, ?)`,
		actionID, from, to, at.UTC().Format(timeFormat), note); err != nil {
		return fmt.Errorf("store: history row: %w", err)
	}
	return nil
}

// GetMaterial reads private reversal material. Callers MUST NOT serialize the
// result onto public paths (invariant I-14).
func (s *Store) GetMaterial(ctx context.Context, q DBTX, scope, actionID string) (map[string]any, bool, error) {
	// The join enforces scope isolation without trusting the caller.
	row := q.QueryRowContext(ctx, `SELECT m.material_json FROM reversal_material m
		JOIN actions a ON a.action_id = m.action_id
		WHERE a.scope = ? AND m.action_id = ?`, scope, actionID)
	var raw string
	if err := row.Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("store: get material: %w", err)
	}
	m, err := jsonUnmarshal(raw)
	if err != nil {
		return nil, false, fmt.Errorf("store: material unmarshal: %w", err)
	}
	return m, true, nil
}

// RecordVerification appends a verification result (invariant I-10:
// verification is independent of execution success and is re-runnable).
func (s *Store) RecordVerification(ctx context.Context, q DBTX, actionID, status, semantics, postconditionsJSON string, at time.Time) error {
	_, err := q.ExecContext(ctx, `INSERT INTO verification_results
		(action_id, status, semantics, postconditions_json, evaluated_at)
		VALUES (?, ?, ?, ?, ?)`,
		actionID, status, semantics, postconditionsJSON, at.UTC().Format(timeFormat))
	if err != nil {
		return fmt.Errorf("store: record verification: %w", err)
	}
	return nil
}

// LatestVerification returns the most recent verification result for an Action.
func (s *Store) LatestVerification(ctx context.Context, q DBTX, actionID string) (status, semantics, postconditionsJSON string, evaluatedAt time.Time, ok bool, err error) {
	row := q.QueryRowContext(ctx, `SELECT status, semantics, postconditions_json, evaluated_at
		FROM verification_results WHERE action_id = ?
		ORDER BY seq DESC LIMIT 1`, actionID)
	var evaluated string
	if err := row.Scan(&status, &semantics, &postconditionsJSON, &evaluated); err != nil {
		if err == sql.ErrNoRows {
			return "", "", "", time.Time{}, false, nil
		}
		return "", "", "", time.Time{}, false, fmt.Errorf("store: latest verification: %w", err)
	}
	if evaluatedAt, err = time.Parse(timeFormat, evaluated); err != nil {
		return "", "", "", time.Time{}, false, fmt.Errorf("store: parse evaluated_at: %w", err)
	}
	return status, semantics, postconditionsJSON, evaluatedAt, true, nil
}

// SweepExpiry applies the server-time expiration rule (Master Prompt §24, §52):
// receivedAt >= expiresAt ⇒ expired. It transitions every APPLIED Action whose
// window has passed. Boundary equality expires — this exact edge is tested.
func (s *Store) SweepExpiry(ctx context.Context, q DBTX, now time.Time) (int, error) {
	rows, err := q.QueryContext(ctx, `SELECT action_id, scope FROM actions
		WHERE status = ? AND expires_at IS NOT NULL AND expires_at <= ?`,
		action.Applied, now.UTC().Format(timeFormat))
	if err != nil {
		return 0, fmt.Errorf("store: sweep select: %w", err)
	}
	var expired []struct{ id, scope string }
	for rows.Next() {
		var id, scope string
		if err := rows.Scan(&id, &scope); err != nil {
			rows.Close()
			return 0, fmt.Errorf("store: sweep scan: %w", err)
		}
		expired = append(expired, struct{ id, scope string }{id, scope})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("store: sweep rows: %w", err)
	}
	for _, e := range expired {
		if err := s.UpdateStatus(ctx, q, e.scope, e.id, action.Applied, action.Expired,
			"eligibility window passed (server time)", now); err != nil {
			return 0, fmt.Errorf("store: sweep transition %s: %w", e.id, err)
		}
	}
	return len(expired), nil
}
