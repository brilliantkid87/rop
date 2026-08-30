package resourceapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/brilliantkid87/rop/pkg/rop"
)

// helper: perform a RESTORABLE update, return (updateActionID, resourceID).
func updateResource(t *testing.T, s *server, resID, value string) string {
	t.Helper()
	status, hdr, body := s.do(t, "PATCH", "/resources/"+resID, `{"value":"`+value+`"}`)
	if status != http.StatusOK {
		t.Fatalf("update: status=%d body=%v", status, body)
	}
	actionID := hdr.Get("ROP-Action-ID")
	if actionID == "" || hdr.Get("ROP-Reversibility") != "RESTORABLE" {
		t.Fatalf("update receipt headers missing: %v", hdr)
	}
	return actionID
}

// TestSuccessfulRestorableUpdate covers: RESTORABLE update applies, records an
// Action with an eligibility window, and the receipt exposes only capability
// metadata (never the prior state).
func TestSuccessfulRestorableUpdate(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	createAction, resID := createResource(t, s, "v1-value")
	_ = createAction

	updateAction := updateResource(t, s, resID, "v2-value")

	// Business state mutated for real (persisted, versioned).
	status, _, body := s.do(t, "GET", "/resources/"+resID, "")
	if status != 200 || body["value"] != "v2-value" || body["version"] != float64(2) {
		t.Fatalf("resource after update: %d %v", status, body)
	}
	// Receipt: RESTORABLE class, live status, window — no prior state.
	status, _, receipt := s.do(t, "GET", "/.well-known/rop/actions/"+updateAction, "")
	if status != 200 {
		t.Fatalf("receipt: %d %v", status, receipt)
	}
	if receipt["reversibility"] != "RESTORABLE" || receipt["status"] != "APPLIED" || receipt["expiresAt"] == nil {
		t.Fatalf("update receipt fields: %v", receipt)
	}
}

// TestRestoreSucceedsAtExpectedVersion covers: successful restore when the
// expected version still matches — value and version are restored, and the
// original Action history remains intact (invariant I-1).
func TestRestoreSucceedsAtExpectedVersion(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	_, resID := createResource(t, s, "v1-value")
	updateAction := updateResource(t, s, resID, "v2-value")

	status, _, result := s.do(t, "POST", "/.well-known/rop/actions/"+updateAction+"/reverse", "")
	if status != 200 || result["outcome"] != "REVERSED" {
		t.Fatalf("restore: %d %v", status, result)
	}
	// Provider-defined restoration semantics: prior value AND prior version.
	status, _, body := s.do(t, "GET", "/resources/"+resID, "")
	if status != 200 || body["value"] != "v1-value" || body["version"] != float64(1) {
		t.Fatalf("resource after restore: %d %v", status, body)
	}

	// Original Action history intact: APPLIED → REVERSING → REVERSED.
	status, _, after := s.do(t, "GET", "/.well-known/rop/actions/"+updateAction, "")
	if status != 200 || after["status"] != "REVERSED" {
		t.Fatalf("update action after restore: %d %v", status, after)
	}
	var hist []string
	rows, err := s.st.DB().Query(`SELECT to_status FROM action_status_history WHERE action_id = ? ORDER BY seq`, updateAction)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		_ = rows.Scan(&v)
		hist = append(hist, v)
	}
	if len(hist) != 3 || hist[0] != "APPLIED" || hist[1] != "REVERSING" || hist[2] != "REVERSED" {
		t.Fatalf("update action history = %v, want [APPLIED REVERSING REVERSED]", hist)
	}

	// Verification of the provider-defined restoration postconditions.
	status, _, ver := s.do(t, "GET", "/.well-known/rop/actions/"+updateAction+"/verification", "")
	if status != 200 || ver["status"] != "VERIFIED" {
		t.Fatalf("verification after restore: %d %v", status, ver)
	}
	for _, p := range ver["postconditions"].([]any) {
		if p.(map[string]any)["satisfied"] != true {
			t.Fatalf("postcondition unsatisfied: %v", p)
		}
	}
}

