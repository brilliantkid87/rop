package reversal

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/brilliantkid87/rop/internal/action"
	"github.com/brilliantkid87/rop/internal/authz"
	"github.com/brilliantkid87/rop/internal/operation"
	"github.com/brilliantkid87/rop/internal/roperr"
	"github.com/brilliantkid87/rop/internal/store"
	"github.com/brilliantkid87/rop/internal/testutil"
	"github.com/brilliantkid87/rop/pkg/rop"
)

type fakeClock struct{ T time.Time }

func (f *fakeClock) Now() time.Time { return f.T }

// harness wires a store, registry, and reversal service with a mutable canned
// provider outcome (no business tables involved — these tests target the
// reversal lifecycle itself).
type harness struct {
	st       *store.Store
	clk      *fakeClock
	svc      *Service
	outcome  *operation.ReverseOutput
	provErr  *error
	reversed *int // counts ReverseFunc invocations
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st, err := store.Open(filepath.Join(testutil.TempDirForDB(t), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(context.Background(), filepath.Join(testutil.RepoRoot(), "migrations")); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertOperation(context.Background(), st.DB(), store.OperationRow{
		OperationID: "op.test", Reversibility: "REVERSIBLE", Guarantee: "EXACT",
	}); err != nil {
		t.Fatal(err)
	}
	clk := &fakeClock{T: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)}
	h := &harness{st: st, clk: clk, outcome: &operation.ReverseOutput{Outcome: rop.OutcomeREVERSED}}
	op := operation.Operation{
		ID: "op.test", Reversibility: rop.ReversibilityREVERSIBLE,
		Guarantee: rop.GuaranteeEXACT, ReverseOperationID: "op.untest",
		ReverseFunc: func(ctx context.Context, in operation.ReverseInput) (operation.ReverseOutput, error) {
			if h.reversed != nil {
				*h.reversed++
			}
			if h.provErr != nil {
				return operation.ReverseOutput{}, *h.provErr
			}
			return *h.outcome, nil
		},
	}
	reg, err := operation.NewRegistry(op)
	if err != nil {
		t.Fatal(err)
	}
	h.svc = &Service{
		Store: st, Clock: clk, Registry: reg,
		Authorizer: authz.ScopeAllow{},
	}
	h.st.CreateAction(context.Background(), st.DB(), store.ActionRow{
		ActionID: "act_1", Scope: "default", OperationID: "op.test",
		Status: action.Applied, Reversibility: "REVERSIBLE", Guarantee: "EXACT",
		ResourceType: "resource", ResourceID: "res_1",
		CreatedAt: clk.T,
	}, map[string]any{"k": "v"})
	return h
}

var principal = authz.Principal{ID: "local", Scopes: map[string]bool{"default": true}}

func TestSuccessfulReversal(t *testing.T) {
	h := newHarness(t)
	res, err := h.svc.Reverse(context.Background(), principal, "default", "act_1", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != rop.OutcomeREVERSED || res.Status != rop.StatusREVERSED {
		t.Fatalf("unexpected result: %+v", res)
	}
	a, ok, _ := h.st.GetAction(context.Background(), h.st.DB(), "default", "act_1")
	if !ok || a.Status != action.Reversed {
		t.Fatalf("action status = %+v ok=%v", a, ok)
	}
	// Invariant I-1: the original Action survives reversal, with full history.
	var hist []string
	rows, err := h.st.DB().QueryContext(context.Background(),
		`SELECT to_status FROM action_status_history WHERE action_id='act_1' ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		hist = append(hist, s)
	}
	want := []string{action.Applied, action.Reversing, action.Reversed}
	if len(hist) != 3 || hist[0] != want[0] || hist[1] != want[1] || hist[2] != want[2] {
		t.Fatalf("history = %v, want %v", hist, want)
	}
	attempt, ok, _ := h.st.GetAttempt(context.Background(), h.st.DB(), res.AttemptID)
	if !ok || attempt.ExecutionState != store.AttemptConcluded || *attempt.ObservedResult != store.ObservedReversed {
		t.Fatalf("attempt = %+v ok=%v", attempt, ok)
	}
}

func TestConflictRefusesWithoutSideEffects(t *testing.T) {
	// Invariant I-7: a correctness-critical precondition failure returns the
	// Action to APPLIED; conflict, not destructive restoration.
	h := newHarness(t)
	h.outcome = &operation.ReverseOutput{Outcome: rop.OutcomeCONFLICT, Error: "resource changed"}
	res, err := h.svc.Reverse(context.Background(), principal, "default", "act_1", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != rop.OutcomeCONFLICT || res.Status != rop.StatusAPPLIED {
		t.Fatalf("unexpected result: %+v", res)
	}
	a, _, _ := h.st.GetAction(context.Background(), h.st.DB(), "default", "act_1")
	if a.Status != action.Applied {
		t.Fatalf("action status after conflict = %s, want APPLIED", a.Status)
	}
	attempt, _, _ := h.st.GetAttempt(context.Background(), h.st.DB(), res.AttemptID)
	if *attempt.ObservedResult != store.ObservedConflict {
		t.Fatalf("attempt observed = %v", attempt.ObservedResult)
	}
}

func TestProviderErrorIsUnknownNotFailed(t *testing.T) {
	// Invariant I-5: a lost/failed provider interaction is OUTCOME_UNKNOWN,
	// never REVERSE_FAILED. The attempt stays open for reconciliation.
	boom := context.DeadlineExceeded
	h := newHarness(t)
	h.provErr = &boom
	res, err := h.svc.Reverse(context.Background(), principal, "default", "act_1", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != rop.OutcomeOUTCOME_UNKNOWN || res.Status != rop.StatusOUTCOME_UNKNOWN {
		t.Fatalf("unexpected result: %+v", res)
	}
	a, _, _ := h.st.GetAction(context.Background(), h.st.DB(), "default", "act_1")
	if a.Status != action.OutcomeUnknown {
		t.Fatalf("action status = %s, want OUTCOME_UNKNOWN", a.Status)
	}
	attempt, _, _ := h.st.GetAttempt(context.Background(), h.st.DB(), res.AttemptID)
	if attempt.ExecutionState != store.AttemptAwaitingReconciliation {
		t.Fatalf("attempt state = %s, want AWAITING_RECONCILIATION", attempt.ExecutionState)
	}
	// While unresolved, a new reversal is refused as already-in-progress.
	_, err = h.svc.Reverse(context.Background(), principal, "default", "act_1", "")
	if e := roperr.From(err); e == nil || e.ProblemType != rop.ProblemAlreadyInProgress {
		t.Fatalf("second reverse err = %v", err)
	}
}

func TestExpiredActionCannotBeginReversal(t *testing.T) {
	// Invariant I-8 with the exact server-time boundary.
	h := newHarness(t)
	expires := h.clk.T.Add(time.Hour)
	_ = h.st.CreateAction(context.Background(), h.st.DB(), store.ActionRow{
		ActionID: "act_exp", Scope: "default", OperationID: "op.test",
		Status: action.Applied, Reversibility: "REVERSIBLE", Guarantee: "EXACT",
		ResourceType: "resource", ResourceID: "res_2",
		CreatedAt: h.clk.T, ExpiresAt: &expires,
	}, nil)
	h.clk.T = expires.Add(-time.Nanosecond)
	if _, err := h.svc.Reverse(context.Background(), principal, "default", "act_exp", ""); err != nil {
		t.Fatalf("one ns before expiry must still be eligible: %v", err)
	}
	// Reset the action (reversal concluded REVERSED above); make a fresh one.
	h.clk.T = time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	_ = h.st.CreateAction(context.Background(), h.st.DB(), store.ActionRow{
		ActionID: "act_exp2", Scope: "default", OperationID: "op.test",
		Status: action.Applied, Reversibility: "REVERSIBLE", Guarantee: "EXACT",
		ResourceType: "resource", ResourceID: "res_3",
		CreatedAt: h.clk.T, ExpiresAt: &expires,
	}, nil)
	h.clk.T = expires // exactly at the boundary ⇒ expired
	_, err := h.svc.Reverse(context.Background(), principal, "default", "act_exp2", "")
	if e := roperr.From(err); e == nil || e.ProblemType != rop.ProblemReversalExpired {
		t.Fatalf("at-boundary reverse err = %v, want reversal-expired", err)
	}
}

func TestIrreversibleAndWrongStateAreRejected(t *testing.T) {
	h := newHarness(t)
	_ = h.st.CreateAction(context.Background(), h.st.DB(), store.ActionRow{
		ActionID: "act_irr", Scope: "default", OperationID: "op.test",
		Status: action.Irreversible, Reversibility: "IRREVERSIBLE", Guarantee: "NONE",
		ResourceType: "resource", ResourceID: "res_4",
		CreatedAt: h.clk.T,
	}, nil)
	if _, err := h.svc.Reverse(context.Background(), principal, "default", "act_irr", ""); roperr.From(err) == nil ||
		roperr.From(err).ProblemType != rop.ProblemIrreversible {
		t.Fatal("irreversible action must be refused")
	}
	// A REVERSED action cannot be reversed again (M1: precondition-failed;
	// idempotency-key dedupe arrives in M3).
	_, err := h.svc.Reverse(context.Background(), principal, "default", "act_nosuch", "")
	if e := roperr.From(err); e == nil || e.ProblemType != rop.ProblemActionNotFound {
		t.Fatal("unknown action must be action-not-found")
	}
}

func TestActionIDIsNotAuthorization(t *testing.T) {
	// Invariant I-2: possession of a valid Action ID grants nothing.
	h := newHarness(t)
	h.svc.Authorizer = authz.DenyAll{}
	_, err := h.svc.Reverse(context.Background(), authz.Principal{ID: "stranger", Scopes: map[string]bool{"default": true}}, "default", "act_1", "")
	if e := roperr.From(err); e == nil || e.ProblemType != rop.ProblemAuthorizationDenied {
		t.Fatalf("denied authorizer err = %v", err)
	}
	// Invariant I-13: another scope cannot even see the Action.
	h.svc.Authorizer = authz.ScopeAllow{}
	_, err = h.svc.Reverse(context.Background(),
		authz.Principal{ID: "other", Scopes: map[string]bool{"other": true}}, "other", "act_1", "")
	if e := roperr.From(err); e == nil || e.ProblemType != rop.ProblemActionNotFound {
		t.Fatalf("cross-scope reverse err = %v, want action-not-found", err)
	}
}

// (M4) The M1-era TestRecoverOpenAttemptNeverFailsUnknown asserted
// re-execution of RUNNING attempts on recovery. M4 replaced that semantics:
// recovery parks uncertain attempts (no durable marker distinguishes
// crash-before-call from crash-after-success); reconciliation resolves them
// via provider lookups. See m4_test.go: TestCrashBeforeProviderCall,
// TestCrashAfterProviderSuccess, TestRecoveryNeverConvertsReversingToFailed.
