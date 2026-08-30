// Package dependency implements Action-to-Action dependencies as durable
// safety constraints (Master Prompt §49; M5): "Action B depends on Action A"
// means reversal of A may be unsafe while B's effect still stands.
//
// Dependencies are safety constraints, NOT workflow ownership: ROP records
// them, exposes them to planning, and refuses unsafe reversal — it never
// executes dependent reversals automatically, orders work, or schedules
// anything.
//
// This package is ROP Core: it MUST NOT import any HTTP package (I-17).
package dependency

import (
	"context"
	"fmt"
	"time"

	"github.com/brilliantkid87/rop/internal/action"
	"github.com/brilliantkid87/rop/internal/roperr"
	"github.com/brilliantkid87/rop/internal/store"
	"github.com/brilliantkid87/rop/pkg/rop"
)

// ResolvedStatuses is the documented reference rule for what counts as an
// "active dependent Action" (M5 review requirement: not "dependent exists ⇒
// block forever"). A dependent B stops blocking its parent A once B's own
// effect has been compensated:
//
//	resolved     = { REVERSED, PARTIALLY_REVERSED }
//	active       = every other status (APPLIED, REVERSING,
//	               OUTCOME_UNKNOWN, REVERSE_FAILED, EXPIRED, IRREVERSIBLE)
//
// Rationale: REVERSED and PARTIALLY_REVERSED mean B's provider-defined
// compensation ran (wholly or with declared residue), so the safety concern
// that motivated the edge is addressed. States like REVERSE_FAILED, EXPIRED,
// and IRREVERSIBLE leave B's effect standing and keep blocking. The rule is
// deliberately provider-overridable in the future (open question OQ-15); it
// is one documented decision, not an accidental hard-code.
var ResolvedStatuses = map[string]bool{
	action.Reversed:          true,
	action.PartiallyReversed: true,
}

// Service records and evaluates dependency edges.
type Service struct {
	Store *store.Store
}

// Add records one edge: dependentActionID depends on parentActionID. It is
// idempotent (a duplicate edge is the same fact, not an error), rejects
// self-dependency and dependency cycles at the domain layer (with a durable
// UNIQUE constraint as backstop), and is scope-safe: both Actions must exist
// in the same scope.
func (s *Service) Add(ctx context.Context, scope, parentActionID, dependentActionID string) error {
	if parentActionID == dependentActionID {
		return roperr.New(rop.ProblemDependencyExists, "an Action cannot depend on itself (%s)", parentActionID)
	}
	if _, ok, err := s.Store.GetAction(ctx, s.Store.DB(), scope, parentActionID); err != nil {
		return err
	} else if !ok {
		return roperr.New(rop.ProblemActionNotFound, "no action %s in scope %s", parentActionID, scope)
	}
	if _, ok, err := s.Store.GetAction(ctx, s.Store.DB(), scope, dependentActionID); err != nil {
		return err
	} else if !ok {
		return roperr.New(rop.ProblemActionNotFound, "no action %s in scope %s", dependentActionID, scope)
	}
	// Cycle check: the new edge is dependent -> parent ("B depends on A").
	// It creates a cycle iff the parent already depends, transitively, on
	// the dependent. The graph is small in v0.1; a bounded DFS is enough and
	// avoids building a generic graph engine.
	if cycle, err := s.reachable(ctx, s.Store.DB(), scope, parentActionID, dependentActionID); err != nil {
		return err
	} else if cycle {
		return roperr.New(rop.ProblemDependencyExists,
			"dependency %s -> %s would create a cycle; a graph that makes safe reversal reasoning impossible is rejected", dependentActionID, parentActionID)
	}
	return s.Store.AddDependency(ctx, s.Store.DB(), scope, parentActionID, dependentActionID, time.Now().UTC())
}

// reachable reports whether parent can reach target by following
// "depends on" edges (dependent -> parent).
func (s *Service) reachable(ctx context.Context, q store.DBTX, scope, from, target string) (bool, error) {
	visited := map[string]bool{from: true}
	stack := []string{from}
	const maxDepth = 64 // cycles are rejected at insert; bound the walk anyway
	for depth := 0; len(stack) > 0 && depth <= maxDepth; depth++ {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		edges, err := s.Store.DependenciesOfDependent(ctx, q, scope, node)
		if err != nil {
			return false, err
		}
		for _, e := range edges {
			if e.ParentActionID == target {
				return true, nil
			}
			if !visited[e.ParentActionID] {
				visited[e.ParentActionID] = true
				stack = append(stack, e.ParentActionID)
			}
		}
	}
	return false, nil
}

// Blocking returns the active dependent Action IDs of parentActionID: those
// whose recorded effect still stands per ResolvedStatuses.
func (s *Service) Blocking(ctx context.Context, scope, parentActionID string) ([]string, error) {
	edges, err := s.Store.DependenciesOfParent(ctx, s.Store.DB(), scope, parentActionID)
	if err != nil {
		return nil, err
	}
	var blocking []string
	for _, e := range edges {
		a, ok, err := s.Store.GetAction(ctx, s.Store.DB(), scope, e.DependentActionID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("dependency: dependent %s missing in scope %s (data inconsistency)", e.DependentActionID, scope)
		}
		if !ResolvedStatuses[a.Status] {
			blocking = append(blocking, e.DependentActionID)
		}
	}
	return blocking, nil
}