// TestStalePlanRejection covers plan freshness (Master Prompt §40, §41): a
// plan generated against basis version N must not authorize reversal after
// the resource moved to version N+1 — the plan itself reports the staleness,
// and execution conflicts regardless of the plan.
func TestStalePlanRejection(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	_, resID := createResource(t, s, "v1-value")
	updateA := updateResource(t, s, resID, "v2-value") // A: v1 -> v2

	// Plan for reversing A, computed while the resource is at v2.
	status, _, plan := s.do(t, "POST", "/.well-known/rop/actions/"+updateA+"/plan-reversal", "")
	if status != 200 {
		t.Fatalf("plan: %d %v", status, plan)
	}
	if plan["basisResourceVersion"] != float64(2) {
		t.Fatalf("plan basis version: %v", plan["basisResourceVersion"])
	}

	// Concurrent mutation: B moves the resource v2 -> v3.
	updateB := updateResource(t, s, resID, "v3-value")

	// The old plan is now stale: it still says basis v2. It is not
	// authorization — replaying it changes nothing; only execution counts.
	status, _, result := s.do(t, "POST", "/.well-known/rop/actions/"+updateA+"/reverse", "")
	if status != 200 || result["outcome"] != "CONFLICT" {
		t.Fatalf("stale-plan reversal must conflict: %d %v", status, result)
	}
	_ = updateB

	// The resource is untouched at v3 with B's value — never blindly v2->v1.
	status, _, body := s.do(t, "GET", "/resources/"+resID, "")
	if status != 200 || body["value"] != "v3-value" || body["version"] != float64(3) {
		t.Fatalf("resource after refused restore: %d %v", status, body)
	}
	// Action A is back to APPLIED (reversal refused without side effects, I-7).
	status, _, after := s.do(t, "GET", "/.well-known/rop/actions/"+updateA, "")
	if status != 200 || after["status"] != "APPLIED" {
		t.Fatalf("action A after conflict: %d %v", status, after)
	}
}

// TestConflictScenarioAthenB demonstrates the mandated case exactly:
//   - Action A: v1 -> v2
//   - Action B: v2 -> v3
//   - reversing Action A MUST NOT silently produce v1; it rejects safely.
func TestConflictScenarioAthenB(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	_, resID := createResource(t, s, "state@v1")
	actionA := updateResource(t, s, resID, "state@v2") // A: v1 -> v2
	actionB := updateResource(t, s, resID, "state@v3") // B: v2 -> v3

	// Reverse A: must refuse (CONFLICT), not restore v1.
	status, _, result := s.do(t, "POST", "/.well-known/rop/actions/"+actionA+"/reverse", "")
	if status != 200 || result["outcome"] != "CONFLICT" {
		t.Fatalf("reversing A after B must conflict: %d %v", status, result)
	}
	if msg, _ := result["error"].(string); !strings.Contains(msg, "concurrent mutation") {
		t.Fatalf("conflict should diagnose concurrent mutation: %v", result)
	}
	status, _, body := s.do(t, "GET", "/resources/"+resID, "")
	if body["value"] != "state@v3" || body["version"] != float64(3) {
		t.Fatalf("v1 must NOT appear: %v", body)
	}
	_ = actionB

	// Reverse B: B's restoration expects v3 == version B produced; restoring
	// B brings back A's value at v2 — a valid, provable restoration.
	status, _, resultB := s.do(t, "POST", "/.well-known/rop/actions/"+actionB+"/reverse", "")
	if status != 200 || resultB["outcome"] != "REVERSED" {
		t.Fatalf("reversing B must succeed: %d %v", status, resultB)
	}
	status, _, body = s.do(t, "GET", "/resources/"+resID, "")
	if body["value"] != "state@v2" || body["version"] != float64(2) {
		t.Fatalf("resource after restoring B: %v", body)
	}
	// Now A's restoration expects v2 — which holds — so A restores to v1.
	status, _, resultA := s.do(t, "POST", "/.well-known/rop/actions/"+actionA+"/reverse", "")
	if status != 200 || resultA["outcome"] != "REVERSED" {
		t.Fatalf("reversing A after B restored: %d %v", status, resultA)
	}
	status, _, body = s.do(t, "GET", "/resources/"+resID, "")
	if body["value"] != "state@v1" || body["version"] != float64(1) {
		t.Fatalf("resource after restoring A: %v", body)
	}
}

