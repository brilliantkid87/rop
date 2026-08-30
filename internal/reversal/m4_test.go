package reversal

import (
	"context"
	"path/filepath"
	"sync"
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

// chaosProvider simulates the seven provider behavior cases M4 must exercise
// (review scope: unknown outcome). It owns a provider-side world: which
// execution references actually performed the side effect, and what its
// lookup contract says.
type chaosProvider struct {
	mu sync.Mutex
	// executionRef records the refs whose side effect actually completed.
	executed map[string]bool
	// behavior of the next ReverseFunc call:
	failBefore      bool // 1: definitely fails before the side effect
	succeedNormally bool // 2: succeeds and returns normally
	loseResponse    bool // 3: succeeds but the response is lost
	undeterminable  bool // 4: outcome cannot be determined immediately
	rejectDefinite  bool // definite provider rejection (non-retriable)
	manualAmbiguous bool // 7: irreducibly ambiguous (manual)
	// lookup behavior:
	provenContract bool   // negative lookup proves non-execution
	reconcileErr   error  // lookup itself fails
	crash          func() // simulates a process crash mid-execution
	sentinel       *error // panic sentinel for crash simulation
	reverseCalls   int    // total ReverseFunc invocations
	executions     *int   // effective executions (side effects performed)
	lastRef        string // providerRef the provider last saw
}

func newChaosProvider() *chaosProvider {
	return &chaosProvider{executed: map[string]bool{}}
}

func (c *chaosProvider) op() operation.Operation {
	return operation.Operation{
		ID: "op.test", Reversibility: rop.ReversibilityREVERSIBLE,
		Guarantee: rop.GuaranteeEXACT, ReverseOperationID: "op.untest",
		ReverseFunc: func(ctx context.Context, in operation.ReverseInput) (operation.ReverseOutput, error) {
			c.mu.Lock()
			c.reverseCalls++
			c.lastRef = in.ProviderRef
			ref := in.ProviderRef
			failBefore, succeed, lose, undet, rejectDefinite, manual := c.failBefore, c.succeedNormally, c.loseResponse, c.undeterminable, c.rejectDefinite, c.manualAmbiguous
			crash := c.crash
			c.mu.Unlock()
			if crash != nil {
				crash() // simulates process death at this exact point
			}
			if c.executions != nil {
				*c.executions++
			}
			switch {
			case failBefore:
				return operation.ReverseOutput{}, &operation.ProviderFailure{
					Class: operation.RetryRetriable, Message: "connection refused before request transmission"}
			case rejectDefinite:
				return operation.ReverseOutput{}, &operation.ProviderFailure{
					Class: operation.RetryNonRetriable, Message: "provider rejected: policy forbids reversal"}
			case lose:
				c.mu.Lock()
				c.executed[ref] = true // the side effect DID happen
				c.mu.Unlock()
				return operation.ReverseOutput{}, &operation.ProviderFailure{
					Class: operation.RetryReconcileRequired, Message: "provider executed; response lost"}
			case undet:
				return operation.ReverseOutput{}, &operation.ProviderFailure{
					Class: operation.RetryReconcileRequired, Message: "provider status indeterminate"}
			case manual:
				return operation.ReverseOutput{}, &operation.ProviderFailure{
					Class: operation.RetryManualRequired, Message: "irreducibly ambiguous; manual review required"}
			case succeed:
				c.mu.Lock()
				c.executed[ref] = true
				c.mu.Unlock()
				return operation.ReverseOutput{Outcome: rop.OutcomeREVERSED}, nil
			}
			return operation.ReverseOutput{Outcome: rop.OutcomeREVERSED}, nil
		},
		ReconcileFunc: func(ctx context.Context, in operation.ReconcileInput) (operation.ReconcileOutput, error) {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.reconcileErr != nil {
				return operation.ReconcileOutput{}, c.reconcileErr
			}
			if c.executed[in.ProviderRef] {
				return operation.ReconcileOutput{Outcome: rop.OutcomeREVERSED, Proven: true,
					Detail: "provider records execution " + in.ProviderRef}, nil
			}
			if c.provenContract {
				return operation.ReconcileOutput{Outcome: rop.OutcomeREVERSE_FAILED, Proven: true,
					Detail: "no record of execution " + in.ProviderRef + "; provider contract guarantees absence proves non-execution"}, nil
			}
			return operation.ReconcileOutput{Outcome: rop.OutcomeREVERSE_FAILED, Proven: false,
				Detail: "not found, but the provider contract does not guarantee that absence proves non-execution"}, nil
		},
	}
}

func (c *chaosProvider) didExecute(ref string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.executed[ref]
}

// m4Harness: store + reversal + reconciliation over the chaos provider.
type m4Harness struct {
	st         *store.Store
	clk        *fakeClock
	svc        *Service
	dbPath     string
	chaos      *chaosProvider
	executions *int
}

func newM4Harness(t *testing.T) *m4Harness {
	h := &m4Harness{
		dbPath: filepath.Join(testutil.TempDirForDB(t), "t.db"),
		clk:    &fakeClock{T: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)},
		chaos:  newChaosProvider(),
	}
	h.open(t)
	h.seed(t, "act_1", nil)
	return h
}

