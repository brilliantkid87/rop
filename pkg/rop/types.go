// Package rop defines the transport-neutral wire vocabulary of ROP v0.1:
// enums, receipt, discovery, plan, reversal result, verification result, and
// problem types. These types are the protocol's public face; naming follows
// the stable camelCase wire convention (Master Prompt §62).
//
// ROP is an experimental protocol / research project. Unknown semantic enum
// values MUST remain unknown (never coerced into a known value).
package rop

import "time"

// Reversibility is the reversibility class of an Operation or Action
// (Master Prompt §6). Unknown values are representable and MUST NOT be
// coerced (Master Prompt §21).
type Reversibility string

const (
	ReversibilityREVERSIBLE              Reversibility = "REVERSIBLE"
	ReversibilityRESTORABLE              Reversibility = "RESTORABLE"
	ReversibilityCOMPENSATABLE           Reversibility = "COMPENSATABLE"
	ReversibilityPARTIALLY_COMPENSATABLE Reversibility = "PARTIALLY_COMPENSATABLE"
	ReversibilityIRREVERSIBLE            Reversibility = "IRREVERSIBLE"
)

// Known reports whether the value is one of the five v0.1 classes.
func (r Reversibility) Known() bool {
	switch r {
	case ReversibilityREVERSIBLE, ReversibilityRESTORABLE, ReversibilityCOMPENSATABLE,
		ReversibilityPARTIALLY_COMPENSATABLE, ReversibilityIRREVERSIBLE:
		return true
	}
	return false
}

// Guarantee is the reversal guarantee class (Master Prompt §8).
type Guarantee string

const (
	GuaranteeEXACT       Guarantee = "EXACT"
	GuaranteeEVENTUAL    Guarantee = "EVENTUAL"
	GuaranteeBEST_EFFORT Guarantee = "BEST_EFFORT"
	GuaranteeMANUAL      Guarantee = "MANUAL"
	GuaranteeNONE        Guarantee = "NONE"
)

// Known reports whether the value is one of the five v0.1 guarantees.
func (g Guarantee) Known() bool {
	switch g {
	case GuaranteeEXACT, GuaranteeEVENTUAL, GuaranteeBEST_EFFORT, GuaranteeMANUAL, GuaranteeNONE:
		return true
	}
	return false
}

// Status is the Action state (Master Prompt §33).
type Status string

const (
	StatusAPPLIED            Status = "APPLIED"
	StatusREVERSING          Status = "REVERSING"
	StatusREVERSED           Status = "REVERSED"
	StatusPARTIALLY_REVERSED Status = "PARTIALLY_REVERSED"
	StatusREVERSE_FAILED     Status = "REVERSE_FAILED"
	StatusOUTCOME_UNKNOWN    Status = "OUTCOME_UNKNOWN"
	StatusEXPIRED            Status = "EXPIRED"
	StatusIRREVERSIBLE       Status = "IRREVERSIBLE"
)

// VerificationStatus is the outcome of evaluating provider-defined
// postconditions. UNKNOWN is a first-class outcome (Master Prompt §47).
type VerificationStatus string

const (
	VerificationVERIFIED VerificationStatus = "VERIFIED"
	VerificationFAILED   VerificationStatus = "FAILED"
	VerificationUNKNOWN  VerificationStatus = "UNKNOWN"
)

// VerificationSemantics classifies how a verification was performed
// (Master Prompt §47). It tells clients how much to trust the result.
type VerificationSemantics string

const (
	SemanticsLOCAL_READONLY        VerificationSemantics = "LOCAL_READONLY"
	SemanticsEXTERNAL_READONLY     VerificationSemantics = "EXTERNAL_READONLY"
	SemanticsEVENTUALLY_CONSISTENT VerificationSemantics = "EVENTUALLY_CONSISTENT"
	SemanticsEXPENSIVE             VerificationSemantics = "EXPENSIVE"
)

// ReversalOutcome is the provider-observed result of a reversal execution.
// CONFLICT means the provider refused because a correctness-critical
// precondition failed (invariant I-7): the Action is not failed, it is
// unchanged. OUTCOME_UNKNOWN is never produced by guessing (invariant I-5).
type ReversalOutcome string

const (
	OutcomeREVERSED           ReversalOutcome = "REVERSED"
	OutcomePARTIALLY_REVERSED ReversalOutcome = "PARTIALLY_REVERSED"
	OutcomeREVERSE_FAILED     ReversalOutcome = "REVERSE_FAILED"
	OutcomeCONFLICT           ReversalOutcome = "CONFLICT"
	OutcomeOUTCOME_UNKNOWN    ReversalOutcome = "OUTCOME_UNKNOWN"
)

// ResourceRef is an opaque provider-scoped resource reference
// (Master Prompt §11). It has no HTTP-route semantics.
type ResourceRef struct {
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
}

// Residue is a provider-declared residual effect (Master Prompt §45).
// Free-form in v0.1 (open question OQ-3): the model stays small — concrete
// descriptions, not a standardized taxonomy. Residue is NOT evidence that
// reversal failed: a reversal may satisfy its provider-defined contract
// while residue remains. For PARTIALLY_COMPENSATABLE Actions, however, the
// partial semantic outcome remains distinguishable from full reversal.
type Residue struct {
	Description string `json:"description"`
	// Expected: the residue was known before reversal (declared in planning).
	Expected bool `json:"expected,omitempty"`
	// ProviderDefined: the residue item is asserted by the provider, not
	// inferred by ROP. Always true for provider-declared residue in v0.1.
	ProviderDefined bool `json:"providerDefined,omitempty"`
	// Manual: whether manual intervention can change this residue, when known
	// (nil = unknown).
	Manual *bool `json:"manualRemediable,omitempty"`
}

