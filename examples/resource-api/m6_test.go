package resourceapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/brilliantkid87/rop/pkg/rop"
)

// TestMalformedActionIDsAreSafe is the M6 security check: hostile Action IDs
// (SQL-ish strings, path traversal, huge input) must never confuse the store
// (all queries are parameterized), must never leak existence, and must
// return the stable not-found problem (docs/security.md M6 second pass).
func TestMalformedActionIDsAreSafe(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	hostile := []string{
		"act_' OR 1=1--",
		"act_1; DROP TABLE actions;--",
		"../../etc/passwd",
		"act_%00",
		strings.Repeat("act_", 1000),
		"act_\x00\x01\x02",
		"act_UNION SELECT material_json FROM reversal_material--",
		"日本語-emoji-🔑",
	}
	// A real Action with real private material exists, so a genuine leak
	// (rather than reflection of the attacker's own input) would surface
	// actual stored values.
	_, realRes := createResource(t, s, "v1-value")
	_ = realRes
	probeLeaks := []string{"previousValue", "v1-value", "previousResourceVersion"}
	for _, id := range hostile {
		for _, path := range []string{
			"/.well-known/rop/actions/" + id,
			"/.well-known/rop/actions/" + id + "/verification",
		} {
			req, err := http.NewRequest("GET", s.ts.URL+path, nil)
			if err != nil {
				// Transport-level rejection: control characters cannot even
				// form a URL. Safe by construction.
				continue
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("hostile id %q: transport error %v", truncate(id), err)
			}
			var body map[string]any
			_ = json.NewDecoder(resp.Body).Decode(&body)
			resp.Body.Close()
			status := resp.StatusCode
			// Safe outcomes: 400/404 problem responses; 301 when net/http
			// normalizes a traversal attempt away.
			switch status {
			case http.StatusBadRequest, http.StatusNotFound, http.StatusMovedPermanently:
			default:
				t.Errorf("hostile id %q: status %d body %v", truncate(id), status, body)
			}
			if status == http.StatusNotFound && body != nil && body["type"] != rop.ProblemActionNotFound {
				t.Errorf("hostile id %q: unexpected problem %v", truncate(id), body)
			}
			// No existence leak, no internal detail leak: the only echoing is
			// the attacker's own (sanitized) input; actual stored material
			// values must never appear.
			if detail, _ := body["detail"].(string); len(detail) > 256 {
				t.Errorf("hostile id %q: detail exceeds sanitize bound", truncate(id))
			}
			for _, leak := range probeLeaks {
				if d, _ := body["detail"].(string); d != id && strings.Contains(d, leak) {
					t.Errorf("hostile id %q leaked internal material %q: %v", truncate(id), leak, body)
				}
			}
		}
	}
	// The store is intact: an honest Action still resolves.
	actionID, _ := createResource(t, s, "still-alive")
	if status, _, _ := s.do(t, "GET", "/.well-known/rop/actions/"+actionID, ""); status != 200 {
		t.Fatalf("store corrupted by hostile IDs: status %d", status)
	}
}

func truncate(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

// TestResidueDescriptionsCarryNoBusinessPayloads is the M6 review check: the
// demo provider's residue descriptions are concrete provider facts, never
// serialized business payloads (Master Prompt §14; security T16).
func TestResidueDescriptionsCarryNoBusinessPayloads(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	secret := "super-secret-resource-body-42"
	_, resID := createResource(t, s, secret)
	notifyAction := notifyResource(t, s, resID, "email")
	_, _, _ = s.do(t, "POST", "/.well-known/rop/actions/"+notifyAction+"/reverse", "")

	for _, path := range []string{
		"/.well-known/rop/actions/" + notifyAction,
		"/.well-known/rop/actions/" + notifyAction + "/plan-reversal",
	} {
		method := "GET"
		if strings.HasSuffix(path, "plan-reversal") {
			method = "POST"
		}
		status, _, body := s.do(t, method, path, "")
		if status != 200 {
			t.Fatalf("%s: %d", path, status)
		}
		raw, _ := json.Marshal(body)
		if strings.Contains(string(raw), secret) {
			t.Errorf("%s: residue serializes business payload %q", path, secret)
		}
	}
}

// TestReconciliationReferenceIsNotAnAuthorizationToken: the durable
// execution identity (rop-rev-<attemptId>) is recorded evidence, never a
// credential — it appears on receipts/results but grants nothing by
// possession (docs/security.md M6 second pass).
func TestReconciliationReferenceIsNotAnAuthorizationToken(t *testing.T) {
	s := newServer(t, rop.Capabilities{Receipts: true, Planning: true, Reversal: true, Verification: true})
	_, resID := createResource(t, s, "v1")
	updateAction := updateResource(t, s, resID, "v2")
	_, _, result := s.do(t, "POST", "/.well-known/rop/actions/"+updateAction+"/reverse", "")
	providerRef, _ := result["providerRef"].(string)
	if providerRef == "" {
		t.Fatal("expected a provider execution identity on the result")
	}
	// Presenting the identity in place of authorization changes nothing: the
	// principal in the reference server already has permission, so assert the
	// identity appears nowhere in an authorization decision — i.e. a denied
	// principal with the identity is still denied (covered at service level
	// in TestActionIDIsNotAuthorization); here we assert the identity alone
	// cannot discover cross-scope Actions.
	_, _, probe := s.do(t, "GET", "/.well-known/rop/actions/"+providerRef, "")
	if probe["type"] != rop.ProblemActionNotFound {
		t.Fatalf("execution identity must not resolve to an Action via lookup: %v", probe)
	}
}
