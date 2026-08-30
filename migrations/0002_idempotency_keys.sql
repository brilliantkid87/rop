-- ROP v0.1 reference implementation, migration 0002 (Milestone 3).
-- Durable reversal-request idempotency (Master Prompt §36).

CREATE TABLE idempotency_keys (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    scope TEXT NOT NULL,
    action_id TEXT NOT NULL REFERENCES actions(action_id),
    -- SHA-256 hex of the raw client key. Raw keys are never stored
    -- (data minimization, Master Prompt §14): they may carry correlation
    -- information the protocol does not need.
    key_hash TEXT NOT NULL CHECK (length(key_hash) = 64),
    -- SHA-256 hex of the request semantics (scope, actionId, verb). Detects
    -- reuse of a key with materially different request semantics.
    request_fingerprint TEXT NOT NULL CHECK (length(request_fingerprint) = 64),
    attempt_id TEXT NOT NULL REFERENCES reversal_attempts(attempt_id),
    created_at TEXT NOT NULL
);

-- The critical idempotency invariant, enforced by the database, not only by
-- application checks: at most one idempotency record per (scope, key).
-- A key therefore can never silently drive two different actions within one
-- scope, and concurrent replays converge on a single record and attempt.
CREATE UNIQUE INDEX idx_idem_scope_key ON idempotency_keys(scope, key_hash);
