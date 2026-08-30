package action

import "testing"

// TestTransitionTableMatchesDocumentedSemantics asserts the centralized table
// matches the normative table in docs/invariants.md (every documented
// transition is permitted; nothing else is).
func TestTransitionTableMatchesDocumentedSemantics(t *testing.T) {
	permitted := map[[2]Status]bool{
		{Applied, Reversing}:                true,
		{Applied, Expired}:                  true,
		{Reversing, Reversed}:               true,
		{Reversing, PartiallyReversed}:      true,
		{Reversing, ReverseFailed}:          true,
		{Reversing, OutcomeUnknown}:         true,
		{Reversing, Applied}:                true, // reversal refused without executing (I-7)
		{OutcomeUnknown, Reversed}:          true,
		{OutcomeUnknown, PartiallyReversed}: true,
		{OutcomeUnknown, ReverseFailed}:     true, // only via reconciliation evidence
		{OutcomeUnknown, Reversing}:         true,
	}
	all := []Status{Applied, Reversing, Reversed, PartiallyReversed, ReverseFailed, OutcomeUnknown, Expired, Irreversible}
	for _, from := range all {
		for _, to := range all {
			want := permitted[[2]Status{from, to}]
			if got := CanTransition(from, to); got != want {
				t.Errorf("CanTransition(%s -> %s) = %v, want %v", from, to, got, want)
			}
		}
	}
	// Terminal-until-evidence states: OUTCOME_UNKNOWN has no timeout exit (I-5);
	// EXPIRED/IRREVERSIBLE are absorbing for new reversals (I-8).
	for _, from := range []Status{Reversed, PartiallyReversed, ReverseFailed, Expired, Irreversible} {
		for _, to := range all {
			if CanTransition(from, to) {
				t.Errorf("terminal state %s must not transition to %s", from, to)
			}
		}
	}
}
