package verification

import (
	"context"
	"errors"
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

func newHarness(t *testing.T, verifyFunc operation.VerifyFunc) (*store.Store, *Service, *fakeClock) {
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
		VerifyFunc: verifyFunc,
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
	if err := st.CreateAction(context.Background(), st.DB(), store.ActionRow{
		ActionID: "act_1", Scope: "default", OperationID: "op.test",
		Status: status, Reversibility: "REVERSIBLE", Guarantee: "EXACT",
		ResourceType: "resource", ResourceID: "res_1", CreatedAt: clk.T,
	}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestVerificationIndependentOfInvocation(t *testing.T) {
	// Invariant I-10: verification evaluates postconditions on its own; a
	// FAILED result is meaningful even though the Action state is unchanged,
	// and is recorded for audit.
	st, svc, clk := newHarness(t, func(ctx context.Context, in operation.VerifyInput) (operation.VerifyOutput, error) {
		return operation.VerifyOutput{
			Status:    rop.VerificationFAILED,
			Semantics: rop.SemanticsLOCAL_READONLY,
			Postconditions: []rop.Postcondition{
				{ID: "resource-absent", Description: "gone", Satisfied: false},
			},
			Detail: "resource still exists",
		}, nil
	})
	seedAction(t, st, clk, action.Applied)
	res, err := svc.Verify(context.Background(), principal, "default", "act_1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != rop.VerificationFAILED || len(res.Postconditions) != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}
	status, _, _, evaluated, ok, err := st.LatestVerification(context.Background(), st.DB(), "act_1")
	if err != nil || !ok || status != "FAILED" || !evaluated.Equal(clk.T) {
		t.Fatalf("recorded verification: %s %v ok=%v err=%v", status, evaluated, ok, err)
	}
	// The Action state is untouched by verification.
	a, _, _ := st.GetAction(context.Background(), st.DB(), "default", "act_1")
	if a.Status != action.Applied {
		t.Fatalf("verification mutated action state: %s", a.Status)
	}
}

func TestVerificationFailureOfTheCheckIsUnknown(t *testing.T) {
	// Master Prompt §47: if verification's own calls fail, the result is
	// UNKNOWN — never reversal failure.
	st, svc, clk := newHarness(t, func(ctx context.Context, in operation.VerifyInput) (operation.VerifyOutput, error) {
		return operation.VerifyOutput{}, errors.New("provider lookup unavailable")
	})
	seedAction(t, st, clk, action.OutcomeUnknown)
	res, err := svc.Verify(context.Background(), principal, "default", "act_1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != rop.VerificationUNKNOWN {
		t.Fatalf("status = %s, want UNKNOWN", res.Status)
	}
	if len(res.Postconditions) != 0 {
		t.Fatalf("postconditions = %+v, want empty", res.Postconditions)
	}
}

func TestVerificationRefusals(t *testing.T) {
	// IRREVERSIBLE actions have no postconditions.
	st, svc, clk := newHarness(t, nil)
	if err := st.CreateAction(context.Background(), st.DB(), store.ActionRow{
		ActionID: "act_irr", Scope: "default", OperationID: "op.test",
		Status: action.Irreversible, Reversibility: "IRREVERSIBLE", Guarantee: "NONE",
		ResourceType: "resource", ResourceID: "res_2", CreatedAt: clk.T,
	}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(context.Background(), principal, "default", "act_irr"); roperr.From(err) == nil ||
		roperr.From(err).ProblemType != rop.ProblemIrreversible {
		t.Fatal("irreversible verification must be refused")
	}
	// Action ID is not authorization (I-2) and scope isolation holds (I-13).
	seedAction(t, st, clk, action.Applied)
	svc.Authorizer = authz.DenyAll{}
	if _, err := svc.Verify(context.Background(), principal, "default", "act_1"); roperr.From(err) == nil ||
		roperr.From(err).ProblemType != rop.ProblemAuthorizationDenied {
		t.Fatal("deny-all must yield authorization-denied")
	}
	svc.Authorizer = authz.ScopeAllow{}
	if _, err := svc.Verify(context.Background(),
		authz.Principal{ID: "o", Scopes: map[string]bool{"other": true}}, "other", "act_1"); roperr.From(err) == nil ||
		roperr.From(err).ProblemType != rop.ProblemActionNotFound {
		t.Fatal("cross-scope verify must be action-not-found")
	}
}
