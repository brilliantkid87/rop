-- ROP v0.1 reference implementation, migration 0004 (Milestone 5).
-- Action-to-Action dependencies (safety constraints, not workflow) and
-- append-only residue records.

CREATE TABLE action_dependencies (
    seq                INTEGER PRIMARY KEY AUTOINCREMENT,
    scope              TEXT NOT NULL,
    parent_action_id   TEXT NOT NULL REFERENCES actions(action_id),
    dependent_action_id TEXT NOT NULL REFERENCES actions(action_id),
    created_at         TEXT NOT NULL,
    -- parent = the Action whose reversal may be unsafe; dependent = the
    -- Action that depends on it ("B depends on A" => parent=A, dependent=B).
    -- Duplicate edges are the same fact; the unique index makes recording
    -- idempotent.
    UNIQUE(parent_action_id, dependent_action_id)
);
CREATE INDEX idx_dep_parent ON action_dependencies(scope, parent_action_id);
CREATE INDEX idx_dep_dependent ON action_dependencies(scope, dependent_action_id);

-- Residue is first-class, append-style evidence of what remains after or
-- despite reversal (Master Prompt §45). DECLARED = known before reversal
-- (planning); DISCOVERED = recorded during reversal execution; VERIFIED =
-- confirmed during verification. History is preserved, never overwritten.
CREATE TABLE action_residue (
    seq          INTEGER PRIMARY KEY AUTOINCREMENT,
    action_id    TEXT NOT NULL REFERENCES actions(action_id),
    source       TEXT NOT NULL CHECK (source IN ('DECLARED', 'DISCOVERED', 'VERIFIED')),
    residue_json TEXT NOT NULL,
    recorded_at  TEXT NOT NULL
);
CREATE INDEX idx_residue_action ON action_residue(action_id);

-- Demo provider business state for the PARTIALLY_COMPENSATABLE scenario.
-- Provider-owned; ROP Core never reads this table.
CREATE TABLE notifications (
    notification_id     TEXT PRIMARY KEY,
    scope               TEXT NOT NULL,
    resource_id         TEXT NOT NULL,
    channel             TEXT NOT NULL,
    status              TEXT NOT NULL,          -- SENT | WITHDRAWN (mutable, compensable)
    created_from_action TEXT NOT NULL,
    delivered_at        TEXT NOT NULL           -- immutable delivery observation (residue B)
);
