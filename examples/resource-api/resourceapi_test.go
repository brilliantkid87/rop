package resourceapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brilliantkid87/rop/internal/authz"
	"github.com/brilliantkid87/rop/internal/clock"
	"github.com/brilliantkid87/rop/internal/dependency"
	"github.com/brilliantkid87/rop/internal/httpapi"
	"github.com/brilliantkid87/rop/internal/operation"
	"github.com/brilliantkid87/rop/internal/planner"
	"github.com/brilliantkid87/rop/internal/reversal"
	"github.com/brilliantkid87/rop/internal/store"
	"github.com/brilliantkid87/rop/internal/testutil"
	"github.com/brilliantkid87/rop/internal/verification"
	"github.com/brilliantkid87/rop/pkg/rop"
)

// fixture loads a golden fixture from spec/fixtures/v0.1.
func fixture(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testutil.RepoRoot(), "spec", "fixtures", "v0.1", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return m
}

// server is the full M1/M2 wiring: demo provider + ROP Core + HTTP binding.
type server struct {
	ts     *httptest.Server
	st     *store.Store
	clk    *clock.Fixed
	svc    *reversal.Service
	plan   *planner.Service
	ver    *verification.Service
	api    *API
	dbPath string
	caps   rop.Capabilities
	reg    *operation.Registry
	depSvc *dependency.Service
}

func newServer(t *testing.T, caps rop.Capabilities) *server {
	t.Helper()
	return newServerAt(t, caps, filepath.Join(testutil.TempDirForDB(t), "t.db"))
}

func newServerAt(t *testing.T, caps rop.Capabilities, dbPath string) *server {
	t.Helper()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	if err := st.Migrate(ctx, filepath.Join(testutil.RepoRoot(), "migrations")); err != nil {
		t.Fatal(err)
	}
	clk := &clock.Fixed{T: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)}
	scope := "default"
	api := &API{Store: st, Clock: clk, Scope: scope, CreateTTL: time.Hour, UpdateTTL: time.Hour, NotifyTTL: time.Hour, ProviderID: "rop-demo"}
	ops, err := api.Operations()
	if err != nil {
		t.Fatal(err)
	}
	reg, err := operation.NewRegistry(ops...)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range ops {
		row := store.OperationRow{OperationID: o.ID, Reversibility: string(o.Reversibility), Guarantee: string(o.Guarantee)}
		if o.TTL != 0 {
			s := int64(o.TTL / time.Second)
			row.TTLSeconds = &s
		}
		if err := st.UpsertOperation(ctx, st.DB(), row); err != nil {
			t.Fatal(err)
		}
	}
	principal := authz.Principal{ID: "local", Scopes: map[string]bool{scope: true}}
	authorizer := authz.ScopeAllow{}
	depSvc := &dependency.Service{Store: st}
	svc := &reversal.Service{Store: st, Clock: clk, Registry: reg, Authorizer: authorizer, Dependencies: depSvc}
	pl := &planner.Service{Store: st, Clock: clk, Registry: reg, Authorizer: authorizer, Dependencies: depSvc}
	ver := &verification.Service{Store: st, Clock: clk, Registry: reg, Authorizer: authorizer}
	cfg := httpapi.Config{
		ProviderID: api.ProviderID, Clock: clk, Store: st, Registry: reg,
		Authorizer: authorizer, Reversal: svc, Planner: pl, Verification: ver,
		Capabilities: caps, Principal: principal, Scope: scope,
	}
	ts := httptest.NewServer(api.Handler(httpapi.Handler(cfg)))
	t.Cleanup(ts.Close)
	return &server{ts: ts, st: st, clk: clk, svc: svc, plan: pl, ver: ver,
		api: api, dbPath: dbPath, caps: caps, reg: reg, depSvc: depSvc}
}

// restart closes the database and reopens it, rebuilding every service on the
// new handle — simulating a process restart against the same persisted DB.
func (s *server) restart(t *testing.T) {
	t.Helper()
	if err := s.st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(s.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s.st = st
	s.api.Store = st
	s.depSvc = &dependency.Service{Store: st}
	s.svc = &reversal.Service{Store: st, Clock: s.clk, Registry: s.reg, Authorizer: s.svc.Authorizer, Dependencies: s.depSvc}
	s.plan = &planner.Service{Store: st, Clock: s.clk, Registry: s.reg, Authorizer: s.plan.Authorizer, Dependencies: s.depSvc}
	s.ver = &verification.Service{Store: st, Clock: s.clk, Registry: s.reg, Authorizer: s.ver.Authorizer}
	s.ts.Close()
	cfg := httpapi.Config{
		ProviderID: s.api.ProviderID, Clock: s.clk, Store: st, Registry: s.reg,
		Authorizer: s.svc.Authorizer, Reversal: s.svc, Planner: s.plan, Verification: s.ver,
		Capabilities: s.caps,
		Principal:    authz.Principal{ID: "local", Scopes: map[string]bool{"default": true}},
		Scope:        s.api.Scope,
	}
	s.ts = httptest.NewServer(s.api.Handler(httpapi.Handler(cfg)))
	t.Cleanup(s.ts.Close)
}

func (s *server) do(t *testing.T, method, path string, body string) (int, http.Header, map[string]any) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, s.ts.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp.StatusCode, resp.Header, m
}

