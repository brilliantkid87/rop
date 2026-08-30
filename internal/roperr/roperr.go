// Package roperr carries semantic failures between ROP Core services and the
// transport binding. Errors carry protocol problem types (Master Prompt §63),
// never HTTP status codes: mapping transport semantics is the binding's job
// (invariant I-17).
package roperr

import "fmt"

// Error is a semantic failure with a stable problem type (a
// urn:rop:problem:* URI from pkg/rop).
type Error struct {
	ProblemType string
	Detail      string
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return e.ProblemType
	}
	return fmt.Sprintf("%s: %s", e.ProblemType, e.Detail)
}

// New builds a semantic error.
func New(problemType, detailFormat string, args ...any) *Error {
	detail := ""
	if detailFormat != "" {
		detail = fmt.Sprintf(detailFormat, args...)
	}
	return &Error{ProblemType: problemType, Detail: detail}
}

// From extracts a *Error from err, or nil.
func From(err error) *Error {
	if e, ok := err.(*Error); ok {
		return e
	}
	return nil
}
