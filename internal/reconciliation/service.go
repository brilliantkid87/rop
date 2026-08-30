// Package reconciliation implements reconciliation as a first-class internal
// domain operation (Master Prompt §38; M4). It resolves OUTCOME_UNKNOWN
// attempts through read-only provider lookups on the durable execution
// identity — never by re-invoking the reversal side effect. State transitions
// happen only on provider evidence the adapter's contract marks as proven;
// insufficient evidence preserves uncertainty. There is deliberately no
// public HTTP surface for reconciliation in v0.1 (OQ-1).
//
// This package is ROP Core: it MUST NOT import any HTTP package (I-17).
package reconciliation

import (
	"context"
	"fmt"

	"github.com/brilliantkid87/rop/internal/action"
	"github.com/brilliantkid87/rop/internal/authz"
	"github.com/brilliantkid87/rop/internal/clock"
	"github.com/brilliantkid87/rop/internal/operation"
	"github.com/brilliantkid87/rop/internal/reversal"
	"github.com/brilliantkid87/rop/internal/roperr"
	"github.com/brilliantkid87/rop/internal/store"
	"github.com/brilliantkid87/rop/pkg/rop"
)

// Service reconciles uncertain Reversal Attempts.
type Service struct {
	Store      *store.Store
	Clock      clock.Clock
	Registry   *operation.Registry
	Authorizer authz.Authorizer
}

// Reconcile performs one reconciliation round for the Action's uncertain
// attempt. It is idempotent: repeated rounds append observations and change
// state only when proven evidence arrives; once the attempt is concluded,
// further calls replay the recorded result without new work.
func (s *Service) Reconcile(ctx context.Context, p authz.Principal, scope, actionID string) (rop.ReversalResult, error) {
	now := s.Clock.Now()
	if !s.Authorizer.Can(p, authz.VerbReconcile, scope) {
		return rop.ReversalResult{}, roperr.New(rop.ProblemAuthorizationDenied, "principal %s may not reconcile in scope %s", p.ID, scope)
	}
	a, ok, err := s.Store.GetAction(ctx, s.Store.DB(), scope, actionID)
	if err != nil {
		return rop.ReversalResult{}, err
	}
	if !ok {
		return rop.ReversalResult{}, roperr.New(rop.ProblemActionNotFound, "no action %s in scope %s", actionID, scope)
	}

	attempt, ok, err := s.Store.GetOpenAttempt(ctx, s.Store.DB(), actionID)
	if err != nil {
		return rop.ReversalResult{}, err
	}
	if !ok || attempt.ExecutionState != store.AttemptAwaitingReconciliation {
		// Nothing uncertain to reconcile. Idempotent replay: return the most
		// recent attempt's recorded projection without new observations.
		latest, found, err := s.Store.GetLatestAttempt(ctx, s.Store.DB(), actionID)
		if err != nil {
			return rop.ReversalResult{}, err
		}
		if !found {
			return rop.ReversalResult{}, roperr.New(rop.ProblemPreconditionFailed,
				"action %s has no reversal attempts to reconcile", actionID)
		}
		return reversal.ReconstructResult(latest, a), nil
	}
	if a.Status != action.OutcomeUnknown {
		return rop.ReversalResult{}, fmt.Errorf("reconciliation: open attempt %s but action is %s (data inconsistency)", attempt.AttemptID, a.Status)
	}

	op, ok := s.Registry.Get(a.OperationID)
	if !ok {
		return rop.ReversalResult{}, fmt.Errorf("reconciliation: operation %s not registered (data inconsistency)", a.OperationID)
	}
	if op.ReconcileFunc == nil {
		return rop.ReversalResult{}, roperr.New(rop.ProblemCapabilityUnavailable,
			"operation %s does not support reconciliation lookups; the attempt remains unknown", a.OperationID)
	}

	material, _, err := s.Store.GetMaterial(ctx, s.Store.DB(), scope, actionID)
	if err != nil {
		return rop.ReversalResult{}, err
	}
	ref := ""
	if attempt.ProviderRef != nil {
		ref = *attempt.ProviderRef
	}
	out, err := op.ReconcileFunc(ctx, operation.ReconcileInput{
		Action: a, Material: material, Now: now, ProviderRef: ref,
	})

	evidence := store.EvidenceInconclusive
	detail := "reconciliation lookup failed"
	if err != nil {
		detail = "reconciliation lookup failed: " + err.Error()
	} else {
		detail = out.Detail
		switch {
		case out.Outcome == rop.OutcomeREVERSED && out.Proven:
			evidence = store.EvidenceProvenReversed
		case out.Outcome == rop.OutcomeREVERSE_FAILED && out.Proven:
			evidence = store.EvidenceProvenNotReversed
		default:
			// Unproven evidence (e.g. a negative lookup under a contract that
			// does not guarantee that absence proves non-execution) stays
			// inconclusive: uncertainty is preserved, never resolved by
			// guessing (Master Prompt §34, §38).
			if detail == "" {
				detail = "provider evidence not proven; uncertainty preserved"
			}
		}
	}
	// Durable, append-only observation — recorded before any transition so
	// the evidence chain survives a crash mid-reconciliation.
	if err := s.Store.RecordObservation(ctx, s.Store.DB(), store.ObservationRow{
		AttemptID: attempt.AttemptID, ObservedAt: now, Evidence: evidence, Detail: detail,
	}); err != nil {
		return rop.ReversalResult{}, err
	}

	switch evidence {
	case store.EvidenceProvenReversed:
		concludedAt := s.Clock.Now()
		if err := s.Store.ConcludeAttempt(ctx, s.Store.DB(), attempt.AttemptID,
			store.ObservedReversed, nil, nil, concludedAt); err != nil {
			return rop.ReversalResult{}, err
		}
		if err := s.Store.UpdateStatus(ctx, s.Store.DB(), scope, actionID,
			action.OutcomeUnknown, action.Reversed,
			"reconciliation: provider evidence proves reversal (attempt "+attempt.AttemptID+")", concludedAt); err != nil {
			return rop.ReversalResult{}, err
		}
	case store.EvidenceProvenNotReversed:
		concludedAt := s.Clock.Now()
		if err := s.Store.ConcludeAttempt(ctx, s.Store.DB(), attempt.AttemptID,
			store.ObservedReverseFailed, nil, nil, concludedAt); err != nil {
			return rop.ReversalResult{}, err
		}
		if err := s.Store.UpdateStatus(ctx, s.Store.DB(), scope, actionID,
			action.OutcomeUnknown, action.ReverseFailed,
			"reconciliation: provider evidence proves the reversal did not occur (attempt "+attempt.AttemptID+")", concludedAt); err != nil {
			return rop.ReversalResult{}, err
		}
	default:
		// Insufficient evidence: the attempt remains
		// AWAITING_RECONCILIATION and the Action OUTCOME_UNKNOWN.
	}

	updated, _, err := s.Store.GetAttempt(ctx, s.Store.DB(), attempt.AttemptID)
	if err != nil {
		return rop.ReversalResult{}, err
	}
	fresh, ok, err := s.Store.GetAction(ctx, s.Store.DB(), scope, actionID)
	if err != nil || !ok {
		return rop.ReversalResult{}, fmt.Errorf("reconciliation: action vanished during reconcile: ok=%v err=%v", ok, err)
	}
	return reversal.ReconstructResult(updated, fresh), nil
}
