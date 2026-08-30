package reconciliation

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/brilliantkid87/rop/internal/action"
	"github.com/brilliantkid87/rop/internal/authz"
	"github.com/brilliantkid87/rop/internal/operation"
	"github.com/brilliantkid87/rop/internal/reversal"
	"github.com/brilliantkid87/rop/internal/roperr"
	"github.com/brilliantkid87/rop/internal/store"
	"github.com/brilliantkid87/rop/internal/testutil"
	"github.com/brilliantkid87/rop/internal/verification"
	"github.com/brilliantkid87/rop/pkg/rop"
)

type fakeClock struct{ T time.Time }

func (f *fakeClock) Now() time.Time { return f.T }

// lookupProvider simulates a provider with a recorded execution world and a
// configurable lookup contract.
type lookupProvider struct {
	mu             sync.Mutex
	executed       map[string]bool
	provenContract bool
	reconcileErr   error
	proven         bool // whether lookups report Proven on negative results
	// failWithoutExecute: the reversal fails as unobservable BEFORE any side
	// effect (e.g. connection dropped mid-request). Nothing was executed.
	failWithoutExecute bool
	sideEffectRuns     int // effective executions (in-place counter)
}

func (p *lookupProvider) op() operation.Operation {
	return operation.Operation{
		ID: "op.test", Reversibility: rop.ReversibilityREVERSIBLE,
		Guarantee: rop.GuaranteeEXACT, ReverseOperationID: "op.untest",
		ReverseFunc: func(ctx context.Context, in operation.ReverseInput) (operation.ReverseOutput, error) {
			p.mu.Lock()
			if !p.failWithoutExecute {
				p.executed[in.ProviderRef] = true
			}
			p.sideEffectRuns++
			p.mu.Unlock()
			// The provider always "loses the response" so the test reaches the
			// uncertain state deterministically.
			return operation.ReverseOutput{}, &operation.ProviderFailure{
				Class: operation.RetryReconcileRequired, Message: "response lost after execution"}
		},
		ReconcileFunc: func(ctx context.Context, in operation.ReconcileInput) (operation.ReconcileOutput, error) {
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.reconcileErr != nil {
				return operation.ReconcileOutput{}, p.reconcileErr
			}
			if p.executed[in.ProviderRef] {
				return operation.ReconcileOutput{Outcome: rop.OutcomeREVERSED, Proven: true,
					Detail: "provider records the execution"}, nil
			}
			return operation.ReconcileOutput{Outcome: rop.OutcomeREVERSE_FAILED, Proven: p.proven,
				Detail: "not found"}, nil
		},
		// Verification evaluates the provider-defined postcondition on live
		// state ("was the effect reverted?"), completely independently of the
		// execution-outcome lookup.
		VerifyFunc: func(ctx context.Context, in operation.VerifyInput) (operation.VerifyOutput, error) {
			return operation.VerifyOutput{
				Status:    rop.VerificationVERIFIED,
				Semantics: rop.SemanticsLOCAL_READONLY,
				Postconditions: []rop.Postcondition{
					{ID: "effect-reverted", Description: "the original effect is reverted", Satisfied: true},
				},
			}, nil
		},
	}
}

type harness struct {
	st     *store.Store
	clk    *fakeClock
	prov   *lookupProvider
	recSvc *Service
	verSvc *verification.Service
	revSvc *reversal.Service
	reg    *operation.Registry
	dbPath string
}

