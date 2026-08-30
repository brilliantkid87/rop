// Package planner produces read-only reversal plans (Master Prompt §39, §40).
// Planning MUST NOT cause external side effects (invariant I-3); a plan is a
// snapshot of knowledge, never a durable authorization to reverse later
// (invariant I-19). Plans are response bodies only, not resources (OQ-7).
//
// This package is ROP Core: it MUST NOT import any HTTP package (I-17).
package planner

import (
	"context"
	"fmt"

	"github.com/brilliantkid87/rop/internal/action"
	"github.com/brilliantkid87/rop/internal/authz"
	"github.com/brilliantkid87/rop/internal/clock"
	"github.com/brilliantkid87/rop/internal/dependency"
	"github.com/brilliantkid87/rop/internal/operation"
	"github.com/brilliantkid87/rop/internal/roperr"
	"github.com/brilliantkid87/rop/internal/store"
	"github.com/brilliantkid87/rop/pkg/rop"
)

// Service builds reversal plans from durable state plus provider plan funcs.
type Service struct {
	Store        *store.Store
	Clock        clock.Clock
	Registry     *operation.Registry
	Authorizer   authz.Authorizer
	Dependencies *dependency.Service // nil = no dependency reporting
}

// Plan returns a read-only reversal plan for one Action.
func (s *Service) Plan(ctx context.Context, p authz.Principal, scope, actionID string) (rop.Plan, error) {
	now := s.Clock.Now()
	if !s.Authorizer.Can(p, authz.VerbPlan, scope) {
		return rop.Plan{}, roperr.New(rop.ProblemAuthorizationDenied, "principal %s may not plan in scope %s", p.ID, scope)
	}
	if _, err := s.Store.SweepExpiry(ctx, s.Store.DB(), now); err != nil {
		return rop.Plan{}, err
	}
	a, ok, err := s.Store.GetAction(ctx, s.Store.DB(), scope, actionID)
	if err != nil {
		return rop.Plan{}, err
	}
	if !ok {
		return rop.Plan{}, roperr.New(rop.ProblemActionNotFound, "no action %s in scope %s", actionID, scope)
	}
	if rop.Reversibility(a.Reversibility) == rop.ReversibilityIRREVERSIBLE || a.Status == action.Irreversible {
		return rop.Plan{}, roperr.New(rop.ProblemIrreversible, "action %s (operation %s) is IRREVERSIBLE", actionID, a.OperationID)
	}

	plan := rop.Plan{
		ActionID:           a.ActionID,
		GeneratedAt:        now,
		CurrentStatus:      rop.Status(a.Status),
		Reversibility:      rop.Reversibility(a.Reversibility),
		Guarantee:          rop.Guarantee(a.Guarantee),
		ExpiresAt:          a.ExpiresAt,
		Preconditions:      []string{},
		Conflicts:          []string{},
		ManualRequirements: []string{},
	}
	if a.Status != action.Applied {
		plan.Conflicts = append(plan.Conflicts,
			fmt.Sprintf("action is %s, not APPLIED; a plan for this state cannot authorize reversal", a.Status))
	}

	// Blocking dependencies are exposed to planning (M5) — the plan is an
	// advisory snapshot; execution revalidates them independently (I-19).
	if s.Dependencies != nil {
		blocking, err := s.Dependencies.Blocking(ctx, scope, actionID)
		if err != nil {
			return rop.Plan{}, err
		}
		plan.BlockingDependencies = blocking
		if len(blocking) > 0 {
			plan.Conflicts = append(plan.Conflicts,
				fmt.Sprintf("%d active dependent Action(s) currently make reversal unsafe", len(blocking)))
		}
	}
	// Residue declared before reversal (append-style history) is merged into
	// the plan; residue discovered later is NOT required to be known here.
	stored, err := s.Store.ResidueForAction(ctx, s.Store.DB(), actionID)
	if err != nil {
		return rop.Plan{}, err
	}
	for _, rec := range stored {
		if rec.Source == store.ResidueDeclared {
			plan.Residue = append(plan.Residue, rec.Residue...)
		}
	}

	op, ok := s.Registry.Get(a.OperationID)
	if !ok {
		return rop.Plan{}, fmt.Errorf("planner: operation %s not registered (data inconsistency)", a.OperationID)
	}
	if op.PlanFunc != nil {
		material, _, err := s.Store.GetMaterial(ctx, s.Store.DB(), scope, actionID)
		if err != nil {
			return rop.Plan{}, err
		}
		out, err := op.PlanFunc(ctx, operation.PlanInput{Action: a, Material: material, Now: now})
		if err != nil {
			// A planning failure is a read failure; it must not mutate state.
			return rop.Plan{}, fmt.Errorf("planner: provider plan func: %w", err)
		}
		plan.Preconditions = append(plan.Preconditions, out.Preconditions...)
		plan.Conflicts = append(plan.Conflicts, out.Conflicts...)
		plan.ManualRequirements = append(plan.ManualRequirements, out.ManualRequirements...)
		plan.ExpectedReversal = out.ExpectedReversal
		plan.Residue = out.Residue
		plan.BasisResourceVersion = out.BasisResourceVersion
		plan.ValidUntil = out.ValidUntil
	}
	return plan, nil
}