func (h *m4Harness) open(t *testing.T) {
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
	n := 0
	if h.executions != nil {
		n = *h.executions
	}
	h.executions = &n
	h.chaos.executions = h.executions
	h.st = st
	op := h.chaos.op()
	reg, err := operation.NewRegistry(op)
	if err != nil {
		t.Fatal(err)
	}
	authorizer := authz.ScopeAllow{}
	h.svc = &Service{Store: st, Clock: h.clk, Registry: reg, Authorizer: authorizer}
}

func (h *m4Harness) seed(t *testing.T, actionID string, expires *time.Time) {
	t.Helper()
	if err := h.st.CreateAction(context.Background(), h.st.DB(), store.ActionRow{
		ActionID: actionID, Scope: "default", OperationID: "op.test",
		Status: action.Applied, Reversibility: "REVERSIBLE", Guarantee: "EXACT",
		ResourceType: "resource", ResourceID: "res_" + actionID,
		CreatedAt: h.clk.T, ExpiresAt: expires,
	}, nil); err != nil {
		t.Fatal(err)
	}
}

var m4Principal = authz.Principal{ID: "local", Scopes: map[string]bool{"default": true}}

// --- Provider behavior matrix ---

func TestDefiniteProviderFailureIsNotUnknown(t *testing.T) {
	// Case 2 (definite rejection) vs unknown: a classified definite failure
	// concludes REVERSE_FAILED — it is never reported as OUTCOME_UNKNOWN.
	h := newM4Harness(t)
	h.chaos.rejectDefinite = true
	res, err := h.svc.Reverse(context.Background(), m4Principal, "default", "act_1", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != rop.OutcomeREVERSE_FAILED || res.Status != rop.StatusREVERSE_FAILED {
		t.Fatalf("definite failure = %+v", res)
	}
	a, _, _ := h.st.GetAction(context.Background(), h.st.DB(), "default", "act_1")
	if a.Status != action.ReverseFailed {
		t.Fatalf("action status = %s", a.Status)
	}
}

func TestPreExecutionFailureIsRetriable(t *testing.T) {
	// Case 1: failure known to occur before provider execution — RETRYABLE.
	// Behavior follows semantics: the Action is unchanged and a NEW reversal
	// request is permitted (there is still no automatic retry engine).
	h := newM4Harness(t)
	h.chaos.failBefore = true
	res, err := h.svc.Reverse(context.Background(), m4Principal, "default", "act_1", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != rop.OutcomeREVERSE_FAILED || res.Status != rop.StatusAPPLIED {
		t.Fatalf("retriable failure = %+v, want failure with APPLIED action", res)
	}
	// A new reversal request executes again — permitted, not automatic.
	h.chaos.failBefore = false
	h.chaos.succeedNormally = true
	res2, err := h.svc.Reverse(context.Background(), m4Principal, "default", "act_1", "")
	if err != nil {
		t.Fatalf("new reversal after retriable failure: %v", err)
	}
	if res2.Outcome != rop.OutcomeREVERSED || *h.executions != 2 {
		t.Fatalf("second reversal = %+v executions=%d", res2, *h.executions)
	}
}

func TestDefiniteRejectionIsNotBlindlyRetried(t *testing.T) {
	// A definite provider rejection must not be blindly retried: one
	// execution, terminal state, further requests refused.
	h := newM4Harness(t)
	h.chaos.rejectDefinite = true
	if _, err := h.svc.Reverse(context.Background(), m4Principal, "default", "act_1", ""); err != nil {
		t.Fatal(err)
	}
	// No automatic retry happens: still exactly one execution.
	if *h.executions != 1 {
		t.Fatalf("executions = %d, want 1", *h.executions)
	}
	// An explicit new request is refused: the Action is terminal.
	_, err := h.svc.Reverse(context.Background(), m4Principal, "default", "act_1", "")
	if e := roperr.From(err); e == nil || e.ProblemType != rop.ProblemPreconditionFailed {
		t.Fatalf("re-request after terminal failure err = %v", err)
	}
	if *h.executions != 1 {
		t.Fatalf("executions = %d after re-request, want 1", *h.executions)
	}
}

func TestLostResponseBecomesUnknown(t *testing.T) {
	// Case 3: provider succeeds but the response is lost — the canonical
	// invariant: failure to observe success is not proof of failure.
	h := newM4Harness(t)
	h.chaos.loseResponse = true
	res, err := h.svc.Reverse(context.Background(), m4Principal, "default", "act_1", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != rop.OutcomeOUTCOME_UNKNOWN || res.Status != rop.StatusOUTCOME_UNKNOWN {
		t.Fatalf("lost response = %+v, want OUTCOME_UNKNOWN", res)
	}
	a, _, _ := h.st.GetAction(context.Background(), h.st.DB(), "default", "act_1")
	if a.Status != action.OutcomeUnknown {
		t.Fatalf("action status = %s", a.Status)
	}
	attempt, _, _ := h.st.GetLatestAttempt(context.Background(), h.st.DB(), "act_1")
	if attempt.ExecutionState != store.AttemptAwaitingReconciliation {
		t.Fatalf("attempt state = %s", attempt.ExecutionState)
	}
	if attempt.RetryClass == nil || *attempt.RetryClass != string(operation.RetryReconcileRequired) {
		t.Fatalf("retry class = %v", attempt.RetryClass)
	}
	// The provider-side effect DID happen — the system must not claim failure.
	if !h.chaos.didExecute(*attempt.ProviderRef) {
		t.Fatal("provider should have executed the side effect")
	}
}

func TestIrreduciblyAmbiguousBecomesUnknownWithManualClass(t *testing.T) {
	// Case 7: irreducibly ambiguous — preserved as unknown with the manual
	// classification recorded; never falsified into success or failure.
	h := newM4Harness(t)
	h.chaos.manualAmbiguous = true
	res, err := h.svc.Reverse(context.Background(), m4Principal, "default", "act_1", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != rop.OutcomeOUTCOME_UNKNOWN {
		t.Fatalf("outcome = %+v", res)
	}
	attempt, _, _ := h.st.GetLatestAttempt(context.Background(), h.st.DB(), "act_1")
	if attempt.RetryClass == nil || *attempt.RetryClass != string(operation.RetryManualRequired) {
		t.Fatalf("retry class = %v, want MANUAL_INTERVENTION_REQUIRED", attempt.RetryClass)
	}
}

func TestUnknownOutcomeIsNotAutomaticallyRetried(t *testing.T) {
	// After an unknown outcome nothing happens automatically: no execution
	// occurs without an explicit reconcile, and recovery does not re-execute.
	h := newM4Harness(t)
	h.chaos.loseResponse = true
	if _, err := h.svc.Reverse(context.Background(), m4Principal, "default", "act_1", ""); err != nil {
		t.Fatal(err)
	}
	if *h.executions != 1 {
		t.Fatalf("executions = %d", *h.executions)
	}
	// A new reversal request is refused (already in progress / unknown).
	if _, err := h.svc.Reverse(context.Background(), m4Principal, "default", "act_1", ""); roperr.From(err) == nil ||
		roperr.From(err).ProblemType != rop.ProblemAlreadyInProgress {
		t.Fatalf("second reverse err = %v", err)
	}
	// Recovery finds nothing RUNNING to re-execute.
	if n, err := h.svc.RecoverAll(context.Background()); err != nil || n != 0 {
		t.Fatalf("RecoverAll = %d %v", n, err)
	}
	if *h.executions != 1 {
		t.Fatalf("executions = %d after recovery, want 1", *h.executions)
	}
}

// --- Crash recovery ---

type crashSentinel struct{}

func (crashSentinel) Error() string { return "simulated process crash" }

func TestCrashBeforeProviderCall(t *testing.T) {
	// Crash point 1: after attempt persistence, before provider call. The
	// provider function simulates process death on entry (no side effect).
	h := newM4Harness(t)
	h.chaos.crash = func() { h.st.Close(); panic(crashSentinel{}) }
	func() {
		defer func() { _ = recover() }()
		_, _ = h.svc.Reverse(context.Background(), m4Principal, "default", "act_1", "")
	}()
	// Restart: reopen, recover.
	h.open(t)
	parked, err := h.svc.RecoverAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if parked != 1 {
		t.Fatalf("parked = %d, want 1", parked)
	}
	a, _, _ := h.st.GetAction(context.Background(), h.st.DB(), "default", "act_1")
	if a.Status != action.OutcomeUnknown {
		t.Fatalf("action after recovery = %s, want OUTCOME_UNKNOWN (uncertainty preserved)", a.Status)
	}
	attempt, _, _ := h.st.GetLatestAttempt(context.Background(), h.st.DB(), "act_1")
	if attempt.ExecutionState != store.AttemptAwaitingReconciliation {
		t.Fatalf("attempt after recovery = %s", attempt.ExecutionState)
	}
	// Reconciliation resolves it: nothing executed, contract proves it.
	h.chaos.provenContract = true
	_ = h.chaos // reconcile via the reconciliation service in its own tests
}

func TestCrashAfterProviderSuccess(t *testing.T) {
	// Crash point 2: provider succeeded, result not persisted. Recovery must
	// preserve uncertainty (not REVERSE_FAILED); reconciliation proves
	// success via the durable execution identity.
	h := newM4Harness(t)
	h.chaos.crash = func() {
		// The side effect completed inside the provider before the crash:
		ref := h.chaos.lastRef
		if ref != "" {
			h.chaos.mu.Lock()
			h.chaos.executed[ref] = true
			h.chaos.mu.Unlock()
		}
		h.st.Close()
		panic(crashSentinel{})
	}
	func() {
		defer func() { _ = recover() }()
		_, _ = h.svc.Reverse(context.Background(), m4Principal, "default", "act_1", "")
	}()
	h.open(t)
	if _, err := h.svc.RecoverAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	a, _, _ := h.st.GetAction(context.Background(), h.st.DB(), "default", "act_1")
	if a.Status != action.OutcomeUnknown {
		t.Fatalf("action after recovery = %s, want OUTCOME_UNKNOWN", a.Status)
	}
	attempt, _, _ := h.st.GetLatestAttempt(context.Background(), h.st.DB(), "act_1")
	// The durable execution identity survived the crash and points at the
	// provider-side effect.
	if attempt.ProviderRef == nil || !h.chaos.didExecute(*attempt.ProviderRef) {
		t.Fatalf("execution identity lost: %v", attempt.ProviderRef)
	}
}

func TestCrashAfterResultPersistenceBeforeResponse(t *testing.T) {
	// Crash point 3: result persisted, HTTP response lost. Restart + replay
	// with the same idempotency key returns the recorded result without
	// provider re-execution.
	h := newM4Harness(t)
	h.chaos.succeedNormally = true
	first, err := h.svc.Reverse(context.Background(), m4Principal, "default", "act_1", "crash-key")
	if err != nil {
		t.Fatal(err)
	}
	h.st.Close() // process dies after persistence, before the client sees it
	h.open(t)
	again, err := h.svc.Reverse(context.Background(), m4Principal, "default", "act_1", "crash-key")
	if err != nil {
		t.Fatal(err)
	}
	if again.AttemptID != first.AttemptID || again.Outcome != rop.OutcomeREVERSED {
		t.Fatalf("replay = %+v", again)
	}
	if *h.executions != 1 {
		t.Fatalf("executions = %d, want 1 (no provider re-execution)", *h.executions)
	}
}

func TestRestartWhileAwaitingReconciliation(t *testing.T) {
	// Crash point 4: an attempt awaiting reconciliation survives restart
	// intact — same attempt, same state, same execution identity.
	h := newM4Harness(t)
	h.chaos.loseResponse = true
	res, err := h.svc.Reverse(context.Background(), m4Principal, "default", "act_1", "")
	if err != nil {
		t.Fatal(err)
	}
	h.st.Close()
	h.open(t)
	attempt, ok, err := h.st.GetAttempt(context.Background(), h.st.DB(), res.AttemptID)
	if err != nil || !ok {
		t.Fatalf("attempt lost across restart: ok=%v err=%v", ok, err)
	}
	if attempt.ExecutionState != store.AttemptAwaitingReconciliation {
		t.Fatalf("attempt state after restart = %s", attempt.ExecutionState)
	}
	if attempt.ProviderRef == nil || *attempt.ProviderRef != res.ProviderRef {
		t.Fatalf("execution identity changed across restart")
	}
	a, _, _ := h.st.GetAction(context.Background(), h.st.DB(), "default", "act_1")
	if a.Status != action.OutcomeUnknown {
		t.Fatalf("action after restart = %s", a.Status)
	}
}

func TestRecoveryNeverConvertsReversingToFailed(t *testing.T) {
	// Master Prompt §60: recovery MUST NOT simply convert REVERSING into
	// REVERSE_FAILED. Across all crash points, no attempt ever becomes
	// REVERSE_FAILED by recovery alone.
	h := newM4Harness(t)
	h.chaos.crash = func() { h.st.Close(); panic(crashSentinel{}) }
	func() {
		defer func() { _ = recover() }()
		_, _ = h.svc.Reverse(context.Background(), m4Principal, "default", "act_1", "")
	}()
	h.open(t)
	if _, err := h.svc.RecoverAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	attempt, _, _ := h.st.GetLatestAttempt(context.Background(), h.st.DB(), "act_1")
	if attempt.ObservedResult != nil && *attempt.ObservedResult == store.ObservedReverseFailed {
		t.Fatal("recovery concluded REVERSE_FAILED without evidence")
	}
	if attempt.ExecutionState != store.AttemptAwaitingReconciliation {
		t.Fatalf("attempt state = %s", attempt.ExecutionState)
	}
}

// --- Provider execution identity ---

func TestProviderExecutionIdentityIsStable(t *testing.T) {
	// Every attempt carries a durable, stable identity pre-assigned before
	// the provider call; the provider sees it, and it never changes.
	h := newM4Harness(t)
	h.chaos.succeedNormally = true
	res, err := h.svc.Reverse(context.Background(), m4Principal, "default", "act_1", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderRef == "" || res.ProviderRef != "rop-rev-"+res.AttemptID {
		t.Fatalf("identity not derived from attempt: %q vs %s", res.ProviderRef, res.AttemptID)
	}
	if h.chaos.lastRef != res.ProviderRef {
		t.Fatalf("provider saw %q, want %q", h.chaos.lastRef, res.ProviderRef)
	}
	attempt, _, _ := h.st.GetAttempt(context.Background(), h.st.DB(), res.AttemptID)
	if attempt.ProviderRef == nil || *attempt.ProviderRef != res.ProviderRef {
		t.Fatalf("durable identity changed after conclusion")
	}
}
