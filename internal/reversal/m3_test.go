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
	"github.com/brilliantkid87/rop/internal/planner"
	"github.com/brilliantkid87/rop/internal/roperr"
	"github.com/brilliantkid87/rop/internal/store"
	"github.com/brilliantkid87/rop/internal/testutil"
	"github.com/brilliantkid87/rop/pkg/rop"
)

// m3Harness is a restart-capable harness with execution counting.
type m3Harness struct {
	st       *store.Store
	clk      *fakeClock
	svc      *Service
	planner  *planner.Service
	dbPath   string
	outcome  *operation.ReverseOutput
	provErr  *error
	executed *int
}

func newM3Harness(t *testing.T) *m3Harness {
	t.Helper()
	dbPath := filepath.Join(testutil.TempDirForDB(t), "t.db")
	h := &m3Harness{dbPath: dbPath, clk: &fakeClock{T: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)}}
	h.open(t)
	h.seed(t, "default", "act_1", nil)
	return h
}

func (h *m3Harness) open(t *testing.T) {
	t.Helper()
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
	counter := 0
	if h.executed != nil {
		counter = *h.executed
	}
	h.st = st
	h.executed = &counter
	outcome := operation.ReverseOutput{Outcome: rop.OutcomeREVERSED}
	if h.outcome != nil {
		outcome = *h.outcome
	}
	h.outcome = &outcome
	clk := h.clk
	op := operation.Operation{
		ID: "op.test", Reversibility: rop.ReversibilityREVERSIBLE,
		Guarantee: rop.GuaranteeEXACT, ReverseOperationID: "op.untest",
		PlanFunc: func(ctx context.Context, in operation.PlanInput) (operation.PlanOutput, error) {
			return operation.PlanOutput{Preconditions: []string{"resource intact"}, ExpectedReversal: "delete"}, nil
		},
		ReverseFunc: func(ctx context.Context, in operation.ReverseInput) (operation.ReverseOutput, error) {
			*h.executed++
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
	authorizer := authz.ScopeAllow{}
	h.svc = &Service{Store: st, Clock: clk, Registry: reg, Authorizer: authorizer}
	h.planner = &planner.Service{Store: st, Clock: clk, Registry: reg, Authorizer: authorizer}
}

// seed inserts an Action for the given scope with optional expiry.
func (h *m3Harness) seed(t *testing.T, scope, actionID string, expires *time.Time) {
	t.Helper()
	err := h.st.CreateAction(context.Background(), h.st.DB(), store.ActionRow{
		ActionID: actionID, Scope: scope, OperationID: "op.test",
		Status: action.Applied, Reversibility: "REVERSIBLE", Guarantee: "EXACT",
		ResourceType: "resource", ResourceID: "res_" + actionID,
		CreatedAt: h.clk.T, ExpiresAt: expires,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

var m3Principal = authz.Principal{ID: "local", Scopes: map[string]bool{"default": true, "other": true}}

// --- Idempotency ---

func TestIdempotentReplaySameKey(t *testing.T) {
	// Master Prompt §36: equivalent requests with the same key MUST NOT create
	// two independent executions; the recorded result is returned.
	h := newM3Harness(t)
	first, err := h.svc.Reverse(context.Background(), m3Principal, "default", "act_1", "client-key-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.svc.Reverse(context.Background(), m3Principal, "default", "act_1", "client-key-1")
	if err != nil {
		t.Fatal(err)
	}
	if *h.executed != 1 {
		t.Fatalf("executions = %d, want 1", *h.executed)
	}
	if first.AttemptID != second.AttemptID || second.Outcome != rop.OutcomeREVERSED {
		t.Fatalf("replay mismatch: %+v vs %+v", first, second)
	}
}

func TestConcurrentSameKeyConverges(t *testing.T) {
	// Concurrent requests with the same key converge on one attempt: the
	// (scope, key_hash) unique index decides the winner; the loser replays.
	h := newM3Harness(t)
	var wg sync.WaitGroup
	results := make([]rop.ReversalResult, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = h.svc.Reverse(context.Background(), m3Principal, "default", "act_1", "race-key")
		}(i)
	}
	wg.Wait()
	for i := 0; i < 2; i++ {
		if errs[i] != nil {
			t.Fatalf("request %d failed: %v", i, errs[i])
		}
	}
	if results[0].AttemptID != results[1].AttemptID {
		t.Fatalf("concurrent same-key requests produced different attempts: %s vs %s",
			results[0].AttemptID, results[1].AttemptID)
	}
	if *h.executed != 1 {
		t.Fatalf("executions = %d, want exactly 1", *h.executed)
	}
}

func TestSameKeyDifferentActionRejected(t *testing.T) {
	// Same key reused with materially different request semantics (another
	// Action in the same scope) is rejected, not silently executed.
	h := newM3Harness(t)
	h.seed(t, "default", "act_2", nil)
	if _, err := h.svc.Reverse(context.Background(), m3Principal, "default", "act_1", "shared-key"); err != nil {
		t.Fatal(err)
	}
	_, err := h.svc.Reverse(context.Background(), m3Principal, "default", "act_2", "shared-key")
	if e := roperr.From(err); e == nil || e.ProblemType != rop.ProblemIdempotencyConflict {
		t.Fatalf("err = %v, want idempotency-key-conflict", err)
	}
	if *h.executed != 1 {
		t.Fatalf("conflicting reuse must not execute: %d", *h.executed)
	}
}

func TestSameKeyDifferentScopeIsSafe(t *testing.T) {
	// The same textual key can safely exist for a different authorization
	// scope: records are scoped (no cross-scope collision, I-13).
	h := newM3Harness(t)
	h.seed(t, "other", "act_other", nil)
	if _, err := h.svc.Reverse(context.Background(), m3Principal, "default", "act_1", "scope-key"); err != nil {
		t.Fatal(err)
	}
	res, err := h.svc.Reverse(context.Background(), m3Principal, "other", "act_other", "scope-key")
	if err != nil {
		t.Fatalf("same key in another scope must be independent: %v", err)
	}
	if res.Outcome != rop.OutcomeREVERSED || *h.executed != 2 {
		t.Fatalf("other-scope reversal did not execute independently: %+v executed=%d", res, *h.executed)
	}
}

func TestIdempotencySurvivesRestart(t *testing.T) {
	// Idempotency state is durable: after a full store close/reopen, a replay
	// with the same key returns the recorded result without re-execution.
	h := newM3Harness(t)
	first, err := h.svc.Reverse(context.Background(), m3Principal, "default", "act_1", "durable-key")
	if err != nil {
		t.Fatal(err)
	}
	h.st.Close()
	h.open(t) // same dbPath, fresh services

	second, err := h.svc.Reverse(context.Background(), m3Principal, "default", "act_1", "durable-key")
	if err != nil {
		t.Fatal(err)
	}
	if *h.executed != 1 {
		t.Fatalf("executions after restart = %d, want 1", *h.executed)
	}
	if first.AttemptID != second.AttemptID || second.Outcome != rop.OutcomeREVERSED {
		t.Fatalf("replay after restart mismatch: %+v vs %+v", first, second)
	}
}

func TestReplayOfUnknownOutcomeIsUnknown(t *testing.T) {
	// Invariant I-5: a replay of an attempt parked AWAITING_RECONCILIATION
	// returns OUTCOME_UNKNOWN — it must not re-execute and must not lie.
	boom := context.DeadlineExceeded
	h := newM3Harness(t)
	h.provErr = &boom
	first, err := h.svc.Reverse(context.Background(), m3Principal, "default", "act_1", "unknown-key")
	if err != nil {
		t.Fatal(err)
	}
	if first.Outcome != rop.OutcomeOUTCOME_UNKNOWN {
		t.Fatalf("first = %+v", first)
	}
	h.provErr = nil // if the replay re-executed, it would now succeed
	second, err := h.svc.Reverse(context.Background(), m3Principal, "default", "act_1", "unknown-key")
	if err != nil {
		t.Fatal(err)
	}
	if second.AttemptID != first.AttemptID || second.Outcome != rop.OutcomeOUTCOME_UNKNOWN {
		t.Fatalf("replay = %+v, want same attempt, OUTCOME_UNKNOWN", second)
	}
	if *h.executed != 1 {
		t.Fatalf("executions = %d, want 1", *h.executed)
	}
}

func TestIdempotencyUniqueConstraintAtDatabaseLevel(t *testing.T) {
	// The idempotency invariant is enforced by the database, not only by
	// application checks (M3 requirement).
	h := newM3Harness(t)
	if err := h.st.CreateAttempt(context.Background(), h.st.DB(), store.AttemptRow{
		AttemptID: "ra_x", ActionID: "act_1", RequestedAt: h.clk.T,
	}); err != nil {
		t.Fatal(err)
	}
	rec := store.IdempotencyRow{
		Scope: "default", ActionID: "act_1", KeyHash: hashKey("k"),
		Fingerprint: fingerprint("default", "act_1"), AttemptID: "ra_x", CreatedAt: h.clk.T,
	}
	if err := h.st.CreateIdempotency(context.Background(), h.st.DB(), rec); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	err := h.st.CreateIdempotency(context.Background(), h.st.DB(), store.IdempotencyRow{
		Scope: "default", ActionID: "act_1", KeyHash: hashKey("k"),
		Fingerprint: fingerprint("default", "act_1"), AttemptID: "ra_x", CreatedAt: h.clk.T,
	})
	if err != store.ErrIdempotencyKeyExists {
		t.Fatalf("second insert err = %v, want ErrIdempotencyKeyExists", err)
	}
}

// --- Expiration hardening ---

func TestNoExpiryActionRemainsEligible(t *testing.T) {
	// An Action without an expiry window never expires, however far the clock
	// moves.
	h := newM3Harness(t)
	h.clk.T = h.clk.T.Add(100 * 24 * time.Hour)
	if _, err := h.svc.Reverse(context.Background(), m3Principal, "default", "act_1", ""); err != nil {
		t.Fatalf("no-expiry action must remain eligible: %v", err)
	}
}

func TestPlanBeforeAndAfterExpiry(t *testing.T) {
	// Planning is read-only and remains available across expiry: before the
	// boundary it is a normal plan; after it, it reports the expired state
	// with an explicit conflict — it never looks actionable (I-19).
	expires := time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC)
	h := newM3Harness(t)
	h.seed(t, "default", "act_exp", &expires)

	before, err := h.planner.Plan(context.Background(), m3Principal, "default", "act_exp")
	if err != nil {
		t.Fatal(err)
	}
	if before.CurrentStatus != rop.StatusAPPLIED || len(before.Conflicts) != 0 {
		t.Fatalf("plan before expiry: %+v", before)
	}
	h.clk.T = expires.Add(time.Minute)
	after, err := h.planner.Plan(context.Background(), m3Principal, "default", "act_exp")
	if err != nil {
		t.Fatal(err)
	}
	if after.CurrentStatus != rop.StatusEXPIRED || len(after.Conflicts) == 0 {
		t.Fatalf("plan after expiry must show EXPIRED + conflict: %+v", after)
	}
}

func TestInFlightReversalFinishesAfterExpiry(t *testing.T) {
	// Master Prompt §52 recommended semantics: expiration controls whether a
	// NEW reversal may begin; a reversal accepted before the deadline
	// finishes. The provider call advances the clock past expiry mid-flight.
	expires := time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC)
	h := newM3Harness(t)
	h.seed(t, "default", "act_mid", &expires)
	h.svc.Clock = &mutatingClock{inner: h.clk, n: 2, mutate: func() {
		// 1st Now() = request acceptance (before expiry); 2nd = attempt
		// conclusion, after the clock moved past the deadline mid-flight.
		h.clk.T = expires.Add(5 * time.Minute)
	}}
	res, err := h.svc.Reverse(context.Background(), m3Principal, "default", "act_mid", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != rop.OutcomeREVERSED || res.Status != rop.StatusREVERSED {
		t.Fatalf("in-flight reversal must finish: %+v", res)
	}
	// The sweeper must not corrupt the concluded attempt afterwards.
	if _, err := h.st.SweepExpiry(context.Background(), h.st.DB(), h.clk.T.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	a, _, _ := h.st.GetAction(context.Background(), h.st.DB(), "default", "act_mid")
	if a.Status != action.Reversed {
		t.Fatalf("sweeper corrupted concluded reversal: %s", a.Status)
	}
	var hist []string
	rows, err := h.st.DB().Query(`SELECT to_status FROM action_status_history WHERE action_id='act_mid' ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		_ = rows.Scan(&v)
		hist = append(hist, v)
	}
	want := []string{action.Applied, action.Reversing, action.Reversed}
	if len(hist) != 3 || hist[0] != want[0] || hist[1] != want[1] || hist[2] != want[2] {
		t.Fatalf("history = %v, want %v (no EXPIRED transitions)", hist, want)
	}
}

// mutatingClock applies mutate() on its Nth Now() call — used to advance
// server time while a reversal is mid-flight (after acceptance, during the
// provider call).
type mutatingClock struct {
	inner  *fakeClock
	mutate func()
	n      int // call number to mutate on
	calls  int
	once   sync.Once
}

func (m *mutatingClock) Now() time.Time {
	m.calls++
	if m.calls == m.n {
		m.once.Do(m.mutate)
	}
	return m.inner.T
}

func TestSweeperPreservesConcludedAttemptsAcrossExpiry(t *testing.T) {
	// An Action that had a concluded attempt (a conflict-refused reversal)
	// and later expires: the sweeper transitions APPLIED→EXPIRED only; the
	// attempt record and the full history remain intact.
	expires := time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC)
	h := newM3Harness(t)
	h.seed(t, "default", "act_conf", &expires)
	h.outcome = &operation.ReverseOutput{Outcome: rop.OutcomeCONFLICT, Error: "changed"}
	if _, err := h.svc.Reverse(context.Background(), m3Principal, "default", "act_conf", "conf-key"); err != nil {
		t.Fatal(err)
	}
	h.clk.T = expires.Add(time.Minute)
	if n, err := h.st.SweepExpiry(context.Background(), h.st.DB(), h.clk.T); err != nil || n != 1 {
		t.Fatalf("sweep: %d %v", n, err)
	}
	a, _, _ := h.st.GetAction(context.Background(), h.st.DB(), "default", "act_conf")
	if a.Status != action.Expired {
		t.Fatalf("status = %s, want EXPIRED", a.Status)
	}
	attempt, ok, _ := h.st.GetAttempt(context.Background(), h.st.DB(), "ra_missing")
	_ = attempt
	_ = ok
	// Look up the attempt through the idempotency record.
	rec, found, err := h.st.GetIdempotency(context.Background(), h.st.DB(), "default", hashKey("conf-key"))
	if err != nil || !found {
		t.Fatalf("idempotency record: found=%v err=%v", found, err)
	}
	got, ok, err := h.st.GetAttempt(context.Background(), h.st.DB(), rec.AttemptID)
	if err != nil || !ok {
		t.Fatalf("attempt: ok=%v err=%v", ok, err)
	}
	if got.ExecutionState != store.AttemptConcluded || got.ObservedResult == nil || *got.ObservedResult != store.ObservedConflict {
		t.Fatalf("concluded attempt corrupted by sweeper: %+v", got)
	}
}

func TestExpirationAcrossRestart(t *testing.T) {
	// Expiry is derived from durable expiresAt and server time, so it applies
	// identically after a restart.
	expires := time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC)
	h := newM3Harness(t)
	h.seed(t, "default", "act_rs", &expires)
	h.clk.T = expires.Add(-time.Hour)
	h.st.Close()
	h.open(t)
	h.clk.T = expires // exactly at the boundary
	if _, err := h.svc.Reverse(context.Background(), m3Principal, "default", "act_rs", ""); roperr.From(err) == nil ||
		roperr.From(err).ProblemType != rop.ProblemReversalExpired {
		t.Fatalf("post-restart at-boundary reverse err = %v", err)
	}
}
