// Package reversal implements reversal invocation as an optional capability:
// eligibility-gated, conflict-refusing, and honest about unknown outcomes
// (Master Prompt §28, §34, §41, §48). The reference implementation executes
// synchronously; OUTCOME_UNKNOWN handling stores an open attempt awaiting
// reconciliation (full recovery semantics arrive in Milestone 4).
//
// This package is ROP Core: it MUST NOT import any HTTP package (I-17).
package reversal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/brilliantkid87/rop/internal/action"
	"github.com/brilliantkid87/rop/internal/authz"
	"github.com/brilliantkid87/rop/internal/clock"
	"github.com/brilliantkid87/rop/internal/dependency"
	"github.com/brilliantkid87/rop/internal/operation"
	"github.com/brilliantkid87/rop/internal/roperr"
	"github.com/brilliantkid87/rop/internal/store"
	"github.com/brilliantkid87/rop/pkg/rop"
)

// Service executes reversal requests for eligible Actions.
type Service struct {
	Store        *store.Store
	Clock        clock.Clock
	Registry     *operation.Registry
	Authorizer   authz.Authorizer
	Dependencies *dependency.Service // nil = no dependency checking (not recommended)
}

// hashKey hashes a raw client idempotency key. Raw keys are never persisted
// (data minimization, Master Prompt §14).
func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// fingerprint captures the request semantics an idempotency key was first
// used for. A replay with the same key but different semantics (a different
// Action within the same scope) is rejected, never silently re-executed.
func fingerprint(scope, actionID string) string {
	sum := sha256.Sum256([]byte("reverse\x00" + scope + "\x00" + actionID))
	return hex.EncodeToString(sum[:])
}

