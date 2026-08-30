package rop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brilliantkid87/rop/internal/testutil"
)

// fixtureDir walks spec/fixtures/v0.1 and returns every fixture decoded as a
// generic document. Master Prompt §64: documentation, fixtures, and
// implementation MUST NOT drift apart.
func loadFixtures(t *testing.T) map[string]map[string]any {
	t.Helper()
	dir := filepath.Join(testutil.RepoRoot(), "spec", "fixtures", "v0.1")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]any{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("fixture %s does not parse: %v", e.Name(), err)
		}
		out[e.Name()] = m
	}
	if len(out) < 15 {
		t.Fatalf("fixture set unexpectedly small: %d", len(out))
	}
	return out
}

// TestAllFixturesParseAndUseStableVocabulary asserts every v0.1 fixture
// parses, uses the stable camelCase wire names, and emits only known
// enum vocabulary except where a fixture deliberately demonstrates
// unknown-value tolerance.
func TestAllFixturesParseAndUseStableVocabulary(t *testing.T) {
	fixtures := loadFixtures(t)
	enumValues := map[string]map[string]bool{
		"verification-status": {"VERIFIED": true, "FAILED": true, "UNKNOWN": true},
		"reversibility": {
			"REVERSIBLE": true, "RESTORABLE": true, "COMPENSATABLE": true,
			"PARTIALLY_COMPENSATABLE": true, "IRREVERSIBLE": true,
		},
		"guarantee": {
			"EXACT": true, "EVENTUAL": true, "BEST_EFFORT": true,
			"MANUAL": true, "NONE": true,
		},
		"status": {
			"APPLIED": true, "REVERSING": true, "REVERSED": true,
			"PARTIALLY_REVERSED": true, "REVERSE_FAILED": true,
			"OUTCOME_UNKNOWN": true, "EXPIRED": true, "IRREVERSIBLE": true,
		},
		"outcome": {
			"REVERSED": true, "PARTIALLY_REVERSED": true, "REVERSE_FAILED": true,
			"CONFLICT": true, "OUTCOME_UNKNOWN": true,
		},
	}
	unknownEnumOK := map[string]bool{"unknown-enum-value.json": true}
	// verification fixtures use "status" for the verification enum, not the
	// Action state enum; their values are checked via the typed decode below.
	verificationFixtures := map[string]bool{
		"verification-verified.json": true, "verification-unknown.json": true,
		"verification-failed.json": true,
	}
	for name, doc := range fixtures {
		if strings.HasPrefix(name, "problem-") {
			continue // problem fixtures: no semantic enums; type checked below
		}
		for field, vocab := range enumValues {
			if field == "verification-status" {
				continue // checked via the fixture-specific decode below
			}
			if field == "status" && verificationFixtures[name] {
				vstr, _ := doc[field].(string)
				if !enumValues["verification-status"][vstr] {
					t.Errorf("%s: verification status %q is not v0.1 vocabulary", name, vstr)
				}
				continue
			}
			v, ok := doc[field]
			if !ok {
				continue
			}
			str, ok := v.(string)
			if !ok {
				continue // non-string field (e.g. problem status codes)
			}
			if !vocab[str] && !unknownEnumOK[name] {
				t.Errorf("%s: %s %q is not v0.1 vocabulary", name, field, str)
			}
		}
		// Problem fixtures use urn:rop:problem:* types.
		if pt, ok := doc["type"].(string); ok && strings.HasPrefix(name, "problem-") {
			const prefix = "urn:rop:problem:"
			if !strings.HasPrefix(pt, prefix) {
				t.Errorf("%s: problem type %q is not a urn:rop:problem:* URI", name, pt)
			}
		}
	}
}

// TestFixtureShapesMatchWireTypes decodes the representative fixtures into
// the wire types to prove the frozen documents and the implementation agree.
func TestFixtureShapesMatchWireTypes(t *testing.T) {
	fixtures := loadFixtures(t)
	decode := func(name string, into any) {
		t.Helper()
		raw, err := json.Marshal(fixtures[name])
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, into); err != nil {
			t.Fatalf("%s does not decode into the wire type: %v", name, err)
		}
	}
	var disc Discovery
	decode("discovery.json", &disc)
	if disc.Protocol != "rop" || len(disc.Versions) != 1 || disc.Versions[0] != "0.1" {
		t.Fatalf("discovery fixture drift: %+v", disc)
	}
	var rec Receipt
	decode("receipt-reversible.json", &rec)
	if rec.Reversibility != ReversibilityREVERSIBLE || rec.Status != StatusAPPLIED {
		t.Fatalf("receipt fixture drift: %+v", rec)
	}
	var result ReversalResult
	decode("reversal-result-unknown.json", &result)
	if result.Outcome != OutcomeOUTCOME_UNKNOWN || result.Status != StatusOUTCOME_UNKNOWN {
		t.Fatalf("unknown-result fixture drift: %+v", result)
	}
	var ver VerificationResult
	decode("verification-unknown.json", &ver)
	if ver.Status != VerificationUNKNOWN {
		t.Fatalf("verification-unknown fixture drift: %+v", ver)
	}
	// The unknown-enum fixture keeps its values unknown and uncoerced.
	var unknown Receipt
	decode("unknown-enum-value.json", &unknown)
	if unknown.Reversibility.Known() || unknown.Guarantee.Known() {
		t.Fatal("unknown-enum fixture values were coerced into known values")
	}
	if unknown.Reversibility != "TIME_REVERSIBLE" {
		t.Fatalf("unknown value not preserved: %q", unknown.Reversibility)
	}
}

// TestMissingRequiredFieldsAreDetectable documents the validator rule from
// spec/versioning.md §2: absence of a required receipt field surfaces as a
// zero value in the wire type, which a conformance validator rejects — the
// missing field is never silently fabricated.
func TestMissingRequiredFieldsAreDetectable(t *testing.T) {
	var rec Receipt
	payload := []byte(`{"status":"APPLIED","reversibility":"REVERSIBLE","guarantee":"EXACT"}`)
	if err := json.Unmarshal(payload, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.ActionID == "" || rec.ProviderID == "" || rec.OperationID == "" {
		return // missing required fields are visible to a validator
	}
	t.Fatal("missing required fields were not detectable")
}

// TestUnknownOptionalFieldsIgnoredOnFixtureRoundTrip: the receipt fixture
// carries an unknown optional field (forward compatibility, §21); it must
// survive document-level handling without breaking typed parsers.
func TestUnknownOptionalFieldsIgnoredOnFixtureRoundTrip(t *testing.T) {
	fixtures := loadFixtures(t)
	doc := fixtures["receipt-reversible.json"]
	if _, ok := doc["anUnknownOptionalField"]; !ok {
		t.Fatal("fixture must retain the unknown-field demonstration")
	}
	raw, _ := json.Marshal(doc)
	var rec Receipt
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.ActionID != "act_01KEXAMPLE00000000000000" {
		t.Fatalf("typed parse broke on unknown field: %+v", rec)
	}
}
