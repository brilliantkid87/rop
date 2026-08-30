package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrIdempotencyKeyExists is returned when the (scope, key_hash) unique
// constraint rejects a second record — i.e. a concurrent request arrived with
// the same key. Callers converge on the existing record (architecture §10).
var ErrIdempotencyKeyExists = errors.New("idempotency key already exists in this scope")

// IdempotencyRow is one durable idempotency record: the mapping
// (scope, actionId, idempotencyKey) -> reversal attempt/result
// (Master Prompt §36). Raw keys are never persisted; only their SHA-256 hash.
type IdempotencyRow struct {
	Seq         int64
	Scope       string
	ActionID    string
	KeyHash     string
	Fingerprint string
	AttemptID   string
	CreatedAt   time.Time
}

// GetIdempotency looks up a record by (scope, keyHash).
func (s *Store) GetIdempotency(ctx context.Context, q DBTX, scope, keyHash string) (IdempotencyRow, bool, error) {
	row := q.QueryRowContext(ctx, `SELECT seq, scope, action_id, key_hash,
		request_fingerprint, attempt_id, created_at FROM idempotency_keys
		WHERE scope = ? AND key_hash = ?`, scope, keyHash)
	var r IdempotencyRow
	var created string
	if err := row.Scan(&r.Seq, &r.Scope, &r.ActionID, &r.KeyHash,
		&r.Fingerprint, &r.AttemptID, &created); err != nil {
		if err == sql.ErrNoRows {
			return IdempotencyRow{}, false, nil
		}
		return IdempotencyRow{}, false, fmt.Errorf("store: get idempotency: %w", err)
	}
	var err error
	if r.CreatedAt, err = time.Parse(timeFormat, created); err != nil {
		return IdempotencyRow{}, false, fmt.Errorf("store: parse idempotency created_at: %w", err)
	}
	return r, true, nil
}

// GetIdempotencyByAttempt returns the idempotency record for an attempt, if any.
func (s *Store) GetIdempotencyByAttempt(ctx context.Context, q DBTX, attemptID string) (IdempotencyRow, bool, error) {
	row := q.QueryRowContext(ctx, `SELECT seq, scope, action_id, key_hash,
		request_fingerprint, attempt_id, created_at FROM idempotency_keys
		WHERE attempt_id = ?`, attemptID)
	var r IdempotencyRow
	var created string
	if err := row.Scan(&r.Seq, &r.Scope, &r.ActionID, &r.KeyHash,
		&r.Fingerprint, &r.AttemptID, &created); err != nil {
		if err == sql.ErrNoRows {
			return IdempotencyRow{}, false, nil
		}
		return IdempotencyRow{}, false, fmt.Errorf("store: get idempotency by attempt: %w", err)
	}
	var err error
	if r.CreatedAt, err = time.Parse(timeFormat, created); err != nil {
		return IdempotencyRow{}, false, fmt.Errorf("store: parse idempotency created_at: %w", err)
	}
	return r, true, nil
}

// CreateIdempotency inserts a record inside the caller's transaction (the
// same transaction that creates the Reversal Attempt, so key registration and
// execution start commit atomically). The (scope, key_hash) unique index
// enforces the idempotency invariant at the database level.
func (s *Store) CreateIdempotency(ctx context.Context, q DBTX, r IdempotencyRow) error {
	_, err := q.ExecContext(ctx, `INSERT INTO idempotency_keys
		(scope, action_id, key_hash, request_fingerprint, attempt_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		r.Scope, r.ActionID, r.KeyHash, r.Fingerprint, r.AttemptID,
		r.CreatedAt.UTC().Format(timeFormat))
	if err != nil {
		if isUniqueConstraintErr(err) {
			return ErrIdempotencyKeyExists
		}
		return fmt.Errorf("store: create idempotency: %w", err)
	}
	return nil
}