// Reverse requests reversal of one Action (one Action per request; no batch,
// no coordination — Master Prompt §29, §30). idemKey may be empty: when
// present, it must map durably to one attempt per (scope, key) — replays
// return the recorded result instead of executing again (Master Prompt §36).
// ROP request idempotency is not exactly-once provider execution (§35).
func (s *Service) Reverse(ctx context.Context, p authz.Principal, scope, actionID, idemKey string) (rop.ReversalResult, error) {
	now := s.Clock.Now()
	if !s.Authorizer.Can(p, authz.VerbReverse, scope) {
		return rop.ReversalResult{}, roperr.New(rop.ProblemAuthorizationDenied, "principal %s may not reverse in scope %s", p.ID, scope)
	}
	if _, err := s.Store.SweepExpiry(ctx, s.Store.DB(), now); err != nil {
		return rop.ReversalResult{}, err
	}

	var keyHash, fp string
	if idemKey != "" {
		keyHash, fp = hashKey(idemKey), fingerprint(scope, actionID)
		rec, found, err := s.Store.GetIdempotency(ctx, s.Store.DB(), scope, keyHash)
		if err != nil {
			return rop.ReversalResult{}, err
		}
		if found {
			// Durable replay: return (or reconstruct) the recorded result —
			// never a second execution. A lost HTTP response retried with the
			// same key lands here (Master Prompt §36).
			if rec.Fingerprint != fp {
				return rop.ReversalResult{}, roperr.New(rop.ProblemIdempotencyConflict,
					"idempotency key was already used for a different Action in this scope")
			}
			return s.replay(ctx, scope, rec)
		}
	}

	a, ok, err := s.Store.GetAction(ctx, s.Store.DB(), scope, actionID)
	if err != nil {
		return rop.ReversalResult{}, err
	}
	if !ok {
		return rop.ReversalResult{}, roperr.New(rop.ProblemActionNotFound, "no action %s in scope %s", actionID, scope)
	}
	switch {
	case rop.Reversibility(a.Reversibility) == rop.ReversibilityIRREVERSIBLE || a.Status == action.Irreversible:
		return rop.ReversalResult{}, roperr.New(rop.ProblemIrreversible, "action %s (operation %s) is IRREVERSIBLE", actionID, a.OperationID)
	case a.Status == action.Expired:
		return rop.ReversalResult{}, roperr.New(rop.ProblemReversalExpired, "reversal window for action %s expired at %s", actionID, fmtTime(a.ExpiresAt))
	case a.Status == action.Reversing || a.Status == action.OutcomeUnknown:
		return rop.ReversalResult{}, roperr.New(rop.ProblemAlreadyInProgress, "action %s already has a non-concluded reversal attempt", actionID)
	case a.Status != action.Applied:
		return rop.ReversalResult{}, roperr.New(rop.ProblemPreconditionFailed, "action %s is %s; only APPLIED actions can begin reversal", actionID, a.Status)
	}

	// Dependency re-check happens here at execution time (M5), independent of
	// any plan: a dependency created after planning still blocks (invariant
	// I-19). ROP refuses unsafe reversal; it never executes dependent
	// reversals automatically.
	if s.Dependencies != nil {
		blocking, err := s.Dependencies.Blocking(ctx, scope, actionID)
		if err != nil {
			return rop.ReversalResult{}, err
		}
		if len(blocking) > 0 {
			return rop.ReversalResult{}, roperr.New(rop.ProblemDependencyExists,
				"action %s has active dependent Actions (%v); reversal is unsafe until they are resolved", actionID, blocking)
		}
	}

	op, ok := s.Registry.Get(a.OperationID)
	if !ok {
		return rop.ReversalResult{}, fmt.Errorf("reversal: operation %s not registered (data inconsistency)", a.OperationID)
	}
	if op.ReverseFunc == nil {
		// The provider has no ROP reversal implementation; its reversal (if
		// any) lives in its own API (capability-model.md §3).
		return rop.ReversalResult{}, roperr.New(rop.ProblemCapabilityUnavailable, "operation %s does not support reversal through ROP", a.OperationID)
	}

	// Transaction A: attempt (RUNNING) + Action→REVERSING + idempotency
	// registration, durably, before any provider call (architecture §9).
	// The (scope, key_hash) unique index and the one-open-attempt index make
	// concurrent duplicates converge at the database level.
	tx, err := s.Store.BeginTx(ctx)
	if err != nil {
		return rop.ReversalResult{}, err
	}
	attemptID := store.NewID("ra")
	// Durable, stable provider execution identity (M4): assigned before any
	// provider call so reconciliation can ask "what happened to provider
	// operation X?" without replaying the side effect. Distinct from the HTTP
	// Idempotency-Key, which protects ROP request handling only.
	providerRef := "rop-rev-" + attemptID
	if err := s.Store.CreateAttempt(ctx, tx, store.AttemptRow{
		AttemptID:      attemptID,
		ActionID:       actionID,
		RequestedAt:    now,
		ExecutionState: store.AttemptRunning,
		ProviderRef:    &providerRef,
	}); err != nil {
		_ = tx.Rollback()
		if err == store.ErrAttemptInProgress {
			if idemKey != "" {
				if res, handled, err := s.replayOrConflict(ctx, scope, actionID, keyHash, fp); handled {
					return res, err
				}
			}
			return rop.ReversalResult{}, roperr.New(rop.ProblemAlreadyInProgress, "action %s already has a non-concluded reversal attempt", actionID)
		}
		return rop.ReversalResult{}, err
	}
	if err := s.Store.UpdateStatus(ctx, tx, scope, actionID, action.Applied, action.Reversing, "reversal attempt "+attemptID, now); err != nil {
		_ = tx.Rollback()
		return rop.ReversalResult{}, err
	}
	if idemKey != "" {
		if err := s.Store.CreateIdempotency(ctx, tx, store.IdempotencyRow{
			Scope: scope, ActionID: actionID, KeyHash: keyHash,
			Fingerprint: fp, AttemptID: attemptID, CreatedAt: now,
		}); err != nil {
			_ = tx.Rollback()
			if err == store.ErrIdempotencyKeyExists {
				// A concurrent request with the same key registered first:
				// converge on its record and result.
				if res, handled, err := s.replayOrConflict(ctx, scope, actionID, keyHash, fp); handled {
					return res, err
				}
			}
			return rop.ReversalResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return rop.ReversalResult{}, fmt.Errorf("reversal: commit attempt: %w", err)
	}

	result, svcErr := s.execute(ctx, op, a, attemptID, providerRef, now)
	return result, svcErr
}

// replayOrConflict handles a lost insert race: another request with the same
// key registered first. It replays if the fingerprint matches and rejects the
// request if the key was used with different semantics. handled=false means
// no idempotency record appeared (fall back to plain already-in-progress).
func (s *Service) replayOrConflict(ctx context.Context, scope, actionID, keyHash, fp string) (rop.ReversalResult, bool, error) {
	rec, found, err := s.Store.GetIdempotency(ctx, s.Store.DB(), scope, keyHash)
	if err != nil || !found {
		return rop.ReversalResult{}, false, err
	}
	if rec.Fingerprint != fp {
		return rop.ReversalResult{}, true, roperr.New(rop.ProblemIdempotencyConflict,
			"idempotency key was already used for a different Action in this scope")
	}
	res, err := s.replay(ctx, scope, rec)
	return res, true, err
}

// replay reconstructs the recorded result of an attempt from durable state.
// It waits briefly for a concurrently-executing attempt to conclude; an
// attempt parked AWAITING_RECONCILIATION (outcome unobserved) replays as
// OUTCOME_UNKNOWN — never re-executed (invariant I-5).
func (s *Service) replay(ctx context.Context, scope string, rec store.IdempotencyRow) (rop.ReversalResult, error) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		attempt, ok, err := s.Store.GetAttempt(ctx, s.Store.DB(), rec.AttemptID)
		if err != nil {
			return rop.ReversalResult{}, err
		}
		if !ok {
			return rop.ReversalResult{}, fmt.Errorf("reversal: idempotency record references missing attempt %s (data inconsistency)", rec.AttemptID)
		}
		if attempt.ExecutionState == store.AttemptRunning && time.Now().Before(deadline) {
			time.Sleep(25 * time.Millisecond)
			continue
		}
		a, ok, err := s.Store.GetAction(ctx, s.Store.DB(), scope, rec.ActionID)
		if err != nil {
			return rop.ReversalResult{}, err
		}
		if !ok {
			return rop.ReversalResult{}, fmt.Errorf("reversal: idempotency record references missing action %s (data inconsistency)", rec.ActionID)
		}
		return ReconstructResult(attempt, a), nil
	}
}