func newHarness(t *testing.T) *harness {
	h := &harness{
		dbPath: filepath.Join(testutil.TempDirForDB(t), "t.db"),
		clk:    &fakeClock{T: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)},
		prov:   &lookupProvider{executed: map[string]bool{}},
	}
	h.open(t)
	if err := h.st.CreateAction(context.Background(), h.st.DB(), store.ActionRow{
		ActionID: "act_1", Scope: "default", OperationID: "op.test",
		Status: action.Applied, Reversibility: "REVERSIBLE", Guarantee: "EXACT",
		ResourceType: "resource", ResourceID: "res_1", CreatedAt: h.clk.T,
	}, nil); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) open(t *testing.T) {
	st, err := store.Open(h.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(context.Background(), filepath.Join(testutil.RepoRoot(), "migrations")); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertOperation(context.Background(), st.DB(), store.OperationRow{
		OperationID: "op.test", Reversibility: "REVERSIBLE", Guarantee: "EXACT",
	}); err != nil {
		t.Fatal(err)
	}
	h.st = st
	op := h.prov.op()
	reg, err := operation.NewRegistry(op)
	if err != nil {
		t.Fatal(err)
	}
	h.reg = reg
	authorizer := authz.ScopeAllow{}
	h.recSvc = &Service{Store: st, Clock: h.clk, Registry: reg, Authorizer: authorizer}
	h.verSvc = &verification.Service{Store: st, Clock: h.clk, Registry: reg, Authorizer: authorizer}
	h.revSvc = &reversal.Service{Store: st, Clock: h.clk, Registry: reg, Authorizer: authorizer}
}

// driveToUnknown brings act_1 into the canonical uncertain state:
// reversal executed, response lost, attempt AWAITING_RECONCILIATION.
func (h *harness) driveToUnknown(t *testing.T, key string) rop.ReversalResult {
	t.Helper()
	res, err := h.revSvc.Reverse(context.Background(), principal, "default", "act_1", key)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != rop.OutcomeOUTCOME_UNKNOWN {
		t.Fatalf("setup: expected unknown, got %+v", res)
	}
	return res
}

var principal = authz.Principal{ID: "local", Scopes: map[string]bool{"default": true}}

func TestReconciliationProvesSuccess(t *testing.T) {
	// The provider lookup proves the reversal occurred:
	// OUTCOME_UNKNOWN -> REVERSED, evidence-gated and durable.
	h := newHarness(t)
	unknown := h.driveToUnknown(t, "")
	res, err := h.recSvc.Reconcile(context.Background(), principal, "default", "act_1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != rop.OutcomeREVERSED || res.Status != rop.StatusREVERSED || res.AttemptID != unknown.AttemptID {
		t.Fatalf("reconciliation = %+v", res)
	}
	a, _, _ := h.st.GetAction(context.Background(), h.st.DB(), "default", "act_1")
	if a.Status != action.Reversed {
		t.Fatalf("action status = %s", a.Status)
	}
	obs, err := h.st.ObservationsForAttempt(context.Background(), h.st.DB(), unknown.AttemptID)
	if err != nil || len(obs) != 1 || obs[0].Evidence != store.EvidenceProvenReversed {
		t.Fatalf("observations = %+v err=%v", obs, err)
	}
}

func TestReconciliationProvesSafeFailure(t *testing.T) {
	// The provider lookup proves the reversal did NOT occur, under a contract
	// that guarantees absence proves non-execution: OUTCOME_UNKNOWN ->
	// REVERSE_FAILED (a safe, evidence-based conclusion).
	h := newHarness(t)
	// The reversal fails as unobservable BEFORE any side effect: nothing was
	// executed, but the response was lost so the outcome is unknown.
	h.prov.failWithoutExecute = true
	res, err := h.revSvc.Reverse(context.Background(), principal, "default", "act_1", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != rop.OutcomeOUTCOME_UNKNOWN {
		t.Fatalf("setup = %+v", res)
	}
	// The provider world has no execution for this ref and the contract
	// guarantees absence proves non-execution:
	h.prov.proven = true
	out, err := h.recSvc.Reconcile(context.Background(), principal, "default", "act_1")
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome != rop.OutcomeREVERSE_FAILED || out.Status != rop.StatusREVERSE_FAILED {
		t.Fatalf("proven failure = %+v", out)
	}
	a, _, _ := h.st.GetAction(context.Background(), h.st.DB(), "default", "act_1")
	if a.Status != action.ReverseFailed {
		t.Fatalf("action status = %s", a.Status)
	}
}

func TestNegativeLookupWithoutProofStaysUnknown(t *testing.T) {
	// A negative provider lookup is NOT proof of non-execution unless the
	// adapter's contract says so: "not found" with proven=false must leave
	// the attempt uncertain (Master Prompt §34, §38).
	h := newHarness(t)
	// Nothing was executed provider-side (the failure happened before the
	// effect), and the lookup contract does NOT prove absence.
	h.prov.failWithoutExecute = true
	h.prov.proven = false
	h.driveToUnknown(t, "")
	out, err := h.recSvc.Reconcile(context.Background(), principal, "default", "act_1")
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome != rop.OutcomeOUTCOME_UNKNOWN || out.Status != rop.StatusOUTCOME_UNKNOWN {
		t.Fatalf("unproven negative lookup resolved = %+v, want OUTCOME_UNKNOWN", out)
	}
	a, _, _ := h.st.GetAction(context.Background(), h.st.DB(), "default", "act_1")
	if a.Status != action.OutcomeUnknown {
		t.Fatalf("action status = %s, want OUTCOME_UNKNOWN", a.Status)
	}
}

func TestFailedLookupIsInconclusive(t *testing.T) {
	// If the reconciliation lookup itself fails, the observation is recorded
	// as INCONCLUSIVE and uncertainty is preserved — never failure.
	h := newHarness(t)
	h.driveToUnknown(t, "")
	h.prov.reconcileErr = context.DeadlineExceeded
	out, err := h.recSvc.Reconcile(context.Background(), principal, "default", "act_1")
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome != rop.OutcomeOUTCOME_UNKNOWN {
		t.Fatalf("outcome = %+v", out)
	}
	attempt, _, _ := h.st.GetLatestAttempt(context.Background(), h.st.DB(), "act_1")
	obs, _ := h.st.ObservationsForAttempt(context.Background(), h.st.DB(), attempt.AttemptID)
	if len(obs) != 1 || obs[0].Evidence != store.EvidenceInconclusive {
		t.Fatalf("observations = %+v", obs)
	}
}

func TestRepeatedReconciliationIsIdempotent(t *testing.T) {
	// Repeated rounds append observations and never re-invoke the side
	// effect; after a conclusion, further reconciles replay the recorded
	// result without new work.
	h := newHarness(t)
	unknown := h.driveToUnknown(t, "")

	// Two rounds whose lookups fail: INCONCLUSIVE observations, state unchanged.
	h.prov.reconcileErr = context.DeadlineExceeded
	for i := 0; i < 2; i++ {
		out, err := h.recSvc.Reconcile(context.Background(), principal, "default", "act_1")
		if err != nil {
			t.Fatal(err)
		}
		if out.Outcome != rop.OutcomeOUTCOME_UNKNOWN {
			t.Fatalf("round %d = %+v", i, out)
		}
	}
	// The lookup recovers and provider-world evidence proves success.
	h.prov.reconcileErr = nil
	h.prov.executed[unknown.ProviderRef] = true
	out, err := h.recSvc.Reconcile(context.Background(), principal, "default", "act_1")
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome != rop.OutcomeREVERSED {
		t.Fatalf("final reconciliation = %+v", out)
	}
	// A further reconcile is idempotent: replay, no new observation.
	obsBefore, _ := h.st.ObservationsForAttempt(context.Background(), h.st.DB(), unknown.AttemptID)
	again, err := h.recSvc.Reconcile(context.Background(), principal, "default", "act_1")
	if err != nil {
		t.Fatal(err)
	}
	if again.Outcome != rop.OutcomeREVERSED || again.AttemptID != unknown.AttemptID {
		t.Fatalf("post-conclusion reconcile = %+v", again)
	}
	obsAfter, _ := h.st.ObservationsForAttempt(context.Background(), h.st.DB(), unknown.AttemptID)
	if len(obsAfter) != len(obsBefore) {
		t.Fatalf("observations grew after conclusion: %d -> %d", len(obsBefore), len(obsAfter))
	}
	// Across every round, the reversal side effect ran exactly once.
	if h.prov.sideEffectRuns != 1 {
		t.Fatalf("side-effect executions = %d, want 1", h.prov.sideEffectRuns)
	}
}

func TestReconciliationRequiresAuthorization(t *testing.T) {
	// Reconciliation is internal but still behind authz; an Action ID grants
	// nothing (invariant I-2), and cross-scope is invisible (I-13).
	h := newHarness(t)
	h.driveToUnknown(t, "")
	h.recSvc.Authorizer = authz.DenyAll{}
	if _, err := h.recSvc.Reconcile(context.Background(), principal, "default", "act_1"); roperr.From(err) == nil ||
		roperr.From(err).ProblemType != rop.ProblemAuthorizationDenied {
		t.Fatalf("denied reconcile err = %v", err)
	}
	h.recSvc.Authorizer = authz.ScopeAllow{}
	if _, err := h.recSvc.Reconcile(context.Background(),
		authz.Principal{ID: "o", Scopes: map[string]bool{"other": true}}, "other", "act_1"); roperr.From(err) == nil ||
		roperr.From(err).ProblemType != rop.ProblemActionNotFound {
		t.Fatalf("cross-scope reconcile err = %v", err)
	}
}

func TestVerificationAndReconciliationRemainDistinct(t *testing.T) {
	// The required M4 case: execution outcome is unknown, verification CAN
	// establish the postcondition — and the reference implementation's
	// documented policy is that verification evidence alone does NOT conclude
	// the attempt; only reconciliation's proven provider evidence does.
	h := newHarness(t)
	h.driveToUnknown(t, "")

	ver, err := h.verSvc.Verify(context.Background(), principal, "default", "act_1")
	if err != nil {
		t.Fatal(err)
	}
	if ver.Status != rop.VerificationVERIFIED {
		t.Fatalf("verification = %+v, want VERIFIED (postcondition holds)", ver)
	}
	// The attempt is still unknown: verification did not conclude it.
	a, _, _ := h.st.GetAction(context.Background(), h.st.DB(), "default", "act_1")
	if a.Status != action.OutcomeUnknown {
		t.Fatalf("action status after verification = %s, want OUTCOME_UNKNOWN", a.Status)
	}
	// Reconciliation, with the same underlying fact, is the path that
	// concludes the attempt — via proven provider evidence.
	out, err := h.recSvc.Reconcile(context.Background(), principal, "default", "act_1")
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome != rop.OutcomeREVERSED {
		t.Fatalf("reconciliation = %+v", out)
	}
}

func TestDurableHistoryPreservesUncertaintySequence(t *testing.T) {
	// The durable record must explain the full sequence: request, execution
	// identity, lost response, transition to unknown, reconciliation
	// observation, conclusion, verification result.
	h := newHarness(t)
	unknown := h.driveToUnknown(t, "history-key")
	if _, err := h.recSvc.Reconcile(context.Background(), principal, "default", "act_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.verSvc.Verify(context.Background(), principal, "default", "act_1"); err != nil {
		t.Fatal(err)
	}

	attempt, ok, err := h.st.GetAttempt(context.Background(), h.st.DB(), unknown.AttemptID)
	if err != nil || !ok {
		t.Fatalf("attempt: ok=%v err=%v", ok, err)
	}
	if attempt.ProviderRef == nil || *attempt.ProviderRef != unknown.ProviderRef {
		t.Fatal("execution identity missing from durable record")
	}
	if attempt.Error == nil || *attempt.Error == "" {
		t.Fatal("observed failure (lost response) not recorded")
	}
	if attempt.RetryClass == nil || *attempt.RetryClass != string(operation.RetryReconcileRequired) {
		t.Fatal("retry classification not recorded")
	}

	// Status history: APPLIED → REVERSING → OUTCOME_UNKNOWN → REVERSED.
	var hist []string
	rows, err := h.st.DB().Query(`SELECT to_status FROM action_status_history WHERE action_id='act_1' ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var v string
		_ = rows.Scan(&v)
		hist = append(hist, v)
	}
	rows.Close()
	want := []string{action.Applied, action.Reversing, action.OutcomeUnknown, action.Reversed}
	if len(hist) != len(want) {
		t.Fatalf("history = %v, want %v", hist, want)
	}
	for i := range want {
		if hist[i] != want[i] {
			t.Fatalf("history = %v, want %v", hist, want)
		}
	}

	// Reconciliation observations: durable and evidence-classified.
	obs, err := h.st.ObservationsForAttempt(context.Background(), h.st.DB(), unknown.AttemptID)
	if err != nil || len(obs) != 1 || obs[0].Evidence != store.EvidenceProvenReversed {
		t.Fatalf("observations = %+v err=%v", obs, err)
	}

	// Verification result recorded independently.
	status, _, _, _, ok, err := h.st.LatestVerification(context.Background(), h.st.DB(), "act_1")
	if err != nil || !ok || status != "VERIFIED" {
		t.Fatalf("verification record: ok=%v status=%s err=%v", ok, status, err)
	}
}
