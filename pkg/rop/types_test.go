package rop

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEnumKnown(t *testing.T) {
	for _, v := range []Reversibility{ReversibilityREVERSIBLE, ReversibilityRESTORABLE, ReversibilityCOMPENSATABLE, ReversibilityPARTIALLY_COMPENSATABLE, ReversibilityIRREVERSIBLE} {
		if !v.Known() {
			t.Errorf("expected known: %s", v)
		}
	}
	for _, g := range []Guarantee{GuaranteeEXACT, GuaranteeEVENTUAL, GuaranteeBEST_EFFORT, GuaranteeMANUAL, GuaranteeNONE} {
		if !g.Known() {
			t.Errorf("expected known: %s", g)
		}
	}
}

// TestUnknownEnumStaysUnknown is the core evolution-tolerance rule (Master
// Prompt §21): an unknown value is never coerced into a known one.
func TestUnknownEnumStaysUnknown(t *testing.T) {
	var r Reversibility = "SELF_DESTRUCTS"
	if r.Known() {
		t.Fatal("unknown value reported as known")
	}
	var payload = []byte(`{"reversibility":"SHAZAM","guarantee":"MOSTLY","status":"MAYBE"}`)
	var doc struct {
		Reversibility Reversibility `json:"reversibility"`
		Guarantee     Guarantee     `json:"guarantee"`
		Status        Status        `json:"status"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Reversibility.Known() || doc.Guarantee.Known() {
		t.Fatal("unknown enum values were coerced")
	}
	if doc.Reversibility != "SHAZAM" {
		t.Fatalf("unknown value not preserved: %q", doc.Reversibility)
	}
}

// TestReceiptIgnoresUnknownFields checks the fixture-compatible evolution
// rule: unknown optional fields are ignored on decode (Master Prompt §21).
func TestReceiptIgnoresUnknownFields(t *testing.T) {
	payload := []byte(`{
		"actionId":"act_x",
		"providerId":"p",
		"operationId":"op",
		"createdAt":"2026-08-30T00:00:00Z",
		"resourceRef":{"resourceType":"resource","resourceId":"res_x","extra":"ignored"},
		"reversibility":"REVERSIBLE",
		"guarantee":"EXACT",
		"status":"APPLIED",
		"futureNested":{"unknown":[1,2,3]}
	}`)
	var rec Receipt
	if err := json.Unmarshal(payload, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.ActionID != "act_x" || rec.Reversibility != ReversibilityREVERSIBLE {
		t.Fatalf("unexpected decode: %+v", rec)
	}
}

func TestReceiptOmitsEmptyOptionalFields(t *testing.T) {
	rec := Receipt{ActionID: "act_x", CreatedAt: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"expiresAt", "residue", "previousResourceVersion", "material"} {
		if contains(string(b), banned) {
			t.Errorf("receipt JSON must not contain %q: %s", banned, b)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