// execute performs the provider call and records the outcome. Every exit path
// concludes (or explicitly parks) the attempt so no attempt is left RUNNING.
// Failure classification (Master Prompt §37) drives behavior: retriable
// pre-execution failures leave the Action unchanged and re-requestable;
// definite rejections conclude REVERSE_FAILED; anything unobservable parks
// the attempt for reconciliation — never reported as failure (invariant I-5).
func (s *Service) execute(ctx context.Context, op operation.Operation, a store.ActionRow, attemptID, providerRef string, now time.Time) (rop.ReversalResult, error) {
	material, _, err := s.Store.GetMaterial(ctx, s.Store.DB(), a.Scope, a.ActionID)
	if err != nil {
		return rop.ReversalResult{}, err
	}
	out, perr := op.ReverseFunc(ctx, operation.ReverseInput{
		Action: a, Material: material, Now: now, ProviderRef: providerRef,
	})
	concludedAt := s.Clock.Now()

	conclude := func(observed string, errMsg, class *string) error {
		return s.Store.ConcludeAttempt(ctx, s.Store.DB(), attemptID, observed, errMsg, class, concludedAt)
	}
	transition := func(from, to, note string) error {
		return s.Store.UpdateStatus(ctx, s.Store.DB(), a.Scope, a.ActionID, from, to, note, concludedAt)
	}

	if perr != nil {
		// A classified provider failure follows its semantics; an
		// unclassified error is treated as unobservable (RECONCILE_REQUIRED):
		// transport failure is never evidence of semantic failure (§34).
		class := operation.RetryReconcileRequired
		var pf *operation.ProviderFailure
		if errors.As(perr, &pf) {
			class = pf.Class
		}
		msg := perr.Error()
		switch class {
		case operation.RetryRetriable:
			// Known to have failed before any provider-side effect: the
			// business state is unchanged and a new reversal request is
			// permitted. Behavior follows semantics; there is no automatic
			// retry loop (M4 scope).
			if err := conclude(store.ObservedReverseFailed, &msg, strPtr(string(class))); err != nil {
				return rop.ReversalResult{}, err
			}
			if err := transition(action.Reversing, action.Applied,
				"retriable failure before provider execution; action unchanged (attempt "+attemptID+"): "+msg); err != nil {
				return rop.ReversalResult{}, err
			}
			return rop.ReversalResult{
				AttemptID: attemptID, ActionID: a.ActionID,
				Status: rop.Status(action.Applied), Outcome: rop.OutcomeREVERSE_FAILED,
				ObservedAt: concludedAt, ProviderRef: providerRef, Error: msg,
			}, nil
		case operation.RetryNonRetriable:
			// Definite provider rejection: never blindly retried.
			if err := conclude(store.ObservedReverseFailed, &msg, strPtr(string(class))); err != nil {
				return rop.ReversalResult{}, err
			}
			if err := transition(action.Reversing, action.ReverseFailed,
				"provider rejected the reversal (attempt "+attemptID+"): "+msg); err != nil {
				return rop.ReversalResult{}, err
			}
			return rop.ReversalResult{
				AttemptID: attemptID, ActionID: a.ActionID,
				Status: rop.Status(action.ReverseFailed), Outcome: rop.OutcomeREVERSE_FAILED,
				ObservedAt: concludedAt, ProviderRef: providerRef, Error: msg,
			}, nil
		default:
			// RECONCILE_REQUIRED / MANUAL_INTERVENTION_REQUIRED / unclassified:
			// the outcome is unobserved; park for reconciliation (I-5).
			if err := s.Store.MarkAwaitingReconciliation(ctx, s.Store.DB(), attemptID, &msg, strPtr(string(class)), concludedAt); err != nil {
				return rop.ReversalResult{}, err
			}
			if err := transition(action.Reversing, action.OutcomeUnknown,
				"provider outcome unobserved (attempt "+attemptID+", class "+string(class)+"): "+msg); err != nil {
				return rop.ReversalResult{}, err
			}
			return rop.ReversalResult{
				AttemptID: attemptID, ActionID: a.ActionID,
				Status: rop.Status(action.OutcomeUnknown), Outcome: rop.OutcomeOUTCOME_UNKNOWN,
				ObservedAt: concludedAt, ProviderRef: providerRef, Error: msg,
			}, nil
		}
	}

	recordResidue := func(source string, items []rop.Residue) error {
		return s.Store.RecordResidue(ctx, s.Store.DB(), a.ActionID, source, items, concludedAt)
	}

	switch out.Outcome {
	case rop.OutcomeREVERSED:
		// Residue after a successful reversal is possible and is NOT evidence
		// of failure (Master Prompt §45): record it, keep REVERSED.
		if err := recordResidue(store.ResidueDiscovered, out.Residue); err != nil {
			return rop.ReversalResult{}, err
		}
		if err := conclude(store.ObservedReversed, nil, nil); err != nil {
			return rop.ReversalResult{}, err
		}
		if err := transition(action.Reversing, action.Reversed,
			"provider observed reversal (attempt "+attemptID+providerOwnRefNote(out.ProviderRef)+")"); err != nil {
			return rop.ReversalResult{}, err
		}
	case rop.OutcomePARTIALLY_REVERSED:
		// Partial compensation: the remaining effects are first-class residue
		// (M5), discovered during reversal and preserved append-style.
		if err := recordResidue(store.ResidueDiscovered, out.Residue); err != nil {
			return rop.ReversalResult{}, err
		}
		if err := conclude(store.ObservedPartiallyReversed, nil, nil); err != nil {
			return rop.ReversalResult{}, err
		}
		if err := transition(action.Reversing, action.PartiallyReversed,
			"provider observed partial reversal (attempt "+attemptID+providerOwnRefNote(out.ProviderRef)+")"); err != nil {
			return rop.ReversalResult{}, err
		}
	case rop.OutcomeREVERSE_FAILED:
		errMsg := out.Error
		if err := conclude(store.ObservedReverseFailed, &errMsg, nil); err != nil {
			return rop.ReversalResult{}, err
		}
		if err := transition(action.Reversing, action.ReverseFailed,
			"provider observed failure (attempt "+attemptID+providerOwnRefNote(out.ProviderRef)+")"); err != nil {
			return rop.ReversalResult{}, err
		}
	case rop.OutcomeCONFLICT:
		// Correctness-critical precondition failed (I-7): the reversal is
		// refused without side effects; the Action returns to APPLIED.
		errMsg := out.Error
		if errMsg == "" {
			errMsg = "correctness-critical precondition failed"
		}
		if err := conclude(store.ObservedConflict, &errMsg, nil); err != nil {
			return rop.ReversalResult{}, err
		}
		if err := transition(action.Reversing, action.Applied,
			"reversal refused on conflict (attempt "+attemptID+"): "+errMsg); err != nil {
			return rop.ReversalResult{}, err
		}
	case rop.OutcomeOUTCOME_UNKNOWN:
		// The provider itself could not determine the outcome; only
		// reconciliation may resolve this (invariant I-5). Attempt stays open.
		errMsg := out.Error
		if err := s.Store.MarkAwaitingReconciliation(ctx, s.Store.DB(), attemptID, &errMsg, strPtr(string(operation.RetryReconcileRequired)), concludedAt); err != nil {
			return rop.ReversalResult{}, err
		}
		if err := transition(action.Reversing, action.OutcomeUnknown,
			"provider outcome unobserved (attempt "+attemptID+providerOwnRefNote(out.ProviderRef)+")"); err != nil {
			return rop.ReversalResult{}, err
		}
	default:
		return rop.ReversalResult{}, fmt.Errorf("reversal: provider returned unknown outcome %q", out.Outcome)
	}

	status := actionStatusFor(out.Outcome)
	return rop.ReversalResult{
		AttemptID: attemptID, ActionID: a.ActionID,
		Status: rop.Status(status), Outcome: out.Outcome,
		ObservedAt: concludedAt, ProviderRef: providerRef, Error: out.Error,
	}, nil
}