// TestConcurrentMutationConflict checks the CAS guard directly: any mutation
// between the update and the reversal (here publish, which bumps the version)
// makes restoration a conflict, never a destructive overwrite.
func TestConcurrentMutationConflict(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	_, resID := createResource(t, s, "v1-value")
	updateAction := updateResource(t, s, resID, "v2-value")
	if status, _, _ := s.do(t, "POST", "/resources/"+resID+"/publish", ""); status != 200 {
		t.Fatal("setup publish failed")
	}
	status, _, result := s.do(t, "POST", "/.well-known/rop/actions/"+updateAction+"/reverse", "")
	if status != 200 || result["outcome"] != "CONFLICT" {
		t.Fatalf("restore after publish must conflict: %d %v", status, result)
	}
	status, _, body := s.do(t, "GET", "/resources/"+resID, "")
	if body["value"] != "v2-value" {
		t.Fatalf("published value must be untouched: %v", body)
	}
	// Verification honestly reports FAILED: the restore did not happen.
	status, _, ver := s.do(t, "GET", "/.well-known/rop/actions/"+updateAction+"/verification", "")
	if status != 200 || ver["status"] != "FAILED" {
		t.Fatalf("verification of unrestored state: %d %v", status, ver)
	}
}

// TestPriorStateMaterialRemainsPrivate extends invariant I-14 to the new
// prior-state material: previousValue / previousVersion / expectedVersion must
// not appear on any public path.
func TestPriorStateMaterialRemainsPrivate(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	_, resID := createResource(t, s, "v1-secret-value")
	updateAction := updateResource(t, s, resID, "v2-public-value")

	paths := map[string]string{
		"status":       "/.well-known/rop/actions/" + updateAction,
		"plan":         "/.well-known/rop/actions/" + updateAction + "/plan-reversal",
		"verification": "/.well-known/rop/actions/" + updateAction + "/verification",
	}
	for name, path := range paths {
		method := "GET"
		if name == "plan" {
			method = "POST"
		}
		status, _, body := s.do(t, method, path, "")
		if status != 200 {
			t.Fatalf("%s: %d", name, status)
		}
		raw, _ := json.Marshal(body)
		for _, banned := range []string{"v1-secret-value", "previousValue", "previousVersion", "expectedVersion", "material"} {
			if strings.Contains(string(raw), banned) {
				t.Errorf("%s path leaks private material key %q: %s", name, banned, raw)
			}
		}
	}
	// The private material IS stored provider-side and survives (needed for
	// safe reversal), but only in reversal_material.
	var mat string
	if err := s.st.DB().QueryRow(`SELECT material_json FROM reversal_material WHERE action_id = ?`,
		updateAction).Scan(&mat); err != nil {
		t.Fatalf("prior-state material must be persisted: %v", err)
	}
	for _, want := range []string{"v1-secret-value", "previousValue", "expectedVersion"} {
		if !strings.Contains(mat, want) {
			t.Errorf("material missing %q: %s", want, mat)
		}
	}
}

// TestRestartPreservesRestorationMaterial covers the restart contract for
// RESTORABLE semantics: after closing and reopening the database, the private
// prior-state material is intact and restoration still succeeds (invariant
// I-11 for M2 scope).
func TestRestartPreservesRestorationMaterial(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	_, resID := createResource(t, s, "v1-value")
	updateAction := updateResource(t, s, resID, "v2-value")

	// Restart: close the store, reopen the same database file, rebuild all
	// services and the HTTP surface on the new handles.
	s.restart(t)

	// Action and material survived the restart.
	status, _, receipt := s.do(t, "GET", "/.well-known/rop/actions/"+updateAction, "")
	if status != 200 || receipt["status"] != "APPLIED" || receipt["reversibility"] != "RESTORABLE" {
		t.Fatalf("receipt after restart: %d %v", status, receipt)
	}
	status, _, result := s.do(t, "POST", "/.well-known/rop/actions/"+updateAction+"/reverse", "")
	if status != 200 || result["outcome"] != "REVERSED" {
		t.Fatalf("restore after restart: %d %v", status, result)
	}
	status, _, body := s.do(t, "GET", "/resources/"+resID, "")
	if body["value"] != "v1-value" || body["version"] != float64(1) {
		t.Fatalf("resource after post-restart restore: %v", body)
	}
}
