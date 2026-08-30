# ROP Release Notes — `0.1.0-experimental`

Date: 2026-08-30. Status: **experimental, non-production**.

## Research hypothesis

> Can reversibility of side-effecting operations be exposed as a small,
> interoperable, machine-readable protocol primitive without becoming a
> distributed transaction manager or workflow engine?

## Outcome of the research cycle

The 2026-08-30 architecture review **approved the protocol with modifications
(MODIFY)**: the thesis was narrowed to the interoperable *description,
recording, evaluation, and verification* of reversibility. Generic reversal
invocation was demoted to an optional advertised capability after the review
found it the least defensible part of the original hypothesis. A genuine
negative finding during implementation replaced recovery-by-re-execution with
recovery-by-reconciliation (a crash leaves no durable evidence distinguishing
"provider call never started" from "provider succeeded unobserved").

## What ROP v0.1 standardizes

- Operation-level reversibility metadata: class (`REVERSIBLE`, `RESTORABLE`,
  `COMPENSATABLE`, `PARTIALLY_COMPENSATABLE`, `IRREVERSIBLE`), guarantee
  (`EXACT`, `EVENTUAL`, `BEST_EFFORT`, `MANUAL`, `NONE`), eligibility window.
- Action Receipts: immutable public records of concrete executions, with
  append-preserving history.
- Per-Action current eligibility: server-time expiration
  (`receivedAt >= expiresAt` ⇒ expired), state, dependency constraints.
- Provider-defined verification semantics with declared semantics classes and
  honest `UNKNOWN` outcomes.
- Explicit uncertainty (`OUTCOME_UNKNOWN`), residue representation (declared /
  discovered / verified), and dependency safety constraints.
- A stable machine-readable problem vocabulary (RFC 9457, `urn:rop:problem:*`).

## What ROP v0.1 deliberately does not standardize

How a provider reverses anything (existing refund/delete/restore APIs remain
the mechanism); business meaning of semantic equivalence; batch reversal;
cancellation; workflow coordination; a public reconciliation wire contract
(reconciliation is an internal domain operation in v0.1); receipt signing;
provider business APIs.

## Core vs optional capabilities

Conformance is capability-based. **ROP v0.1 Core** requires discovery, Operation
metadata, receipts, eligibility, verification semantics, uncertainty
representation, problem types, and evolution tolerance. Optional advertised
capabilities: `planning`, `reversal` (generic invocation), `reconciliation`
(future). Dependencies and residue are structural. Vague claims such as
"ROP-compatible" are not conformance claims. See `docs/capability-model.md`.

## Demonstrated scenarios

One persisted, versioned reference domain demonstrates: REVERSIBLE create with
CAS-guarded reversal; RESTORABLE update with captured prior-state material and
conflict-refusing restoration (including the mandated `v1→v2→v3` case);
PARTIALLY_COMPENSATABLE notification with immutable-delivery residue;
IRREVERSIBLE publish boundary; dependency blocking and unblocking; durable
idempotent reversal with concurrent convergence; expiration to the nanosecond
boundary; unknown outcomes resolved by evidence-gated reconciliation; crash
recovery that preserves uncertainty.

## Relationship to prior art

The mechanisms are established prior art (compensation/Saga, idempotency keys,
optimistic concurrency, outbox journals, reconciliation); ROP claims novelty
only in the standardized cross-provider representation. Closest relatives —
MicroProfile LRA and WS-BusinessActivity — are coordinator/participant
workflow protocols; ROP has neither. See `docs/prior-art.md`.

## Current limitations

Single-node SQLite reference implementation; reconciliation is internal (no
wire contract); verification is only as good as the provider's declared
postconditions; a malicious provider can misreport its own surface; the
missing-receipt window is bounded by an intent journal, never closed;
`/.well-known/rop` is experimental and unregistered. Full list:
`docs/v0.1-assessment.md`, `docs/security.md`.

## Known open questions

All fifteen are tracked with M6 status labels in `docs/open-questions.md`
(10 RESOLVED_FOR_V0_1, 3 DEFERRED, 2 STILL_OPEN: OQ-6 retry-guidance
visibility, OQ-10 Well-Known URI registration).

## Status

**Experimental. Non-production.** ROP v0.1 makes no guarantee of reversal
success, does not claim exactly-once execution, and cannot undo arbitrary side
effects. A negative research result remains acceptable.
