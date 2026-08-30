// Package authz defines the authorization abstraction (Master Prompt §54).
// Possession of an Action ID is identity, never authority (invariant I-2):
// every verb is evaluated against the principal's scopes and permissions.
// The MVP ships a minimal in-scope allow authorizer; enterprise IAM is a
// non-goal (§54).
//
// This package is ROP Core: it MUST NOT import any HTTP package (I-17).
package authz

import "fmt"

// Verb is one auditable capability boundary (§54).
type Verb string

const (
	VerbInspect   Verb = "inspect"
	VerbPlan      Verb = "plan"
	VerbReverse   Verb = "reverse"
	VerbVerify    Verb = "verify"
	VerbReconcile Verb = "reconcile"
)

// Principal is the requester identity. Scopes maps the authorization scopes
// the principal may act within (tenant/organization/account — ROP does not
// standardize the business meaning, Master Prompt §17).
type Principal struct {
	ID     string
	Scopes map[string]bool
}

// Authorizer evaluates a principal's permission for one verb in one scope.
type Authorizer interface {
	Can(p Principal, verb Verb, scope string) bool
}

// ScopeAllow authorizes any principal that is a member of the scope, for all
// verbs. Reference MVP only: single-tenant mode is explicitly not a security
// claim (docs/security.md T7).
type ScopeAllow struct{}

// Can implements Authorizer.
func (ScopeAllow) Can(p Principal, verb Verb, scope string) bool {
	if p.ID == "" || scope == "" {
		return false
	}
	return p.Scopes[scope]
}

// DenyAll refuses everything; used in tests to assert that Action ID
// possession grants nothing (invariant I-2).
type DenyAll struct{}

// Can implements Authorizer.
func (DenyAll) Can(Principal, Verb, string) bool { return false }

// ErrDenied is the sentinel for authorization failure.
var ErrDenied = fmt.Errorf("authorization denied")