// Receipt is the immutable public Action Receipt (Master Prompt §12).
// It MUST NOT contain reusable privileged reversal credentials or private
// reversal material (invariant I-14).
type Receipt struct {
	ActionID      string        `json:"actionId"`
	ProviderID    string        `json:"providerId"`
	OperationID   string        `json:"operationId"`
	CreatedAt     time.Time     `json:"createdAt"`
	ResourceRef   ResourceRef   `json:"resourceRef"`
	Reversibility Reversibility `json:"reversibility"`
	Guarantee     Guarantee     `json:"guarantee"`
	Status        Status        `json:"status"`
	ExpiresAt     *time.Time    `json:"expiresAt,omitempty"`
	Residue       []Residue     `json:"residue,omitempty"`
}

// Discovery is the /.well-known/rop document (Master Prompt §20).
// Capabilities are explicit booleans and MUST match actual behavior.
type Discovery struct {
	Protocol     string       `json:"protocol"`
	Versions     []string     `json:"versions"`
	Binding      string       `json:"binding"`
	Capabilities Capabilities `json:"capabilities"`
}

// Capabilities advertises optional ROP capabilities (docs/capability-model.md §3).
type Capabilities struct {
	Receipts       bool `json:"receipts"`
	Planning       bool `json:"planning"`
	Reversal       bool `json:"reversal"`
	Verification   bool `json:"verification"`
	Reconciliation bool `json:"reconciliation,omitempty"`
}

// Plan is a read-only reversal plan snapshot (Master Prompt §39–§40).
// It is knowledge at a point in time and is never authorization (invariant I-19).
type Plan struct {
	ActionID             string        `json:"actionId"`
	GeneratedAt          time.Time     `json:"generatedAt"`
	BasisResourceVersion *int64        `json:"basisResourceVersion,omitempty"`
	ValidUntil           *time.Time    `json:"validUntil,omitempty"`
	CurrentStatus        Status        `json:"currentStatus"`
	Reversibility        Reversibility `json:"reversibility"`
	Guarantee            Guarantee     `json:"guarantee"`
	ExpiresAt            *time.Time    `json:"expiresAt,omitempty"`
	Preconditions        []string      `json:"preconditions,omitempty"`
	ExpectedReversal     string        `json:"expectedReversal,omitempty"`
	Residue              []Residue     `json:"residue,omitempty"`
	Conflicts            []string      `json:"conflicts,omitempty"`
	ManualRequirements   []string      `json:"manualRequirements,omitempty"`
	// BlockingDependencies lists active dependent Actions that currently make
	// reversal unsafe (added M5). Plans are advisory snapshots; execution
	// revalidates dependencies independently (invariant I-19).
	BlockingDependencies []string `json:"blockingDependencies,omitempty"`
}

// ReversalResult reports the outcome of a reversal invocation. Transport
// success never implies semantic success (Master Prompt §28): the semantic
// outcome is in Status/Outcome.
type ReversalResult struct {
	AttemptID   string          `json:"attemptId"`
	ActionID    string          `json:"actionId"`
	Status      Status          `json:"status"`
	Outcome     ReversalOutcome `json:"outcome"`
	ObservedAt  time.Time       `json:"observedAt"`
	ProviderRef string          `json:"providerRef,omitempty"`
	Error       string          `json:"error,omitempty"`
}

// Postcondition is one provider-defined semantic check (Master Prompt §7, §46).
type Postcondition struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Satisfied   bool   `json:"satisfied"`
}

// VerificationResult reports evaluation of provider-defined postconditions.
type VerificationResult struct {
	ActionID       string                `json:"actionId"`
	Status         VerificationStatus    `json:"status"`
	Semantics      VerificationSemantics `json:"semantics"`
	Postconditions []Postcondition       `json:"postconditions"`
	EvaluatedAt    time.Time             `json:"evaluatedAt"`
	Detail         string                `json:"detail,omitempty"`
}

// Problem type URIs (Master Prompt §63). urn form keeps the experiment
// independent of any DNS identity (open question OQ-10 notes registration).
const (
	ProblemActionNotFound        = "urn:rop:problem:action-not-found"
	ProblemReversalExpired       = "urn:rop:problem:reversal-expired"
	ProblemIrreversible          = "urn:rop:problem:irreversible-action"
	ProblemReversalConflict      = "urn:rop:problem:reversal-conflict"
	ProblemAlreadyInProgress     = "urn:rop:problem:reversal-already-in-progress"
	ProblemAuthorizationDenied   = "urn:rop:problem:authorization-denied"
	ProblemPreconditionFailed    = "urn:rop:problem:precondition-failed"
	ProblemVerificationFailed    = "urn:rop:problem:verification-failed"
	ProblemCapabilityUnavailable = "urn:rop:problem:capability-unavailable"
	ProblemUnknownEnumValue      = "urn:rop:problem:unknown-enum-value"
	ProblemMalformedPayload      = "urn:rop:problem:malformed-payload"
	// ProblemIdempotencyConflict: an Idempotency-Key was reused with materially
	// different request semantics (a different Action within the same scope).
	// Added in M3 as a backwards-compatible problem-type addition (§21).
	ProblemIdempotencyConflict = "urn:rop:problem:idempotency-key-conflict"
	// ProblemDependencyExists: an active dependent Action makes reversal of
	// this Action unsafe (added in M5; compatible problem-type addition).
	ProblemDependencyExists = "urn:rop:problem:dependency-exists"
)
