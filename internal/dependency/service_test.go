package dependency

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/brilliantkid87/rop/internal/action"
	"github.com/brilliantkid87/rop/internal/roperr"
	"github.com/brilliantkid87/rop/internal/store"
	"github.com/brilliantkid87/rop/internal/testutil"
	"github.com/brilliantkid87/rop/pkg/rop"
)

type harness struct {
	st  *store.Store
	svc *Service
	clk *fixedClock
}

type fixedClock struct{ T time.Time }

func (f *fixedClock) Now() time.Time { return f.T }

func newHarness(t *testing.T) *harness {
	st, err := store.Open(filepath.Join(testutil.TempDirForDB(t), "t.db"))
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
	clk := &fixedClock{T: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)}
	svc := &Service{Store: st}
	seed := func(id, status string) {
		err := st.CreateAction(context.Background(), st.DB(), store.ActionRow{
			ActionID: id, Scope: "default", OperationID: "op.test",
			Status: status, Reversibility: "REVERSIBLE", Guarantee: "EXACT",
			ResourceType: "resource", ResourceID: "res_" + id, CreatedAt: clk.T,
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"act_a", "act_b", "act_c", "act_d"} {
		seed(id, action.Applied)
	}
	return &harness{st: st, svc: svc, clk: clk}
}

func TestDirectCycleRejected(t *testing.T) {
	h := newHarness(t)
	if err := h.svc.Add(context.Background(), "default", "act_a", "act_b"); err != nil {
		t.Fatal(err)
	}
	// B depends on A; adding A depends on B is a direct cycle.
	err := h.svc.Add(context.Background(), "default", "act_b", "act_a")
	if e := roperr.From(err); e == nil || e.ProblemType != rop.ProblemDependencyExists {
		t.Fatalf("direct cycle err = %v, want dependency-exists", err)
	}
}

func TestIndirectCycleRejected(t *testing.T) {
	h := newHarness(t)
	// B->A, C->B; adding A->C closes the loop A...C->A indirectly.
	if err := h.svc.Add(context.Background(), "default", "act_a", "act_b"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Add(context.Background(), "default", "act_b", "act_c"); err != nil {
		t.Fatal(err)
	}
	err := h.svc.Add(context.Background(), "default", "act_c", "act_a")
	if e := roperr.From(err); e == nil || e.ProblemType != rop.ProblemDependencyExists {
		t.Fatalf("indirect cycle err = %v, want dependency-exists", err)
	}
}

func TestSelfDependencyRejected(t *testing.T) {
	h := newHarness(t)
	err := h.svc.Add(context.Background(), "default", "act_a", "act_a")
	if e := roperr.From(err); e == nil || e.ProblemType != rop.ProblemDependencyExists {
		t.Fatalf("self dependency err = %v", err)
	}
}

func TestDuplicateDependencyIsIdempotent(t *testing.T) {
	// Duplicate edge = the same fact, handled safely: no error, one row.
	h := newHarness(t)
	if err := h.svc.Add(context.Background(), "default", "act_a", "act_b"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Add(context.Background(), "default", "act_a", "act_b"); err != nil {
		t.Fatalf("duplicate dependency must be a safe no-op: %v", err)
	}
	deps, err := h.st.DependenciesOfParent(context.Background(), h.st.DB(), "default", "act_a")
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 {
		t.Fatalf("dependency rows = %d, want 1", len(deps))
	}
}

func TestCrossScopeDependencyRejected(t *testing.T) {
	// Dependency edges are scope-local: an edge referencing an Action that
	// does not exist in the given scope is rejected (invariant I-13).
	h := newHarness(t)
	err := h.svc.Add(context.Background(), "other-scope", "act_a", "act_b")
	if e := roperr.From(err); e == nil || e.ProblemType != rop.ProblemActionNotFound {
		t.Fatalf("cross-scope dependency err = %v, want action-not-found", err)
	}
}

func TestActiveDependentRule(t *testing.T) {
	// The documented rule (ResolvedStatuses): REVERSED and PARTIALLY_REVERSED
	// dependents stop blocking; every other status keeps blocking. This is a
	// documented decision (OQ-15), not an accidental hard-code.
	h := newHarness(t)
	if err := h.svc.Add(context.Background(), "default", "act_a", "act_b"); err != nil {
		t.Fatal(err)
	}
	blocking, err := h.svc.Blocking(context.Background(), "default", "act_a")
	if err != nil || len(blocking) != 1 || blocking[0] != "act_b" {
		t.Fatalf("APPLIED dependent must block: %v err=%v", blocking, err)
	}
	for _, tc := range []struct {
		status     string
		wantBlocks bool
	}{
		{action.Reversing, true},
		{action.OutcomeUnknown, true},
		{action.ReverseFailed, true},
		{action.Expired, true},
		{action.Irreversible, true},
		{action.Reversed, false},
		{action.PartiallyReversed, false},
	} {
		// Fixture: set the status directly. This test exercises the Blocking
		// rule over all statuses, which the legal transition table cannot
		// reach in one hop; runtime code always transitions legally.
		if _, err := h.st.DB().ExecContext(context.Background(),
			`UPDATE actions SET status = ? WHERE action_id = 'act_b'`, tc.status); err != nil {
			t.Fatal(err)
		}
		blocking, err := h.svc.Blocking(context.Background(), "default", "act_a")
		if err != nil {
			t.Fatal(err)
		}
		got := len(blocking) > 0
		if got != tc.wantBlocks {
			t.Errorf("status %s: blocking=%v, want %v", tc.status, got, tc.wantBlocks)
		}
	}
}
