-- ROP v0.1 reference implementation, migration 0001.
-- See docs/architecture.md §8 (persistence) and docs/failure-model.md.
-- Column additions must carry a comment stating why the data is required
-- (data minimization, Master Prompt §14).

CREATE TABLE operations (
    -- Operation-level capability metadata (reusable behavior definition).
    operation_id        TEXT PRIMARY KEY,
    reversibility       TEXT NOT NULL,
    guarantee           TEXT NOT NULL,
    ttl_seconds         INTEGER,                -- NULL = no eligibility window
    reverse_operation_id TEXT
);

CREATE TABLE actions (
    -- One concrete execution of an Operation. Insert-only; status changes are
    -- durable updates plus append-only history rows (invariant I-1).
    action_id     TEXT PRIMARY KEY,
    scope         TEXT NOT NULL,                -- scope isolation (invariant I-13)
    operation_id  TEXT NOT NULL REFERENCES operations(operation_id),
    status        TEXT NOT NULL,
    reversibility TEXT NOT NULL,                -- Action-time metadata (invariant I-15)
    guarantee     TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    expires_at    TEXT,                         -- NULL = no window (server time governs)
    residue_json  TEXT NOT NULL DEFAULT '[]'
);
CREATE INDEX idx_actions_scope ON actions(scope);

CREATE TABLE action_status_history (
    seq        INTEGER PRIMARY KEY AUTOINCREMENT,
    action_id  TEXT NOT NULL REFERENCES actions(action_id),
    from_status TEXT,
    to_status  TEXT NOT NULL,
    changed_at TEXT NOT NULL,
    note       TEXT
);

CREATE TABLE reversal_attempts (
    -- First-class Reversal Attempt entity (Master Prompt §32). Not fields on Action.
    attempt_id     TEXT PRIMARY KEY,
    action_id      TEXT NOT NULL REFERENCES actions(action_id),
    requested_at   TEXT NOT NULL,
    execution_state TEXT NOT NULL CHECK (execution_state IN
        ('PENDING','RUNNING','AWAITING_RECONCILIATION','CONCLUDED')),
    observed_result TEXT CHECK (observed_result IS NULL OR observed_result IN
        ('REVERSED','PARTIALLY_REVERSED','REVERSE_FAILED','CONFLICT')),
    provider_ref   TEXT,
    error          TEXT,
    verification_status TEXT,
    concluded_at   TEXT
);
-- At most one non-concluded attempt per Action. M1 concurrency guard; full
-- durable idempotency-key persistence is Milestone 3.
CREATE UNIQUE INDEX idx_open_attempt
    ON reversal_attempts(action_id) WHERE execution_state != 'CONCLUDED';

CREATE TABLE reversal_material (
    -- Private Reversal Material (Master Prompt §13). NEVER serialized on public
    -- paths (invariant I-14). Retention tied to eligibility window (§15).
    action_id    TEXT PRIMARY KEY REFERENCES actions(action_id),
    material_json TEXT NOT NULL
);

CREATE TABLE verification_results (
    -- Verification is independent of execution success (invariant I-10).
    seq                 INTEGER PRIMARY KEY AUTOINCREMENT,
    action_id           TEXT NOT NULL REFERENCES actions(action_id),
    status              TEXT NOT NULL CHECK (status IN ('VERIFIED','FAILED','UNKNOWN')),
    semantics           TEXT NOT NULL,
    postconditions_json TEXT NOT NULL,
    evaluated_at        TEXT NOT NULL
);

-- Demo provider business state. Provider-owned; ROP Core never reads this table
-- directly (provider adapters do). Lives in the same SQLite database so the
-- M1 "business state + Action journal" write is locally atomic
-- (docs/architecture.md §9).
CREATE TABLE resources (
    resource_id        TEXT PRIMARY KEY,
    scope              TEXT NOT NULL,
    version            INTEGER NOT NULL,
    value              TEXT NOT NULL,
    published          INTEGER NOT NULL DEFAULT 0,
    created_from_action TEXT NOT NULL
);