func createResource(t *testing.T, s *server, value string) (string, string) {
	t.Helper()
	status, hdr, body := s.do(t, "POST", "/resources", `{"value":"`+value+`","unknownField":{"x":1}}`)
	if status != http.StatusCreated {
		t.Fatalf("create: status=%d body=%v", status, body)
	}
	actionID := hdr.Get("ROP-Action-ID")
	if actionID == "" || hdr.Get("ROP-Reversibility") != "REVERSIBLE" {
		t.Fatalf("missing receipt headers: %v", hdr)
	}
	resID, _ := body["id"].(string)
	if resID == "" {
		t.Fatalf("no resource id: %v", body)
	}
	return actionID, resID
}

// TestFirstVerticalSlice runs the approved M1 slice end to end:
// DISCOVER → EXECUTE → ACTION+RECEIPT → PLAN → REVERSE → VERIFY.
func TestFirstVerticalSlice(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})

	// DISCOVER
	status, _, disc := s.do(t, "GET", "/.well-known/rop", "")
	if status != 200 || disc["protocol"] != "rop" {
		t.Fatalf("discovery: %d %v", status, disc)
	}
	caps := disc["capabilities"].(map[string]any)
	for _, k := range []string{"receipts", "planning", "reversal", "verification"} {
		if caps[k] != true {
			t.Fatalf("capability %s not advertised: %v", k, caps)
		}
	}

	actionID, resID := createResource(t, s, "hello")

	// ACTION + RECEIPT
	status, _, receipt := s.do(t, "GET", "/.well-known/rop/actions/"+actionID, "")
	if status != 200 || receipt["status"] != "APPLIED" || receipt["reversibility"] != "REVERSIBLE" {
		t.Fatalf("receipt: %d %v", status, receipt)
	}
	if receipt["expiresAt"] == nil || receipt["providerId"] != "rop-demo" {
		t.Fatalf("receipt eligibility fields missing: %v", receipt)
	}

	// PLAN
	status, _, plan := s.do(t, "POST", "/.well-known/rop/actions/"+actionID+"/plan-reversal", "")
	if status != 200 {
		t.Fatalf("plan: %d %v", status, plan)
	}
	if plan["expectedReversal"] == "" || plan["basisResourceVersion"] == nil {
		t.Fatalf("plan freshness/preconditions missing: %v", plan)
	}

	// REVERSE (transport success; semantic outcome in body, §28)
	status, _, result := s.do(t, "POST", "/.well-known/rop/actions/"+actionID+"/reverse", "")
	if status != 200 || result["outcome"] != "REVERSED" || result["status"] != "REVERSED" {
		t.Fatalf("reverse: %d %v", status, result)
	}

	// The business row is gone, but the Action is not (I-1).
	if status, _, _ = s.do(t, "GET", "/resources/"+resID, ""); status != 404 {
		t.Fatalf("resource still present after reversal")
	}
	status, _, after := s.do(t, "GET", "/.well-known/rop/actions/"+actionID, "")
	if status != 200 || after["status"] != "REVERSED" {
		t.Fatalf("action after reversal: %d %v", status, after)
	}

	// VERIFY: provider-defined postconditions, independent of execution (I-10).
	status, _, ver := s.do(t, "GET", "/.well-known/rop/actions/"+actionID+"/verification", "")
	if status != 200 || ver["status"] != "VERIFIED" || ver["semantics"] != "LOCAL_READONLY" {
		t.Fatalf("verification: %d %v", status, ver)
	}
	pcs := ver["postconditions"].([]any)
	if len(pcs) != 1 || pcs[0].(map[string]any)["satisfied"] != true {
		t.Fatalf("postconditions: %v", pcs)
	}
}

