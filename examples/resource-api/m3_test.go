package resourceapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/brilliantkid87/rop/pkg/rop"
)

// doKey issues a request with an optional Idempotency-Key header.
func (s *server) doKey(t *testing.T, method, path, key string) (int, http.Header, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, s.ts.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, resp.Header, m
}

// TestHTTPIdempotentRetryAfterLostResponse simulates the canonical lost-
// response case (Master Prompt §36): the client issued a reversal, the HTTP
// response vanished, and the client retries with the same key. The retry must
// return the recorded result — one reversal execution total.
func TestHTTPIdempotentRetryAfterLostResponse(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	_, resID := createResource(t, s, "v1-value")
	updateAction := updateResource(t, s, resID, "v2-value")

	// First request (its response is "lost" — we ignore it entirely).
	status1, _, body1 := s.doKey(t, "POST", "/.well-known/rop/actions/"+updateAction+"/reverse", "client-key-42")
	if status1 != 200 || body1["outcome"] != "REVERSED" {
		t.Fatalf("first: %d %v", status1, body1)
	}
	// Retry with the same key after the "lost" response.
	status2, _, body2 := s.doKey(t, "POST", "/.well-known/rop/actions/"+updateAction+"/reverse", "client-key-42")
	if status2 != 200 || body2["outcome"] != "REVERSED" {
		t.Fatalf("retry: %d %v", status2, body2)
	}
	if body1["attemptId"] != body2["attemptId"] {
		t.Fatalf("retry produced a different attempt: %v vs %v", body1["attemptId"], body2["attemptId"])
	}
	// Exactly one effective reversal execution: one attempt row, one history
	// transition sequence, resource restored once.
	var attempts, reversing int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM reversal_attempts WHERE action_id = ?`, updateAction).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM action_status_history WHERE action_id = ? AND to_status = 'REVERSING'`, updateAction).Scan(&reversing); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || reversing != 1 {
		t.Fatalf("attempts=%d REVERSING transitions=%d, want 1/1", attempts, reversing)
	}
	// The restoration is complete and verifiable.
	status, _, ver := s.do(t, "GET", "/.well-known/rop/actions/"+updateAction+"/verification", "")
	if status != 200 || ver["status"] != "VERIFIED" {
		t.Fatalf("verification: %d %v", status, ver)
	}
}

// TestHTTPIdempotencyConflictOnDifferentAction: the same key aimed at a
// different Action in the same scope is materially different request
// semantics and MUST be rejected, not executed.
func TestHTTPIdempotencyConflictOnDifferentAction(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	_, res1 := createResource(t, s, "one")
	_, res2 := createResource(t, s, "two")
	a1 := updateResource(t, s, res1, "one-v2")
	a2 := updateResource(t, s, res2, "two-v2")

	if status, _, _ := s.doKey(t, "POST", "/.well-known/rop/actions/"+a1+"/reverse", "shared"); status != 200 {
		t.Fatal("first reversal failed")
	}
	status, _, problem := s.doKey(t, "POST", "/.well-known/rop/actions/"+a2+"/reverse", "shared")
	if status != http.StatusConflict || problem["type"] != rop.ProblemIdempotencyConflict {
		t.Fatalf("reused key: %d %v, want 409 idempotency-key-conflict", status, problem)
	}
	// Action 2 is untouched and still reversible with its own key.
	status, _, receipt := s.do(t, "GET", "/.well-known/rop/actions/"+a2, "")
	if status != 200 || receipt["status"] != "APPLIED" {
		t.Fatalf("action 2 state: %d %v", status, receipt)
	}
	if status, _, body := s.doKey(t, "POST", "/.well-known/rop/actions/"+a2+"/reverse", "other-key"); status != 200 || body["outcome"] != "REVERSED" {
		t.Fatalf("own-key reversal: %d %v", status, body)
	}
}

// TestInspectionAndVerificationRemainAvailableAfterExpiry: expiry stops new
// reversals, not visibility or honest verification (M3 semantics).
func TestInspectionAndVerificationRemainAvailableAfterExpiry(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	_, resID := createResource(t, s, "v1-value")
	updateAction := updateResource(t, s, resID, "v2-value")

	s.clk.T = s.clk.T.Add(2 * time.Hour) // past the eligibility window

	// Inspectable: EXPIRED status with the receipt fields intact.
	status, _, receipt := s.do(t, "GET", "/.well-known/rop/actions/"+updateAction, "")
	if status != 200 || receipt["status"] != "EXPIRED" {
		t.Fatalf("expired receipt: %d %v", status, receipt)
	}
	// New reversal rejected at the exact server-time boundary rule.
	status, _, problem := s.doKey(t, "POST", "/.well-known/rop/actions/"+updateAction+"/reverse", "")
	if status != http.StatusGone || problem["type"] != rop.ProblemReversalExpired {
		t.Fatalf("reverse expired: %d %v", status, problem)
	}
	// Planning remains available and reports the stale state with conflicts.
	status, _, plan := s.do(t, "POST", "/.well-known/rop/actions/"+updateAction+"/plan-reversal", "")
	if status != 200 || plan["currentStatus"] != "EXPIRED" {
		t.Fatalf("plan after expiry: %d %v", status, plan)
	}
	if conflicts, _ := plan["conflicts"].([]any); len(conflicts) == 0 {
		t.Fatalf("plan after expiry must carry conflicts: %v", plan)
	}
	// Verification remains available and honest: the resource still exists,
	// so the restoration postconditions are FAILED (not UNKNOWN, not hidden).
	status, _, ver := s.do(t, "GET", "/.well-known/rop/actions/"+updateAction+"/verification", "")
	if status != 200 || ver["status"] != "FAILED" {
		t.Fatalf("verification after expiry: %d %v", status, ver)
	}
	// The business state is untouched by all of the above.
	if _, _, body := s.do(t, "GET", "/resources/"+resID, ""); body["value"] != "v2-value" {
		t.Fatalf("resource mutated after expiry sweep: %v", body)
	}
	if !strings.Contains(receipt["expiresAt"].(string), "Z") {
		t.Fatalf("expiresAt not RFC 3339: %v", receipt["expiresAt"])
	}
}
