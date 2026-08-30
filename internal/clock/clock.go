// Package clock abstracts time so eligibility boundaries are deterministic
// under test (Master Prompt §24). Server/provider time is authoritative for
// all ROP decisions; client time MUST NOT determine reversal eligibility.
package clock

import "time"

// Clock provides the current authoritative time (UTC).
type Clock interface {
	Now() time.Time
}

// System is the wall-clock implementation.
type System struct{}

func (System) Now() time.Time { return time.Now().UTC() }

// Fixed is a deterministic implementation for tests.
type Fixed struct{ T time.Time }

func (f Fixed) Now() time.Time { return f.T }