func TestIrreversibleBoundary(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	_, resID := createResource(t, s, "doomed")

	status, hdr, body := s.do(t, "POST", "/resources/"+resID+"/publish", "")
	if status != 200 || hdr.Get("ROP-Reversibility") != "IRREVERSIBLE" {
		t.Fatalf("publish: %d %v %v", status, hdr, body)
	}
	pubAction := hdr.Get("ROP-Action-ID")

	// Crossing the boundary: reversal is refused (422 irreversible-action).
	status, _, problem := s.do(t, "POST", "/.well-known/rop/actions/"+pubAction+"/reverse", "")
	if status != 422 || problem["type"] != rop.ProblemIrreversible {
		t.Fatalf("reverse irreversible: %d %v", status, problem)
	}
	// No postconditions exist for irreversible operations.
	status, _, problem = s.do(t, "GET", "/.well-known/rop/actions/"+pubAction+"/verification", "")
	if status != 422 || problem["type"] != rop.ProblemIrreversible {
		t.Fatalf("verify irreversible: %d %v", status, problem)
	}
	// The resource survives; the create Action remains; a stale create
	// reversal now conflicts instead of destructively restoring (covered in
	// TestConflictOverDestructiveRestore).
	if status, _, _ = s.do(t, "GET", "/resources/"+resID, ""); status != 200 {
		t.Fatal("resource should still exist after publish")
	}
	_ = pubAction
}

func TestConflictOverDestructiveRestore(t *testing.T) {
	// Invariant I-7: reversal after concurrent mutation yields CONFLICT and
	// leaves the Action APPLIED — never a blind v3→v1 restore.
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	actionID, resID := createResource(t, s, "v1")
	// Simulate concurrent provider-side mutation by publishing (mutates state
	// without ROP reversal).
	if status, _, _ := s.do(t, "POST", "/resources/"+resID+"/publish", ""); status != 200 {
		t.Fatal("setup publish failed")
	}
	status, _, result := s.do(t, "POST", "/.well-known/rop/actions/"+actionID+"/reverse", "")
	if status != 200 || result["outcome"] != "CONFLICT" {
		t.Fatalf("expected CONFLICT outcome: %d %v", status, result)
	}
	status, _, after := s.do(t, "GET", "/.well-known/rop/actions/"+actionID, "")
	if status != 200 || after["status"] != "APPLIED" {
		t.Fatalf("action after conflict: %d %v (want APPLIED)", status, after)
	}
	// The resource survives untouched.
	if status, _, body := s.do(t, "GET", "/resources/"+resID, ""); status != 200 || body["value"] != "v1" {
		t.Fatalf("resource after conflict: %d %v", status, body)
	}
}

func TestExpiredActionLifecycle(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	actionID, _ := createResource(t, s, "ephemeral")
	// Server time past the eligibility window.
	s.clk.T = s.clk.T.Add(2 * time.Hour)
	status, _, receipt := s.do(t, "GET", "/.well-known/rop/actions/"+actionID, "")
	if status != 200 || receipt["status"] != "EXPIRED" {
		t.Fatalf("expired receipt: %d %v", status, receipt)
	}
	status, _, problem := s.do(t, "POST", "/.well-known/rop/actions/"+actionID+"/reverse", "")
	if status != 410 || problem["type"] != rop.ProblemReversalExpired {
		t.Fatalf("reverse expired: %d %v", status, problem)
	}
}

func TestReceiptsContainNoPrivateMaterial(t *testing.T) {
	// Invariant I-14: no public path exposes private reversal material.
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	actionID, _ := createResource(t, s, "secret")
	for _, path := range []string{
		"/.well-known/rop/actions/" + actionID,
		"/.well-known/rop/actions/" + actionID + "/verification",
	} {
		status, _, body := s.do(t, "GET", path, "")
		if status != 200 && status != 422 {
			t.Fatalf("%s: %d", path, status)
		}
		raw, _ := json.Marshal(body)
		for _, banned := range []string{"previousResourceVersion", "reversal_material", "material_json", "snapshot"} {
			if strings.Contains(string(raw), banned) {
				t.Errorf("%s leaks private material key %q: %s", path, banned, raw)
			}
		}
	}
	// Plans are public too.
	status, _, plan := s.do(t, "POST", "/.well-known/rop/actions/"+actionID+"/plan-reversal", "")
	if status != 200 {
		t.Fatal("plan failed")
	}
	raw, _ := json.Marshal(plan)
	if strings.Contains(string(raw), "previousResourceVersion") {
		t.Errorf("plan leaks material: %s", raw)
	}
}

