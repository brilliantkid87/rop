package planner

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

func newHarness(t *testing.T, planFunc operation.PlanFunc) (*store.Store, *Service, *fakeClock) {
	t.Helper()
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
	clk := &fakeClock{T: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)}
	reg, err := operation.NewRegistry(operation.Operation{
		ID: "op.test", Reversibility: rop.ReversibilityREVERSIBLE,
		Guarantee: rop.GuaranteeEXACT, ReverseOperationID: "op.untest",
		PlanFunc: planFunc,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: st, Clock: clk, Registry: reg, Authorizer: authz.ScopeAllow{}}
	return st, svc, clk
}

var principal = authz.Principal{ID: "local", Scopes: map[string]bool{"default": true}}

func seedAction(t *testing.T, st *store.Store, clk *fakeClock, status string) {
	t.Helper()
	err := st.CreateAction(context.Background(), st.DB(), store.ActionRow{
		ActionID: "act_1", Scope: "default", OperationID: "op.test",
		Status: status, Reversibility: "REVERSIBLE", Guarantee: "EXACT",
		ResourceType: "resource", ResourceID: "res_1", CreatedAt: clk.T,
	}, map[string]any{"previousResourceVersion": float64(7)})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPlanReflectsProviderAndFreshness(t *testing.T) {
	v := int64(7)
	valid := time.Date(2026, 8, 30, 0, 5, 0, 0, time.UTC)
	st, svc, clk := newHarness(t, func(ctx context.Context, in operation.PlanInput) (operation.PlanOutput, error) {
		return operation.PlanOutput{
			Preconditions:        []string{"resource version unchanged"},
			ExpectedReversal:     "delete the resource",
			BasisResourceVersion: &v,
			ValidUntil:           &valid,
			Residue:              []rop.Residue{{Description: "audit record remains"}},
		}, nil
	})
	seedAction(t, st, clk, action.Applied)
	plan, err := svc.Plan(context.Background(), principal, "default", "act_1")
	if err != nil {
		t.Fatal(err)
	}
	if plan.CurrentStatus != rop.StatusAPPLIED || plan.GeneratedAt != clk.T {
		t.Fatalf("plan meta wrong: %+v", plan)
	}
	if len(plan.Preconditions) != 1 || plan.BasisResourceVersion == nil || *plan.BasisResourceVersion != 7 {
		t.Fatalf("plan provider fields wrong: %+v", plan)
	}
	if len(plan.Residue) != 1 || plan.Residue[0].Description != "audit record remains" {
		t.Fatalf("residue wrong: %+v", plan.Residue)
	}
}

func TestPlanningHasNoSideEffects(t *testing.T) {
	// Invariant I-3: planning produces no external business side effects and
	// does not mutate Action state or history.
	var calls int
	st, svc, clk := newHarness(t, func(ctx context.Context, in operation.PlanInput) (operation.PlanOutput, error) {
		calls++
		return operation.PlanOutput{Preconditions: []string{"p"}}, nil
	})
	seedAction(t, st, clk, action.Applied)
	var rowsBefore, histBefore int
	_ = st.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM actions`).Scan(&rowsBefore)
	_ = st.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM action_status_history`).Scan(&histBefore)
	for i := 0; i < 3; i++ {
		if _, err := svc.Plan(context.Background(), principal, "default", "act_1"); err != nil {
			t.Fatal(err)
		}
	}
	var rowsAfter, histAfter int
	_ = st.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM actions`).Scan(&rowsAfter)
	_ = st.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM action_status_history`).Scan(&histAfter)
	if calls != 3 || rowsAfter != rowsBefore || histAfter != histBefore {
		t.Fatalf("planning mutated state: calls=%d rows %d->%d hist %d->%d",
			calls, rowsBefore, rowsAfter, histBefore, histAfter)
	}
}

func TestPlanIsNotAuthorizationAndKnowsIt(t *testing.T) {
	// Invariant I-19 support: a plan for a non-APPLIED action carries an
	// explicit conflict instead of looking actionable.
	st, svc, clk := newHarness(t, func(ctx context.Context, in operation.PlanInput) (operation.PlanOutput, error) {
		return operation.PlanOutput{}, nil
	})
	seedAction(t, st, clk, action.Reversed)
	plan, err := svc.Plan(context.Background(), principal, "default", "act_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) == 0 {
		t.Fatal("plan for non-APPLIED action must list a conflict")
	}
}

func TestPlanDenials(t *testing.T) {
	st, svc, clk := newHarness(t, nil)
	seedAction(t, st, clk, action.Applied)
	if _, err := svc.Plan(context.Background(), authz.Principal{ID: "x", Scopes: map[string]bool{"elsewhere": true}}, "elsewhere", "act_1"); roperr.From(err) == nil ||
		roperr.From(err).ProblemType != rop.ProblemActionNotFound {
		t.Fatal("cross-scope plan must be action-not-found (I-13)")
	}
	svc.Authorizer = authz.DenyAll{}
	if _, err := svc.Plan(context.Background(), principal, "default", "act_1"); roperr.From(err) == nil ||
		roperr.From(err).ProblemType != rop.ProblemAuthorizationDenied {
		t.Fatal("deny-all must yield authorization-denied (I-2)")
	}
}
