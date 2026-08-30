// Package verification evaluates provider-defined semantic postconditions,
// independently from reversal invocation success (Master Prompt §46, §47;
// invariant I-10). Verification MUST NOT create business side effects, and a
// failed verification call yields UNKNOWN — never a false reversal failure.
//
// This package is ROP Core: it MUST NOT import any HTTP package (I-17).
package verification

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/brilliantkid87/rop/internal/action"
	"github.com/brilliantkid87/rop/internal/authz"
	"github.com/brilliantkid87/rop/internal/clock"
	"github.com/brilliantkid87/rop/internal/operation"
	"github.com/brilliantkid87/rop/internal/roperr"
	"github.com/brilliantkid87/rop/internal/store"
	"github.com/brilliantkid87/rop/pkg/rop"
)

// Service runs and records verification results.
type Service struct {
	Store      *store.Store
	Clock      clock.Clock
	Registry   *operation.Registry
	Authorizer authz.Authorizer
}

// Verify evaluates the Action's provider-defined postconditions and records
// the result. It is read-only with respect to business state.
func (s *Service) Verify(ctx context.Context, p authz.Principal, scope, actionID string) (rop.VerificationResult, error) {
	now := s.Clock.Now()
	if !s.Authorizer.Can(p, authz.VerbVerify, scope) {
		return rop.VerificationResult{}, roperr.New(rop.ProblemAuthorizationDenied, "principal %s may not verify in scope %s", p.ID, scope)
	}
	a, ok, err := s.Store.GetAction(ctx, s.Store.DB(), scope, actionID)
	if err != nil {
		return rop.VerificationResult{}, err
	}
	if !ok {
		return rop.VerificationResult{}, roperr.New(rop.ProblemActionNotFound, "no action %s in scope %s", actionID, scope)
	}
	if rop.Reversibility(a.Reversibility) == rop.ReversibilityIRREVERSIBLE || a.Status == action.Irreversible {
		return rop.VerificationResult{}, roperr.New(rop.ProblemIrreversible, "action %s (operation %s) is IRREVERSIBLE; no postconditions are defined", actionID, a.OperationID)
	}

	op, ok := s.Registry.Get(a.OperationID)
	if !ok {
		return rop.VerificationResult{}, fmt.Errorf("verification: operation %s not registered (data inconsistency)", a.OperationID)
	}
	if op.VerifyFunc == nil {
		return rop.VerificationResult{}, roperr.New(rop.ProblemCapabilityUnavailable, "operation %s does not define verification postconditions", a.OperationID)
	}

	material, _, err := s.Store.GetMaterial(ctx, s.Store.DB(), scope, actionID)
	if err != nil {
		return rop.VerificationResult{}, err
	}
	out, err := op.VerifyFunc(ctx, operation.VerifyInput{Action: a, Material: material, Now: now})
	if err != nil {
		// Verification's own calls failed (transport, provider lookup): the
		// honest result is UNKNOWN, never reversal failure (Master Prompt §47).
		result := rop.VerificationResult{
			ActionID:       actionID,
			Status:         rop.VerificationUNKNOWN,
			Semantics:      opSemantics(op),
			Postconditions: []rop.Postcondition{},
			EvaluatedAt:    now,
			Detail:         fmt.Sprintf("verification could not be evaluated: %v", err),
		}
		if recErr := s.record(ctx, actionID, result); recErr != nil {
			return rop.VerificationResult{}, recErr
		}
		return result, nil
	}

	result := rop.VerificationResult{
		ActionID:       actionID,
		Status:         out.Status,
		Semantics:      out.Semantics,
		Postconditions: out.Postconditions,
		EvaluatedAt:    now,
		Detail:         out.Detail,
	}
	if result.Postconditions == nil {
		result.Postconditions = []rop.Postcondition{}
	}
	if err := s.record(ctx, actionID, result); err != nil {
		return rop.VerificationResult{}, err
	}
	// Verification failure is distinct evidence: it may conclude an unknown
	// outcome, but it never manufactures one. M1 records results; state-level
	// reconciliation consumption of verification evidence arrives in M4.
	return result, nil
}

func (s *Service) record(ctx context.Context, actionID string, r rop.VerificationResult) error {
	pc, err := json.Marshal(r.Postconditions)
	if err != nil {
		return fmt.Errorf("verification: marshal postconditions: %w", err)
	}
	if err := s.Store.RecordVerification(ctx, s.Store.DB(), actionID, string(r.Status), string(r.Semantics), string(pc), r.EvaluatedAt); err != nil {
		return err
	}
	return nil
}

func opSemantics(op operation.Operation) rop.VerificationSemantics {
	// The declared semantics class travels with the operation definition; a
	// failed evaluation still reports how the verification would have run
	// (Master Prompt §47).
	if op.Reversibility == rop.ReversibilityREVERSIBLE {
		return rop.SemanticsLOCAL_READONLY
	}
	return rop.SemanticsEVENTUALLY_CONSISTENT
}