func TestCapabilityStrippedServer(t *testing.T) {
	// capability-model.md §3 + OQ-11: a Core-only provider (no planning, no
	// reversal advertised) still serves Core and refuses optional endpoints
	// explicitly with 501 capability-unavailable.
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: false, Reversal: false, Verification: true})
	actionID, _ := createResource(t, s, "core-only")

	status, _, disc := s.do(t, "GET", "/.well-known/rop", "")
	caps := disc["capabilities"].(map[string]any)
	if caps["planning"] != false || caps["reversal"] != false {
		t.Fatalf("capabilities: %v", caps)
	}
	for _, path := range []string{
		"/.well-known/rop/actions/" + actionID + "/plan-reversal",
		"/.well-known/rop/actions/" + actionID + "/reverse",
	} {
		status, _, problem := s.do(t, "POST", path, "")
		if status != 501 || problem["type"] != rop.ProblemCapabilityUnavailable {
			t.Fatalf("%s: %d %v, want 501 capability-unavailable", path, status, problem)
		}
	}
	// Core status still works: eligibility per OQ-13 (b).
	status, _, receipt := s.do(t, "GET", "/.well-known/rop/actions/"+actionID, "")
	if status != 200 || receipt["status"] != "APPLIED" {
		t.Fatalf("core status on capability-stripped server: %d %v", status, receipt)
	}
}

func TestGoldenFixturesMatchServerShapes(t *testing.T) {
	// Master Prompt §22/§64: golden compatibility against frozen v0.1 Level 1
	// (Core) fixtures. Volatile fields (IDs, timestamps, attempt metadata) are
	// stripped; everything structural must match.
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	actionID, _ := createResource(t, s, "golden")
	_, _, result := s.do(t, "POST", "/.well-known/rop/actions/"+actionID+"/reverse", "")
	_ = result
	status, _, ver := s.do(t, "GET", "/.well-known/rop/actions/"+actionID+"/verification", "")
	if status != 200 {
		t.Fatal("verification failed")
	}
	status, _, receipt := s.do(t, "GET", "/.well-known/rop/actions/"+actionID, "")
	if status != 200 {
		t.Fatal("receipt failed")
	}

	strip := func(m map[string]any, keys ...string) map[string]any {
		for _, k := range keys {
			delete(m, k)
		}
		return m
	}
	wantReceipt := strip(fixture(t, "receipt-reversible.json"), "anUnknownOptionalField",
		"actionId", "providerId", "createdAt", "expiresAt", "operationId", "resourceRef")
	gotReceipt := strip(receipt, "actionId", "providerId", "createdAt", "expiresAt", "operationId", "resourceRef")
	// Status differs post-reversal (fixture freezes the APPLIED shape).
	gotReceipt["status"] = wantReceipt["status"]
	if !jsonDeepEqual(t, wantReceipt, gotReceipt) {
		t.Errorf("receipt shape drifted from fixture")
	}

	wantVer := strip(fixture(t, "verification-verified.json"), "actionId", "evaluatedAt")
	gotVer := strip(ver, "actionId", "evaluatedAt")
	if !jsonDeepEqual(t, wantVer, gotVer) {
		t.Errorf("verification shape drifted from fixture: want %v got %v", wantVer, gotVer)
	}

	// Problem shapes.
	s2 := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	aid2, _ := createResource(t, s2, "x")
	s2.clk.T = s2.clk.T.Add(2 * time.Hour)
	_, _, problem := s2.do(t, "POST", "/.well-known/rop/actions/"+aid2+"/reverse", "")
	wantProblem := strip(fixture(t, "problem-expired.json"), "detail")
	gotProblem := strip(problem, "detail")
	if !jsonDeepEqual(t, wantProblem, gotProblem) {
		t.Errorf("problem shape drifted from fixture: want %v got %v", wantProblem, gotProblem)
	}

	// Discovery: booleans and vocabulary (fixture's unknown capability key
	// must be ignored by our parser — evolution tolerance, §21).
	s3 := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	_, _, disc := s3.do(t, "GET", "/.well-known/rop", "")
	wantDisc := fixture(t, "discovery.json")
	delete(wantDisc["capabilities"].(map[string]any), "someFutureCapability")
	if !jsonDeepEqual(t, wantDisc, disc) {
		t.Errorf("discovery shape drifted from fixture: want %v got %v", wantDisc, disc)
	}
}

func jsonDeepEqual(t *testing.T, a, b map[string]any) bool {
	t.Helper()
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	// Compare via generic JSON value equality.
	var av, bv any
	_ = json.Unmarshal(ab, &av)
	_ = json.Unmarshal(bb, &bv)
	return jsonEqual(av, bv)
}

func jsonEqual(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			if !jsonEqual(v, bv[k]) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !jsonEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}
