package resourceapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/brilliantkid87/rop/pkg/rop"
)

// notifyResource performs the PARTIALLY_COMPENSATABLE scenario and returns
// the notify action ID.
func notifyResource(t *testing.T, s *server, resID, channel string) string {
	t.Helper()
	status, hdr, body := s.do(t, "POST", "/resources/"+resID+"/notify", `{"channel":"`+channel+`"}`)
	if status != http.StatusCreated {
		t.Fatalf("notify: status=%d body=%v", status, body)
	}
	actionID := hdr.Get("ROP-Action-ID")
	if actionID == "" || hdr.Get("ROP-Reversibility") != "PARTIALLY_COMPENSATABLE" {
		t.Fatalf("notify receipt headers: %v", hdr)
	}
	return actionID
}

// TestDependentBlocksReversal: Action B (an update) depends on Action A (the
// create); reversal of A is rejected with dependency-exists while B is
// active. ROP refuses — it never executes dependent reversals itself.
func TestDependentBlocksReversal(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	createAction, resID := createResource(t, s, "v1")
	_ = createAction
	updateResource(t, s, resID, "v2") // B depends on A

	status, _, problem := s.do(t, "POST", "/.well-known/rop/actions/"+findCreateAction(t, s, resID)+"/reverse", "")
	if status != http.StatusConflict || problem["type"] != rop.ProblemDependencyExists {
		t.Fatalf("blocked reversal: %d %v, want 409 dependency-exists", status, problem)
	}
	// The resource is untouched: refusal, not destructive restoration.
	if _, _, body := s.do(t, "GET", "/resources/"+resID, ""); body["value"] != "v2" {
		t.Fatalf("resource mutated despite block: %v", body)
	}
}