func providerOwnRefNote(ref string) string {
	if ref == "" {
		return ""
	}
	return ", provider ref " + ref
}

func actionStatusFor(o rop.ReversalOutcome) string {
	switch o {
	case rop.OutcomeREVERSED:
		return action.Reversed
	case rop.OutcomePARTIALLY_REVERSED:
		return action.PartiallyReversed
	case rop.OutcomeREVERSE_FAILED:
		return action.ReverseFailed
	case rop.OutcomeCONFLICT:
		return action.Applied
	default:
		return action.OutcomeUnknown
	}
}

// ReconstructResult projects a durable attempt onto the wire result. There is
// no path that manufactures a REVERSED outcome without recorded evidence.
func ReconstructResult(attempt store.AttemptRow, a store.ActionRow) rop.ReversalResult {
	res := rop.ReversalResult{
		AttemptID: attempt.AttemptID,
		ActionID:  attempt.ActionID,
		Status:    rop.Status(a.Status),
	}
	if attempt.ProviderRef != nil {
		res.ProviderRef = *attempt.ProviderRef
	}
	if attempt.Error != nil {
		res.Error = *attempt.Error
	}
	if attempt.ConcludedAt != nil {
		res.ObservedAt = *attempt.ConcludedAt
	} else {
		res.ObservedAt = attempt.RequestedAt
	}
	switch {
	case attempt.ObservedResult != nil:
		switch *attempt.ObservedResult {
		case store.ObservedReversed:
			res.Outcome = rop.OutcomeREVERSED
		case store.ObservedPartiallyReversed:
			res.Outcome = rop.OutcomePARTIALLY_REVERSED
		case store.ObservedReverseFailed:
			res.Outcome = rop.OutcomeREVERSE_FAILED
		case store.ObservedConflict:
			res.Outcome = rop.OutcomeCONFLICT
		}
	default:
		// Open attempt (RUNNING after wait, or AWAITING_RECONCILIATION): the
		// outcome is genuinely unobserved (invariant I-5).
		res.Outcome = rop.OutcomeOUTCOME_UNKNOWN
	}
	return res
}

