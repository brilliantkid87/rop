package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/brilliantkid87/rop/pkg/rop"
)

// DependencyRow is one durable Action-to-Action dependency edge: the
// dependent Action's effect makes reversal of the parent Action potentially
// unsafe (Master Prompt §49). Safety constraint only — never a workflow edge
// that ROP would traverse for execution.
type DependencyRow struct {
	Seq               int64
	Scope             string
	ParentActionID    string
	DependentActionID string
	CreatedAt         time.Time
}

// AddDependency records one edge idempotently (UNIQUE(parent, dependent)).
// Cycle/self/duplicate policy lives in the domain layer (internal/dependency);
// the unique index is the durable backstop.
func (s *Store) AddDependency(ctx context.Context, q DBTX, scope, parentActionID, dependentActionID string, at time.Time) error {
	_, err := q.ExecContext(ctx, `INSERT INTO action_dependencies
		(scope, parent_action_id, dependent_action_id, created_at)
		VALUES (?, ?, ?, ?)`,
		scope, parentActionID, dependentActionID, at.UTC().Format(timeFormat))
	if err != nil {
		if isUniqueConstraintErr(err) {
			return nil // duplicate edge = same fact; safe no-op
		}
		return fmt.Errorf("store: add dependency: %w", err)
	}
	return nil
}

// DependenciesOfParent returns edges where parentActionID is the parent
// (i.e. Actions that depend on it), scope-filtered (invariant I-13).
func (s *Store) DependenciesOfParent(ctx context.Context, q DBTX, scope, parentActionID string) ([]DependencyRow, error) {
	return s.queryDependencies(ctx, q,
		`SELECT seq, scope, parent_action_id, dependent_action_id, created_at
		 FROM action_dependencies WHERE scope = ? AND parent_action_id = ?`, scope, parentActionID)
}

// DependenciesOfDependent returns edges where dependentActionID is the
// dependent (i.e. Actions it depends on), scope-filtered. Used for cycle
// detection walks.
func (s *Store) DependenciesOfDependent(ctx context.Context, q DBTX, scope, dependentActionID string) ([]DependencyRow, error) {
	return s.queryDependencies(ctx, q,
		`SELECT seq, scope, parent_action_id, dependent_action_id, created_at
		 FROM action_dependencies WHERE scope = ? AND dependent_action_id = ?`, scope, dependentActionID)
}

func (s *Store) queryDependencies(ctx context.Context, q DBTX, query, scope, id string) ([]DependencyRow, error) {
	rows, err := q.QueryContext(ctx, query, scope, id)
	if err != nil {
		return nil, fmt.Errorf("store: query dependencies: %w", err)
	}
	defer rows.Close()
	var out []DependencyRow
	for rows.Next() {
		var d DependencyRow
		var created string
		if err := rows.Scan(&d.Seq, &d.Scope, &d.ParentActionID, &d.DependentActionID, &created); err != nil {
			return nil, fmt.Errorf("store: scan dependency: %w", err)
		}
		if d.CreatedAt, err = time.Parse(timeFormat, created); err != nil {
			return nil, fmt.Errorf("store: parse dependency created_at: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Residue sources (CHECK-constrained in the schema): DECLARED = known before
// reversal (exposed to planning); DISCOVERED = recorded during reversal
// execution; VERIFIED = confirmed during verification. Append-style history:
// discovery after declaration adds a record, it never overwrites.
const (
	ResidueDeclared   = "DECLARED"
	ResidueDiscovered = "DISCOVERED"
	ResidueVerified   = "VERIFIED"
)

type ResidueRecord struct {
	Seq        int64
	ActionID   string
	Source     string
	Residue    []rop.Residue
	RecordedAt time.Time
}

// RecordResidue appends one residue record. Residue is provider-declared
// evidence of what remains; it is NOT evidence that reversal failed.
func (s *Store) RecordResidue(ctx context.Context, q DBTX, actionID, source string, items []rop.Residue, at time.Time) error {
	if len(items) == 0 {
		return nil
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("store: marshal residue: %w", err)
	}
	_, err = q.ExecContext(ctx, `INSERT INTO action_residue
		(action_id, source, residue_json, recorded_at) VALUES (?, ?, ?, ?)`,
		actionID, source, raw, at.UTC().Format(timeFormat))
	if err != nil {
		return fmt.Errorf("store: record residue: %w", err)
	}
	return nil
}

// ResidueForAction returns the append-style residue history of an Action,
// oldest first.
func (s *Store) ResidueForAction(ctx context.Context, q DBTX, actionID string) ([]ResidueRecord, error) {
	rows, err := q.QueryContext(ctx, `SELECT seq, action_id, source, residue_json, recorded_at
		FROM action_residue WHERE action_id = ? ORDER BY seq`, actionID)
	if err != nil {
		return nil, fmt.Errorf("store: residue for action: %w", err)
	}
	defer rows.Close()
	var out []ResidueRecord
	for rows.Next() {
		var r ResidueRecord
		var raw, recorded string
		if err := rows.Scan(&r.Seq, &r.ActionID, &r.Source, &raw, &recorded); err != nil {
			return nil, fmt.Errorf("store: scan residue: %w", err)
		}
		if err := json.Unmarshal([]byte(raw), &r.Residue); err != nil {
			return nil, fmt.Errorf("store: unmarshal residue: %w", err)
		}
		if r.RecordedAt, err = time.Parse(timeFormat, recorded); err != nil {
			return nil, fmt.Errorf("store: parse residue recorded_at: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
