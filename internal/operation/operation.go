// Package operation defines the experimental provider abstraction (Master
// Prompt §76) and the registry of Operation definitions. Providers own
// business-specific reversal semantics; ROP owns interoperable lifecycle
// semantics. The API is deliberately not frozen (Master Prompt §76).
//
// This package is ROP Core: it MUST NOT import any HTTP package (I-17).
package operation

import (
	"context"
	"fmt"
	"time"

	"github.com/brilliantkid87/rop/internal/store"
	"github.com/brilliantkid87/rop/pkg/rop"
)

// PlanInput carries the durable facts a plan may consult. Planning MUST NOT
// cause external side effects (invariant I-3): PlanFunc receives read-only
// data and returns a description.
type PlanInput struct {
	Action   store.ActionRow
	Material map[string]any
	Now      time.Time
}

// PlanOutput is the provider's contribution to a reversal plan. Concrete
// facts only — numeric risk scores are forbidden (Master Prompt §39).
type PlanOutput struct {
	Preconditions        []string
	ExpectedReversal     string
	Residue              []rop.Residue
	Conflicts            []string
	ManualRequirements   []string
	BasisResourceVersion *int64
	ValidUntil           *time.Time
}

// ReverseInput carries what a reversal execution may need. ProviderRef is the
// durable, stable provider execution identity pre-assigned by ROP for this
// attempt (Master Prompt §32/§36 context; M4): providers MUST make execution
// safe to look up (and, where they support it, idempotent) by this reference.
// It is distinct from any HTTP Idempotency-Key, which protects ROP request
// handling only.
type ReverseInput struct {
	Action      store.ActionRow
	Material    map[string]any
	Now         time.Time
	ProviderRef string
}

// ProviderFailure is a classified provider-side failure (Master Prompt §37).
// The classes are internal: they drive behavior (retry/reconcile/park), they
// are not protocol enums. A plain (unclassified) error is always treated as
// RECONCILE_REQUIRED: an unclassifiable failure must not collapse into
// semantic failure (Master Prompt §34).
type ProviderFailure struct {
	Class   RetryClass
	Message string
}

func (e *ProviderFailure) Error() string { return e.Message }

// RetryClass is the internal failure taxonomy (Master Prompt §37).
type RetryClass string

const (
	// RetryRetriable: the failure is known to have occurred before any
	// provider-side effect (e.g. connection refused before send). The
	// business state is unchanged; a new reversal request is permitted.
	RetryRetriable RetryClass = "RETRYABLE"
	// RetryNonRetriable: the provider definitively rejected the reversal. No
	// automatic retry; the Action concludes REVERSE_FAILED.
	RetryNonRetriable RetryClass = "NON_RETRYABLE"
	// RetryReconcileRequired: the outcome could not be observed (timeout after
	// possible execution, lost response). Only reconciliation may resolve it.
	RetryReconcileRequired RetryClass = "RECONCILE_REQUIRED"
	// RetryManualRequired: the outcome is irreducibly ambiguous for automated
	// resolution; preserved as unknown for human intervention.
	RetryManualRequired RetryClass = "MANUAL_INTERVENTION_REQUIRED"
)

// ReverseOutput is the provider-observed result of one reversal execution.
// Outcome CONFLICT means the provider refused on a correctness-critical
// precondition (I-7): the Action is unchanged, not failed. Outcome
// OUTCOME_UNKNOWN means the result was not observed (I-5); the attempt stays
// open for reconciliation. PARTIALLY_REVERSED arrives with Residue (§45).
type ReverseOutput struct {
	Outcome     rop.ReversalOutcome
	ProviderRef string
	Error       string
	Residue     []rop.Residue
}

// VerifyInput carries what a verification may consult.
type VerifyInput struct {
	Action   store.ActionRow
	Material map[string]any
	Now      time.Time
}