// RecoverAll implements the restart contract (Master Prompt §60; M4). An
// attempt found RUNNING after a process crash has an unobservable outcome: no
// durable marker can distinguish "the provider call never started" from "the
// provider succeeded but the result was not recorded". Recovery therefore
// parks every RUNNING attempt as AWAITING_RECONCILIATION and the Action as
// OUTCOME_UNKNOWN. It NEVER concludes an attempt as REVERSE_FAILED for lack
// of evidence (invariants I-5, I-11) and never re-invokes the provider side
// effect; reconciliation resolves uncertainty through provider lookups on the
// durable execution identity.
func (s *Service) RecoverAll(ctx context.Context) (int, error) {
	attempts, err := s.Store.GetRunningAttempts(ctx, s.Store.DB())
	if err != nil {
		return 0, err
	}
	now := s.Clock.Now()
	parked := 0
	for _, attempt := range attempts {
		a, ok, err := s.Store.GetActionByID(ctx, s.Store.DB(), attempt.ActionID)
		if err != nil {
			return parked, err
		}
		if !ok {
			return parked, fmt.Errorf("reversal: recovery found attempt %s for missing action %s (data inconsistency)", attempt.AttemptID, attempt.ActionID)
		}
		msg := "recovery: process restart; outcome unobserved"
		if err := s.Store.MarkAwaitingReconciliation(ctx, s.Store.DB(), attempt.AttemptID, &msg, strPtr(string(operation.RetryReconcileRequired)), now); err != nil {
			return parked, err
		}
		if err := s.Store.UpdateStatus(ctx, s.Store.DB(), a.Scope, a.ActionID,
			action.Reversing, action.OutcomeUnknown,
			"restart recovery: outcome unobserved (attempt "+attempt.AttemptID+")", now); err != nil {
			return parked, err
		}
		parked++
	}
	return parked, nil
}

func fmtTime(t *time.Time) string {
	if t == nil {
		return "n/a"
	}
	return t.UTC().Format(time.RFC3339)
}

func strPtr(s string) *string { return &s }

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