// findCreateAction locates the create Action for a resource via the DB.
func findCreateAction(t *testing.T, s *server, resID string) string {
	t.Helper()
	var id string
	if err := s.st.DB().QueryRow(`SELECT created_from_action FROM resources WHERE resource_id = ?`, resID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestResolvedDependentUnblocks: reversing B (its RESTORABLE restore) puts B
// into REVERSED — a resolved status — which stops blocking A per the
// documented active-dependent rule (OQ-15).
func TestResolvedDependentUnblocks(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	createAction, resID := createResource(t, s, "v1")
	createActionID := findCreateAction(t, s, resID)
	_ = createAction
	updateB := updateResource(t, s, resID, "v2")

	// While B is active, A is blocked.
	if status, _, _ := s.do(t, "POST", "/.well-known/rop/actions/"+createActionID+"/reverse", ""); status != http.StatusConflict {
		t.Fatalf("expected block, got status %d", status)
	}
	// Resolve B: its restoration succeeds (version still matches).
	if status, _, body := s.do(t, "POST", "/.well-known/rop/actions/"+updateB+"/reverse", ""); status != 200 || body["outcome"] != "REVERSED" {
		t.Fatalf("resolve B: %d %v", status, body)
	}
	// A is no longer blocked and reverses successfully.
	if status, _, body := s.do(t, "POST", "/.well-known/rop/actions/"+createActionID+"/reverse", ""); status != 200 || body["outcome"] != "REVERSED" {
		t.Fatalf("reverse A after resolving B: %d %v", status, body)
	}
	if status, _, _ := s.do(t, "GET", "/resources/"+resID, ""); status != 404 {
		t.Fatal("resource should be gone after create reversal")
	}
}

// TestDependencyAfterPlanConflicts: a dependency introduced after planning
// must not be bypassed by the stale plan — execution re-checks (invariant
// I-19), and the refreshed plan reports the blocker.
func TestDependencyAfterPlanConflicts(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	createActionID, resID := createResource(t, s, "v1")
	createActionID = findCreateAction(t, s, resID)

	// Plan while unblocked.
	status, _, plan := s.do(t, "POST", "/.well-known/rop/actions/"+createActionID+"/plan-reversal", "")
	if status != 200 || plan["blockingDependencies"] != nil {
		t.Fatalf("plan before dependency: %d %v", status, plan)
	}
	// Dependency appears after planning.
	updateResource(t, s, resID, "v2")

	// The stale plan does not authorize reversal.
	status, _, problem := s.do(t, "POST", "/.well-known/rop/actions/"+createActionID+"/reverse", "")
	if status != http.StatusConflict || problem["type"] != rop.ProblemDependencyExists {
		t.Fatalf("post-plan dependency block: %d %v", status, problem)
	}
	// A refreshed plan exposes the blocker.
	status, _, plan = s.do(t, "POST", "/.well-known/rop/actions/"+createActionID+"/plan-reversal", "")
	if status != 200 {
		t.Fatal("refreshed plan failed")
	}
	deps, _ := plan["blockingDependencies"].([]any)
	if len(deps) != 1 {
		t.Fatalf("refreshed plan must expose blocking dependency: %v", plan)
	}
}

// TestPartialCompensableScenario covers the required M5 chain end to end:
// genuine partial compensation, PARTIALLY_REVERSED outcome, durable residue
// (declared + discovered), partial verification without pretending full
// reversal, and append-preserving history.
func TestPartialCompensableScenario(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	_, resID := createResource(t, s, "notify-me")
	notifyAction := notifyResource(t, s, resID, "email")

	// Receipt: PARTIALLY_COMPENSATABLE with declared residue visible.
	status, _, receipt := s.do(t, "GET", "/.well-known/rop/actions/"+notifyAction, "")
	if status != 200 || receipt["reversibility"] != "PARTIALLY_COMPENSATABLE" || receipt["guarantee"] != "BEST_EFFORT" {
		t.Fatalf("notify receipt: %d %v", status, receipt)
	}
	residue, _ := receipt["residue"].([]any)
	if len(residue) != 1 {
		t.Fatalf("declared residue must be visible: %v", receipt)
	}

	// Known residue appears in planning.
	status, _, plan := s.do(t, "POST", "/.well-known/rop/actions/"+notifyAction+"/plan-reversal", "")
	if status != 200 {
		t.Fatal("plan failed")
	}
	planResidue, _ := plan["residue"].([]any)
	if len(planResidue) != 1 || !strings.Contains(plan["expectedReversal"].(string), "withdraw") {
		t.Fatalf("plan must expose known residue and partial expectation: %v", plan)
	}

	// Reversal: compensates effect A (withdraw), cannot remove effect B
	// (immutable delivery record) — PARTIALLY_REVERSED with residue.
	status, _, result := s.do(t, "POST", "/.well-known/rop/actions/"+notifyAction+"/reverse", "")
	if status != 200 || result["outcome"] != "PARTIALLY_REVERSED" || result["status"] != "PARTIALLY_REVERSED" {
		t.Fatalf("partial reversal: %d %v", status, result)
	}

	// Effect A really was compensated in provider state.
	if _, _, body := s.do(t, "GET", "/resources/"+resID, ""); body["value"] != "notify-me" {
		t.Fatalf("resource changed by notify reversal: %v", body)
	}
	var ntfStatus string
	if err := s.st.DB().QueryRow(`SELECT status FROM notifications WHERE created_from_action = ?`, notifyAction).Scan(&ntfStatus); err != nil {
		t.Fatal(err)
	}
	if ntfStatus != "WITHDRAWN" {
		t.Fatalf("notification status = %s, want WITHDRAWN (effect A compensated)", ntfStatus)
	}

	// Residue durably recorded: DECLARED before reversal + DISCOVERED during
	// it — append-style history, both preserved.
	var residueRows int
	if err := s.st.DB().QueryRow(`SELECT COUNT(DISTINCT source) FROM action_residue WHERE action_id = ?`, notifyAction).Scan(&residueRows); err != nil {
		t.Fatal(err)
	}
	if residueRows != 2 {
		t.Fatalf("residue sources = %d, want 2 (DECLARED + DISCOVERED)", residueRows)
	}

	// Partial verification: postcondition holds (withdrawn + delivery record
	// remains) WITHOUT pretending full reversal.
	status, _, ver := s.do(t, "GET", "/.well-known/rop/actions/"+notifyAction+"/verification", "")
	if status != 200 || ver["status"] != "VERIFIED" {
		t.Fatalf("partial verification: %d %v", status, ver)
	}
	for _, p := range ver["postconditions"].([]any) {
		if p.(map[string]any)["satisfied"] != true {
			t.Fatalf("partial postcondition unsatisfied: %v", p)
		}
	}
	status, _, after := s.do(t, "GET", "/.well-known/rop/actions/"+notifyAction, "")
	if after["status"] != "PARTIALLY_REVERSED" {
		t.Fatalf("verification must not upgrade the outcome: %v", after)
	}

	// History append-preserving: APPLIED → REVERSING → PARTIALLY_REVERSED.
	var hist []string
	rows, err := s.st.DB().Query(`SELECT to_status FROM action_status_history WHERE action_id = ? ORDER BY seq`, notifyAction)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		_ = rows.Scan(&v)
		hist = append(hist, v)
	}
	if len(hist) != 3 || hist[0] != "APPLIED" || hist[1] != "REVERSING" || hist[2] != "PARTIALLY_REVERSED" {
		t.Fatalf("history = %v", hist)
	}
}

// TestResidueVisibleAfterRestart: residue is durable and remains visible on
// the receipt after a full restart (M5 requirement).
func TestResidueVisibleAfterRestart(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	_, resID := createResource(t, s, "persist-residue")
	notifyAction := notifyResource(t, s, resID, "email")
	if _, _, _ = s.do(t, "POST", "/.well-known/rop/actions/"+notifyAction+"/reverse", ""); true {
	}
	s.restart(t)
	status, _, receipt := s.do(t, "GET", "/.well-known/rop/actions/"+notifyAction, "")
	if status != 200 || receipt["status"] != "PARTIALLY_REVERSED" {
		t.Fatalf("receipt after restart: %d %v", status, receipt)
	}
	// Both lifecycle stages survive: the DECLARED item (known before
	// reversal) and the DISCOVERED item (recorded during it) — append-style
	// history, deduplicated by description only.
	residue, _ := receipt["residue"].([]any)
	if len(residue) != 2 {
		t.Fatalf("residue after restart = %d entries, want 2 (DECLARED + DISCOVERED): %v", len(residue), receipt)
	}
	for _, r := range residue {
		desc, _ := r.(map[string]any)["description"].(string)
		if !strings.Contains(desc, "immutable delivery") {
			t.Fatalf("residue description: %v", desc)
		}
	}
	// And the verification evidence is still available.
	if status, _, ver := s.do(t, "GET", "/.well-known/rop/actions/"+notifyAction+"/verification", ""); status != 200 || ver["status"] != "VERIFIED" {
		t.Fatalf("verification after restart: %d %v", status, ver)
	}
}

// TestDependencyStateSurvivesRestart: dependency edges are durable and keep
// blocking after restart (M5 requirement).
func TestDependencyStateSurvivesRestart(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	_, resID := createResource(t, s, "v1")
	createActionID := findCreateAction(t, s, resID)
	updateResource(t, s, resID, "v2") // B depends on A

	s.restart(t)

	status, _, problem := s.do(t, "POST", "/.well-known/rop/actions/"+createActionID+"/reverse", "")
	if status != http.StatusConflict || problem["type"] != rop.ProblemDependencyExists {
		t.Fatalf("dependency block lost across restart: %d %v", status, problem)
	}
}

// TestNoPartialLabelOnFullReversal guards against enum-faking: an ordinary
// successful REVERSIBLE create reversal must NOT produce PARTIALLY_REVERSED
// or residue (Master Prompt: do not fake partial compensation).
func TestNoPartialLabelOnFullReversal(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	createAction, resID := createResource(t, s, "clean")
	createActionID := findCreateAction(t, s, resID)
	_ = createAction
	if status, _, body := s.do(t, "POST", "/.well-known/rop/actions/"+createActionID+"/reverse", ""); status != 200 || body["outcome"] != "REVERSED" {
		t.Fatalf("clean reversal: %d %v", status, body)
	}
	status, _, receipt := s.do(t, "GET", "/.well-known/rop/actions/"+createActionID, "")
	if status != 200 || receipt["status"] != "REVERSED" || receipt["residue"] != nil {
		t.Fatalf("full reversal must carry no residue: %d %v", status, receipt)
	}
	var rows int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM action_residue WHERE action_id = ?`, createActionID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("residue rows = %d, want 0", rows)
	}
}
