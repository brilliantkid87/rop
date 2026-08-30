// Package action defines the Action state machine and its single centralized
// transition table (Master Prompt §33). The table mirrors the normative table
// in docs/invariants.md; a test asserts the two remain consistent by checking
// every documented transition is permitted and no other is.
package action

import "fmt"

// Status mirrors rop.Status; the state machine owns transition legality.
type Status = string

const (
	Applied           Status = "APPLIED"
	Reversing         Status = "REVERSING"
	Reversed          Status = "REVERSED"
	PartiallyReversed Status = "PARTIALLY_REVERSED"
	ReverseFailed     Status = "REVERSE_FAILED"
	OutcomeUnknown    Status = "OUTCOME_UNKNOWN"
	Expired           Status = "EXPIRED"
	Irreversible      Status = "IRREVERSIBLE"
)

// transitions is the single source of truth for state-transition validation.
// Key: source state. Values: permitted destination states.
//
// Notes:
//   - REVERSING -> APPLIED: a reversal was rejected without executing
//     (e.g. provider conflict, invariant I-7); the Action is unchanged.
//   - OUTCOME_UNKNOWN exits only via evidence (invariant I-5).
//   - EXPIRED and IRREVERSIBLE are absorbing for new reversals (invariant I-8).
//   - REVERSE_FAILED -> REVERSING requires a policy decision, not automation
//     (retry taxonomy); not permitted in M1.
var transitions = map[Status][]Status{
	Applied:           {Reversing, Expired},
	Reversing:         {Reversed, PartiallyReversed, ReverseFailed, OutcomeUnknown, Applied},
	OutcomeUnknown:    {Reversed, PartiallyReversed, ReverseFailed, Reversing},
	Reversed:          {},
	PartiallyReversed: {},
	ReverseFailed:     {},
	Expired:           {},
	Irreversible:      {},
}

// CanTransition reports whether from -> to is permitted.
func CanTransition(from, to Status) bool {
	for _, d := range transitions[from] {
		if d == to {
			return true
		}
	}
	return false
}

// TransitionError is returned for an illegal transition.
type TransitionError struct{ From, To Status }

func (e TransitionError) Error() string {
	return fmt.Sprintf("illegal action state transition %s -> %s", e.From, e.To)
}
