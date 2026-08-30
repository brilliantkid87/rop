-- ROP v0.1 reference implementation, migration 0003 (Milestone 4).
-- Retry taxonomy on attempts + durable reconciliation observations.

-- Internal failure classification of the attempt (Master Prompt §37). Values:
-- RETRYABLE | NON_RETRYABLE | RECONCILE_REQUIRED | MANUAL_INTERVENTION_REQUIRED
-- NULL = attempt concluded without a classified failure. Not a protocol enum.
ALTER TABLE reversal_attempts ADD COLUMN retry_class TEXT;

-- Append-only reconciliation evidence (Master Prompt §38). One row per
-- reconciliation lookup; uncertainty history is never overwritten.
CREATE TABLE reconciliation_observations (
    seq        INTEGER PRIMARY KEY AUTOINCREMENT,
    attempt_id TEXT NOT NULL REFERENCES reversal_attempts(attempt_id),
    observed_at TEXT NOT NULL,
    evidence   TEXT NOT NULL CHECK (evidence IN
        ('PROVEN_REVERSED', 'PROVEN_NOT_REVERSED', 'INCONCLUSIVE')),
    detail     TEXT
);
CREATE INDEX idx_recon_attempt ON reconciliation_observations(attempt_id);
