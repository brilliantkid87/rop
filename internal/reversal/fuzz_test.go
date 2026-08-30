package reversal

import "testing"

// FuzzIdempotencyKeySemantics fuzzes key material and request targets: the
// fingerprint must be deterministic per (scope, actionId, verb), must differ
// across different Actions and scopes (so a key can never accidentally
// replay another Action's result), and hashing must never panic on any
// input (M6; invariant I-21).
func FuzzIdempotencyKeySemantics(f *testing.F) {
	f.Add("key", "default", "act_1", "reverse")
	f.Add("", "default", "act_1", "reverse")
	f.Add("ключ-🔑", "область", "act_2", "reverse")
	f.Add("same", "default", "act_1", "plan")
	f.Add("\x00\x01", "s", "a", "reverse")
	f.Fuzz(func(t *testing.T, key, scopeA, actionA, verbA string) {
		h := hashKey(key)
		// Determinism.
		if h != hashKey(key) {
			t.Fatal("key hash is not deterministic")
		}
		// Hash length is fixed by construction (the DB CHECK relies on it).
		if len(h) != 64 {
			t.Fatalf("hash length = %d, want 64", len(h))
		}
		fpA := fingerprint(scopeA, actionA)
		if fpA != fingerprint(scopeA, actionA) {
			t.Fatal("fingerprint is not deterministic")
		}
		// A fingerprint for a different (scope, action) pair must never
		// collide for the same verb — this is what makes cross-Action key
		// reuse detectable rather than accidentally replayable.
		fpB := fingerprint(scopeA+"x", actionA)
		fpC := fingerprint(scopeA, actionA+"x")
		if fpA == fpB || fpA == fpC {
			t.Fatalf("fingerprint collision across (scope, action) pairs")
		}
	})
}
