package action

import "testing"

// FuzzStateTransitions fuzzes transition pairs: the invariants must hold for
// every input — terminal states never transition, OUTCOME_UNKNOWN never
// exits without evidence-bearing transitions, and EXPIRED/IRREVERSIBLE are
// absorbing (spec/core.md §4; invariants I-5, I-8).
func FuzzStateTransitions(f *testing.F) {
	f.Add("APPLIED", "REVERSING")
	f.Add("REVERSING", "REVERSE_FAILED")
	f.Add("OUTCOME_UNKNOWN", "REVERSE_FAILED")
	f.Add("EXPIRED", "APPLIED")
	f.Add("IRREVERSIBLE", "REVERSED")
	f.Add("", "")
	f.Add("ANY", "THING")
	f.Fuzz(func(t *testing.T, from, to string) {
		if !CanTransition(from, to) {
			return
		}
		// If a transition is permitted, these must hold:
		switch from {
		case Reversed, PartiallyReversed, ReverseFailed, Expired, Irreversible:
			t.Fatalf("terminal state %s transitioned to %s", from, to)
		}
		if from == OutcomeUnknown && to == Applied {
			t.Fatalf("OUTCOME_UNKNOWN silently reverted to APPLIED without evidence")
		}
	})
}
