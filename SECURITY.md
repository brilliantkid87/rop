# Security Policy

## Scope

This repository contains two distinct things, and security reports should
distinguish them:

1. **The reference implementation** (`cmd/`, `internal/`, `pkg/`,
   `examples/`) — Go code with vulnerabilities like any software.
2. **The experimental protocol** (`spec/`, `docs/`) — whose security
   properties and *fundamental, documented limitations* are analyzed in
   [`docs/security.md`](docs/security.md).

## Reporting implementation vulnerabilities

Report privately to the maintainers (open a restricted advisory or contact a
maintainer directly). Do not open a public issue for an exploitable
implementation defect. Please include: affected component, reproduction steps,
and impact assessment.

Implementation defects affecting core correctness are fixed on the
`v0.1.0-experimental` line and recorded in `docs/task-tracker.md`.

## Protocol limitations are not vulnerabilities

ROP is an **experimental, non-production protocol**. Several properties are
documented limitations of the current design, not bugs to report — please read
before filing:

- No authentication scheme is defined; principals are deployment-defined.
- Receipts are unsigned; tamper-evidence is future work (OQ-2).
- A malicious or compromised provider adapter can misreport its own protocol
  surface; ROP makes deception expensive (independent verification), not
  impossible.
- The missing-receipt window (an external effect occurring before any durable
  record) is bounded by an intent journal, never closed.
- Possession of an Action ID is identity, never authorization; cross-scope
  requests return not-found by design.
- `OUTCOME_UNKNOWN` persists until evidence arrives; that is intentional.

These are tracked in `docs/security.md` (threats T1–T17) and the open
questions register. Design-level proposals belong in a public issue or
discussion referencing the relevant document.