// ReconcileInput carries what a reconciliation lookup may consult. ProviderRef
// is the durable execution identity: reconciliation asks "what happened to
// provider operation X?" without replaying the side effect (M4).
type ReconcileInput struct {
	Action      store.ActionRow
	Material    map[string]any
	Now         time.Time
	ProviderRef string
}

// ReconcileOutput is one provider lookup observation. Proven expresses the
// adapter contract: a negative lookup ("not found") may only be reported as
// a proven failure when the provider guarantees that absence proves
// non-execution; unproven evidence stays inconclusive (Master Prompt §38).
type ReconcileOutput struct {
	Outcome rop.ReversalOutcome // REVERSED or REVERSE_FAILED as evidence
	Proven  bool
	Detail  string
}

// VerifyOutput evaluates provider-defined postconditions (Master Prompt §7,
// §46). Verification MUST NOT create business side effects (§47).
type VerifyOutput struct {
	Status         rop.VerificationStatus
	Semantics      rop.VerificationSemantics
	Postconditions []rop.Postcondition
	Detail         string
}

// PlanFunc, ReverseFunc, VerifyFunc, and ReconcileFunc are the provider-owned
// semantics hooks (Master Prompt §76). ReconcileFunc is a read-only provider
// lookup for an existing execution reference — never a re-invocation of the
// side effect (M4).
type PlanFunc func(ctx context.Context, in PlanInput) (PlanOutput, error)

type ReverseFunc func(ctx context.Context, in ReverseInput) (ReverseOutput, error)

type VerifyFunc func(ctx context.Context, in VerifyInput) (VerifyOutput, error)

type ReconcileFunc func(ctx context.Context, in ReconcileInput) (ReconcileOutput, error)

// Operation is a reusable behavior definition plus its provider-owned
// reversal functions. TTL == 0 means no eligibility window.
type Operation struct {
	ID                 string
	Reversibility      rop.Reversibility
	Guarantee          rop.Guarantee
	TTL                time.Duration
	ReverseOperationID string

	PlanFunc      PlanFunc
	ReverseFunc   ReverseFunc
	VerifyFunc    VerifyFunc
	ReconcileFunc ReconcileFunc
}

// Validate checks the Operation's declared metadata against the v0.1
// vocabulary. Unknown values are a construction error, never stored.
func (o Operation) Validate() error {
	if o.ID == "" {
		return fmt.Errorf("operation: empty id")
	}
	if !o.Reversibility.Known() {
		return fmt.Errorf("operation %s: unknown reversibility %q", o.ID, o.Reversibility)
	}
	if !o.Guarantee.Known() {
		return fmt.Errorf("operation %s: unknown guarantee %q", o.ID, o.Guarantee)
	}
	if o.Reversibility == rop.ReversibilityIRREVERSIBLE && o.TTL != 0 {
		return fmt.Errorf("operation %s: IRREVERSIBLE operation must not declare a TTL", o.ID)
	}
	if o.Reversibility == rop.ReversibilityIRREVERSIBLE && (o.ReverseFunc != nil || o.VerifyFunc != nil) {
		return fmt.Errorf("operation %s: IRREVERSIBLE operation must not declare reversal or verification", o.ID)
	}
	if o.ReverseFunc != nil && o.ReverseOperationID == "" {
		return fmt.Errorf("operation %s: reversal-capable operations must declare reverseOperationId", o.ID)
	}
	return nil
}

// Registry holds the provider's Operation definitions.
type Registry struct {
	ops map[string]Operation
}

// NewRegistry validates and registers the given operations.
func NewRegistry(ops ...Operation) (*Registry, error) {
	r := &Registry{ops: make(map[string]Operation, len(ops))}
	for _, o := range ops {
		if err := o.Validate(); err != nil {
			return nil, err
		}
		if _, dup := r.ops[o.ID]; dup {
			return nil, fmt.Errorf("operation: duplicate id %q", o.ID)
		}
		r.ops[o.ID] = o
	}
	return r, nil
}

// Get returns the operation definition, if registered.
func (r *Registry) Get(id string) (Operation, bool) {
	o, ok := r.ops[id]
	return o, ok
}
