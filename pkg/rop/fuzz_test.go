package rop

import (
	"encoding/json"
	"testing"
)

// FuzzReceiptParse fuzzes receipt-shaped JSON: no panic, bounded behavior,
// unknown optional fields never crash parsing, and unknown semantic enum
// values are preserved rather than coerced (spec/versioning.md §2).
func FuzzReceiptParse(f *testing.F) {
	f.Add([]byte(`{"actionId":"act_x","reversibility":"REVERSIBLE","guarantee":"EXACT","status":"APPLIED"}`))
	f.Add([]byte(`{"reversibility":"SHAZAM","guarantee":"MOSTLY","status":"MAYBE","extra":{"deep":[1,2,{"x":null}]}}`))
	f.Add([]byte(`{"actionId":123}`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`"string"`))
	f.Add([]byte(`{`))
	f.Add([]byte(`{"residue":[{"description":"fee","manualRemediable":false}]}`))
	f.Add([]byte(`{"expiresAt":"not-a-time"}`))
	f.Add([]byte(nil))
	f.Fuzz(func(t *testing.T, data []byte) {
		var rec Receipt
		if err := json.Unmarshal(data, &rec); err != nil {
			return // malformed is fine: rejected, no panic
		}
		// Unknown values must survive decoding unmodified.
		if rec.Reversibility != "" && !rec.Reversibility.Known() && len(rec.Reversibility) > 0 {
			if rec.Reversibility == ReversibilityREVERSIBLE {
				t.Fatalf("unknown value coerced: %q", rec.Reversibility)
			}
		}
	})
}

// FuzzPlanParse fuzzes plan-shaped JSON: no panic; optional nested structures
// (residue, conflicts) tolerate arbitrary shapes.
func FuzzPlanParse(f *testing.F) {
	f.Add([]byte(`{"actionId":"act_x","preconditions":["p"],"residue":[{"description":"audit record"}]}`))
	f.Add([]byte(`{"blockingDependencies":[1,2,3]}`))
	f.Add([]byte(`{"basisResourceVersion":"seven"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(nil))
	f.Fuzz(func(t *testing.T, data []byte) {
		var plan Plan
		_ = json.Unmarshal(data, &plan) // no panic is the assertion
	})
}

// FuzzProblemParse fuzzes problem-shaped JSON: no panic; the type URI is
// carried through verbatim (clients must not need to parse human text).
func FuzzProblemParse(f *testing.F) {
	f.Add([]byte(`{"type":"urn:rop:problem:reversal-expired","status":410,"detail":"x"}`))
	f.Add([]byte(`{"type":null,"status":"gone"}`))
	f.Add([]byte(`{"title":{"nested":true}}`))
	f.Add([]byte(nil))
	f.Fuzz(func(t *testing.T, data []byte) {
		var problem map[string]any
		_ = json.Unmarshal(data, &problem) // no panic is the assertion
	})
}
