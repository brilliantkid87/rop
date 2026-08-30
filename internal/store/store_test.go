package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/brilliantkid87/rop/internal/action"
	"github.com/brilliantkid87/rop/internal/testutil"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := testutil.TempDirForDB(t)
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(context.Background(), filepath.Join(testutil.RepoRoot(), "migrations")); err != nil {
		t.Fatal(err)
	}
	seedOperation(t, st)
	return st
}

// seedOperation inserts the Operation the test Actions reference (FK).
func seedOperation(t *testing.T, st *Store) {
	t.Helper()
	err := st.UpsertOperation(context.Background(), st.DB(), OperationRow{
		OperationID: "op.test", Reversibility: "REVERSIBLE", Guarantee: "EXACT",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func testAction(id string, expires *time.Time) ActionRow {
	return ActionRow{
		ActionID: id, Scope: "default", OperationID: "op.test",
		Status: action.Applied, Reversibility: "REVERSIBLE", Guarantee: "EXACT",
		ResourceType: "resource", ResourceID: "res_x",
		CreatedAt: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC),
		ExpiresAt: expires,
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	st := openTestStore(t)
	if err := st.Migrate(context.Background(), filepath.Join(testutil.RepoRoot(), "migrations")); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestCreateActionPersistsHistoryAndMaterial(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	seedOperation(t, st)
	mat := map[string]any{"previousResourceVersion": float64(1)}
	if err := st.CreateAction(ctx, st.DB(), testAction("act_1", nil), mat); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.GetAction(ctx, st.DB(), "default", "act_1")
	if err != nil || !ok {
		t.Fatalf("get action: ok=%v err=%v", ok, err)
	}
	if got.Status != action.Applied || got.OperationID != "op.test" {
		t.Fatalf("unexpected action: %+v", got)
	}
	m, ok, err := st.GetMaterial(ctx, st.DB(), "default", "act_1")
	if err != nil || !ok || m["previousResourceVersion"] != float64(1) {
		t.Fatalf("material: ok=%v err=%v m=%v", ok, err, m)
	}
}

func TestGetActionIsScopeFiltered(t *testing.T) {
	// Invariant I-13: cross-scope lookup is indistinguishable from not-found.
	ctx := context.Background()
	st := openTestStore(t)
	seedOperation(t, st)
	if err := st.CreateAction(ctx, st.DB(), testAction("act_1", nil), nil); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.GetAction(ctx, st.DB(), "other-scope", "act_1"); err != nil || ok {
		t.Fatalf("cross-scope lookup must fail: ok=%v err=%v", ok, err)
	}
	if _, ok, err := st.GetMaterial(ctx, st.DB(), "other-scope", "act_1"); err != nil || ok {
		t.Fatalf("cross-scope material lookup must fail: ok=%v err=%v", ok, err)
	}
}

func TestUpdateStatusAppendsHistory(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	seedOperation(t, st)
	if err := st.CreateAction(ctx, st.DB(), testAction("act_1", nil), nil); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC)
	if err := st.UpdateStatus(ctx, st.DB(), "default", "act_1", action.Applied, action.Reversing, "t1", now); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateStatus(ctx, st.DB(), "default", "act_1", action.Reversing, action.Reversed, "t2", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	// Illegal transition refused.
	if err := st.UpdateStatus(ctx, st.DB(), "default", "act_1", action.Reversed, action.Applied, "t3", now); err == nil {
		t.Fatal("illegal transition must be refused")
	}
	var n int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM action_status_history WHERE action_id = 'act_1'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 { // initial + two transitions; illegal one wrote nothing
		t.Fatalf("history rows = %d, want 3 (invariant I-1: append-only audit)", n)
	}
	var status string
	if err := st.DB().QueryRowContext(ctx,
		`SELECT status FROM actions WHERE action_id = 'act_1'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != action.Reversed {
		t.Fatalf("status = %s", status)
	}
}

func TestOneNonConcludedAttemptPerAction(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	seedOperation(t, st)
	if err := st.CreateAction(ctx, st.DB(), testAction("act_1", nil), nil); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC)
	if err := st.CreateAttempt(ctx, st.DB(), AttemptRow{AttemptID: "ra_1", ActionID: "act_1", RequestedAt: now}); err != nil {
		t.Fatal(err)
	}
	err := st.CreateAttempt(ctx, st.DB(), AttemptRow{AttemptID: "ra_2", ActionID: "act_1", RequestedAt: now})
	if err != ErrAttemptInProgress {
		t.Fatalf("second open attempt: err=%v, want ErrAttemptInProgress", err)
	}
	// Concluding frees the slot.
	ref := "provider-ref-1"
	if err := st.ConcludeAttempt(ctx, st.DB(), "ra_1", ObservedReversed, &ref, nil, now); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateAttempt(ctx, st.DB(), AttemptRow{AttemptID: "ra_2", ActionID: "act_1", RequestedAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("attempt after conclusion must be allowed: %v", err)
	}
}

func TestSweepExpiryBoundary(t *testing.T) {
	// Master Prompt §24/§52: receivedAt >= expiresAt ⇒ expired, tested exactly
	// at the boundary and one tick before.
	ctx := context.Background()
	st := openTestStore(t)
	base := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	expires := base.Add(time.Hour)
	if err := st.CreateAction(ctx, st.DB(), testAction("act_edge", &expires), nil); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateAction(ctx, st.DB(), testAction("act_late", nil), nil); err != nil {
		t.Fatal(err) // no window: never expires
	}
	if n, err := st.SweepExpiry(ctx, st.DB(), expires.Add(-time.Nanosecond)); err != nil || n != 0 {
		t.Fatalf("one nanosecond before expiry: swept=%d err=%v, want 0", n, err)
	}
	got, _, _ := st.GetAction(ctx, st.DB(), "default", "act_edge")
	if got.Status != action.Applied {
		t.Fatalf("status before boundary = %s", got.Status)
	}
	if n, err := st.SweepExpiry(ctx, st.DB(), expires); err != nil || n != 1 {
		t.Fatalf("exactly at expiry: swept=%d err=%v, want 1", n, err)
	}
	got, _, _ = st.GetAction(ctx, st.DB(), "default", "act_edge")
	if got.Status != action.Expired {
		t.Fatalf("status at boundary = %s, want EXPIRED", got.Status)
	}
	if n, err := st.SweepExpiry(ctx, st.DB(), expires.Add(time.Nanosecond)); err != nil || n != 0 {
		t.Fatalf("re-sweep: swept=%d err=%v, want 0", n, err)
	}
	// No-window action is untouched.
	got, _, _ = st.GetAction(ctx, st.DB(), "default", "act_late")
	if got.Status != action.Applied {
		t.Fatalf("no-window action expired: %s", got.Status)
	}
}

func TestVerificationRecordsAppend(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	seedOperation(t, st)
	if err := st.CreateAction(ctx, st.DB(), testAction("act_1", nil), nil); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC)
	if err := st.RecordVerification(ctx, st.DB(), "act_1", "FAILED", "LOCAL_READONLY", "[]", now); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordVerification(ctx, st.DB(), "act_1", "UNKNOWN", "LOCAL_READONLY", "[]", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	status, _, _, evaluated, ok, err := st.LatestVerification(ctx, st.DB(), "act_1")
	if err != nil || !ok {
		t.Fatalf("latest verification: ok=%v err=%v", ok, err)
	}
	if status != "UNKNOWN" || !evaluated.Equal(now.Add(time.Minute)) {
		t.Fatalf("latest = %s at %v", status, evaluated)
	}
}
