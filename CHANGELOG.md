# Changelog

All notable changes to ROP will be documented in this file.

ROP is currently experimental and non-production.

## [0.1.2-experimental] - 2026-08-30

### Status

Clarification-only patch release. **Protocol wire version remains `0.1`.**
Wire-compatible with `0.1.0-experimental`, `0.1.1-experimental`, the frozen
v0.1 fixtures, and the independent Rust clean-room implementation. No
behavioral code changes in the Go reference implementation.

Driven by the independent Rust clean-room validation
(`docs/clean-room-rust-v0.1.md`): all 22 published fixtures parsed, 70 live
black-box interoperability tests passed against the Go reference server, zero
protocol divergence was observed, and ROP v0.1 Core proved independently
implementable from the published specification set.

### Added (documentation)

* Public clean-room findings summary: `docs/clean-room-rust-v0.1.md`
  (methodology, allowed source set, fixture/interop results, findings
  A1–A15, independent supplementary audit).

### Fixed (documentation clarifications; no protocol semantic change)

* **A1 — CLOSED (INPUT_SET_OMISSION):** the previously reported missing
  normative documents (`docs/invariants.md`, `docs/capability-model.md`) were
  present in the published repository all along; their absence was a clean-room
  input-set omission, not a ROP publication defect. A1 must not be cited as a
  ROP specification defect.
* **A2:** `malformed-payload` added to the normative problem vocabulary
  (`spec/core.md` §9) with usage definition and HTTP 400 binding; it was
  already emitted by the reference implementation. No duplicate
  malformed-request problem type was created.
* **A3:** `CONFLICT` documented in `spec/core.md` §4 as an attempt-level
  outcome, not an Action lifecycle state; on conflict the Action remains in /
  returns to `APPLIED` with no business mutation, and the result `status`
  field mirrors the resulting Action state. No new Action state was added.
* **A4:** `receivedAt` documented in `spec/core.md` §11 as the
  provider/server-observed receipt time — not client authority, not
  client-supplied, not required as a public wire field. Eligibility rule
  unchanged (`receivedAt < expiresAt` eligible).
* **A6:** every reversal-plan wire member fully specified in
  `spec/http-binding.md` §3 (camelCase name, type, optionality, semantics,
  absence behavior), covering the members the clean-room report flagged as
  insufficiently specified. No new plan functionality.
* **A15:** `expiresAt` wording conflict between `docs/capability-model.md`
  and `spec/core.md` resolved: `expiresAt` is present only when an eligibility
  window applies; a non-IRREVERSIBLE Action MAY have no window; absence means
  the Action does not expire solely due to time. No behavior change.

### Compatibility

There are **no protocol, wire-format, semantic, capability, state-machine,
fixture, or conformance changes** from `0.1.1-experimental` or
`0.1.0-experimental`. Frozen fixtures are unchanged. Protocol version remains
`0.1`. Reference implementation release version: `0.1.2-experimental`
(correcting the `VERSION` file, which the `0.1.1-experimental` packaging fix
had left at `0.1.0-experimental`).

---

## [0.1.1-experimental] - 2026-08-30

### Fixed

- Added the previously omitted `cmd/ropd` source directory to the public release.
- Added the previously omitted `cmd/ropctl` source directory to the public release.
- Corrected `.gitignore` rules so root-level `ropd` and `ropctl` build artifacts do not accidentally exclude the corresponding source directories under `cmd/`.

The previous rules:

```gitignore
ropd
ropd.exe
ropctl
ropctl.exe
````

were replaced with root-anchored build artifact rules:

```gitignore
/ropd
/ropd.exe
/ropctl
/ropctl.exe
```

### Compatibility

There are **no protocol, wire-format, semantic, capability, state-machine, fixture, or conformance changes** from `0.1.0-experimental`.

Protocol version remains:

```text
0.1
```

Reference implementation release version:

```text
0.1.1-experimental
```

The following remain unchanged:

* Operation and Action semantics
* Action Receipts
* reversal eligibility semantics
* reversibility classes
* guarantees
* expiration semantics
* idempotency semantics
* `OUTCOME_UNKNOWN`
* reconciliation model
* verification semantics
* dependency semantics
* residue representation
* capability-based conformance
* RFC 9457 problem vocabulary
* experimental ROP/HTTP binding
* OpenAPI `x-rop` extension
* frozen v0.1 interoperability fixtures

### Notes

This release supersedes `0.1.0-experimental` for users cloning, building, or inspecting the reference implementation source.

The original `0.1.0-experimental` tag remains available as historical release evidence.

The issue was limited to packaging/source inclusion. ROP v0.1 protocol semantics were not affected.

---

## [0.1.0-experimental] - 2026-08-30

### Status

**Experimental, non-production.**

### Research hypothesis

> Can reversibility of side-effecting operations be exposed as a small,
> interoperable, machine-readable protocol primitive without becoming a
> distributed transaction manager or workflow engine?

### Research-cycle outcome

The architecture review approved the protocol with modifications (`MODIFY`).

The original hypothesis was narrowed to the interoperable:

* description;
* recording;
* evaluation;
* verification;

of reversibility for side-effecting operations.

Generic reversal invocation was demoted from a defining protocol primitive to an optional advertised capability.

During implementation, recovery-by-re-execution was also rejected for ambiguous provider outcomes and replaced with recovery-by-reconciliation.

A crash after an external provider may have executed cannot safely distinguish:

```text
provider call never executed
```

from:

```text
provider succeeded but the result was not observed
```

without additional durable evidence.

ROP therefore preserves uncertainty instead of blindly retrying or falsely reporting failure.

### Added

#### Core protocol semantics

* Transport-neutral ROP Core model.
* Operation vs Action distinction.
* Action-specific reversal eligibility.
* Immutable public Action Receipts.
* Separate private reversal material.
* Append-preserving Action history.
* Reversal Attempt model.
* Explicit uncertainty through `OUTCOME_UNKNOWN`.
* Provider-defined semantic verification.
* Capability-based conformance model.

#### Reversibility classes

Added:

* `REVERSIBLE`
* `RESTORABLE`
* `COMPENSATABLE`
* `PARTIALLY_COMPENSATABLE`
* `IRREVERSIBLE`

#### Guarantees

Added:

* `EXACT`
* `EVENTUAL`
* `BEST_EFFORT`
* `MANUAL`
* `NONE`

#### Eligibility and expiration

* Server-time reversal eligibility.
* Exact expiration boundary:

```text
receivedAt < expiresAt
→ eligible

receivedAt >= expiresAt
→ expired
```

* Actions without expiration windows.
* Expiration persistence across restart.
* In-progress reversals remain valid after their acceptance deadline passes.

#### Reversal planning

* Reversal-plan representation.
* `basisResourceVersion`.
* Stale-plan detection.
* Execution-time revalidation of correctness-critical preconditions.
* Blocking dependency exposure.
* Expected residue representation.

Planning remains an optional capability.

#### Reversal invocation

* Experimental generic reversal invocation capability.
* Durable Reversal Attempts.
* Provider execution identity.
* Explicit distinction between:

  * request acceptance;
  * reversal execution;
  * verification.

Generic reversal invocation is optional and is not required for ROP Core conformance.

#### Durable idempotency

* HTTP `Idempotency-Key` support.
* Durable idempotency records.
* Concurrent duplicate-request convergence.
* Request fingerprint validation.
* Restart-safe replay behavior.
* Lost-response replay without duplicate compensation.
* Separation of HTTP idempotency from provider-side execution identity.

ROP does not claim exactly-once provider execution.

#### Unknown outcomes

* First-class `OUTCOME_UNKNOWN`.
* `AWAITING_RECONCILIATION` attempt behavior.
* Ambiguous provider outcomes are never silently converted to failure.
* Unknown outcomes are never blindly re-executed.

#### Reconciliation

* Internal reconciliation service.
* Durable provider execution references.
* Evidence-gated reconciliation.
* Durable reconciliation observations:

  * `PROVEN_REVERSED`
  * `PROVEN_NOT_REVERSED`
  * `INCONCLUSIVE`
* Negative provider lookups do not automatically prove that reversal never occurred.
* Restart recovery parks ambiguous attempts for reconciliation.

A public reconciliation wire contract remains outside v0.1.

#### Verification

* Provider-defined postcondition verification.
* Verification remains independent from reversal execution.
* Verification can report:

  * verified;
  * failed;
  * unknown.
* Verification may observe a desired postcondition while execution outcome remains unresolved.

#### Concurrency protection

* Resource-version / CAS protection.
* Unsafe restoration after concurrent mutation is rejected.
* Demonstrated conflict case:

```text
v1 → v2
v2 → v3

reverse first Action
→ conflict
```

instead of destructively restoring `v1`.

* Sequential reversal remains possible when later reversal restores the required version anchor.

#### Dependencies

* Durable Action dependency edges.
* Dependencies act as reversal safety constraints.
* Planning exposes blocking dependents.
* Execution re-checks dependency state.
* Direct and indirect cycles are rejected.
* Cross-scope dependencies are rejected.
* ROP does not automatically traverse or reverse dependency graphs.

#### Partial compensation and residue

* Genuine `PARTIALLY_COMPENSATABLE` reference scenario.
* Durable residue representation.
* Residue lifecycle stages:

  * `DECLARED`
  * `DISCOVERED`
  * `VERIFIED`
* Partial compensation can verify successfully while residue remains.
* Residue does not automatically mean reversal failed.

#### Experimental HTTP binding

Added experimental ROP/HTTP endpoints including:

```text
GET  /.well-known/rop
GET  /.well-known/rop/actions/{actionId}
POST /.well-known/rop/actions/{actionId}/plan-reversal
POST /.well-known/rop/actions/{actionId}/reverse
GET  /.well-known/rop/actions/{actionId}/verification
```

The `/.well-known/rop` URI is experimental and is not currently registered with IANA.

#### HTTP problem model

* RFC 9457 Problem Details.
* Stable `urn:rop:problem:*` problem vocabulary.
* Machine-readable errors for conditions including:

  * expired Action;
  * irreversible Action;
  * stale/conflicting reversal;
  * dependency conflict;
  * idempotency-key conflict;
  * unavailable optional capability;
  * authorization/scope failure.

#### OpenAPI extension

Added experimental:

```text
x-rop
```

for Operation-level reversibility metadata.

The extension intentionally does not model workflow orchestration.

#### Reference implementation

Added Go reference implementation with:

* SQLite persistence;
* migrations;
* transport-neutral internal domain layer;
* ROP/HTTP server;
* `ropd` daemon implementation;
* `ropctl` CLI implementation;
* persisted versioned demo resource API.

Note: the `cmd/ropd` and `cmd/ropctl` source directories were developed during this release cycle but were accidentally omitted from the `0.1.0-experimental` public Git snapshot. They are included beginning with `0.1.1-experimental`.

#### Interoperability fixtures

Added frozen v0.1 fixtures covering:

* discovery;
* capability advertisement;
* all reversibility classes;
* expiration;
* dependencies;
* residue;
* reversal plans;
* successful reversal;
* partial reversal;
* unknown outcomes;
* verification outcomes;
* RFC 9457 problems;
* unknown optional fields;
* unknown semantic enum handling.

#### Testing and hardening

Added coverage for:

* valid and invalid state transitions;
* Operation vs Action separation;
* append-preserving history;
* exact expiration boundaries;
* stale plans;
* concurrent resource mutation;
* durable idempotency;
* concurrent duplicate requests;
* restart persistence;
* unknown provider outcomes;
* reconciliation;
* crash recovery;
* dependency cycles;
* partial compensation;
* residue;
* scope isolation;
* wire-format evolution tolerance;
* adversarial scenarios.

Added focused fuzz testing for:

* Action Receipt parsing;
* reversal-plan parsing;
* RFC 9457 problem parsing;
* state-transition sequences;
* idempotency semantics;
* dependency graphs.

### Fixed during the research cycle

Several implementation defects were discovered through testing and corrected before release:

* Actions without an expiry window could incorrectly expire because a missing timestamp was persisted as an empty string instead of SQL `NULL`.
* Variable-width RFC 3339 timestamp strings were unsafe for nanosecond-boundary lexical comparison; durable timestamps were changed to a fixed-width representation.
* SQLite constraint classification incorrectly treated some FK/CHECK constraint failures as uniqueness conflicts.
* Recovery-by-re-execution was found unsafe for ambiguous provider outcomes and replaced with reconciliation-based recovery.
* Client-controlled identifiers could be reflected without sufficient bounds in problem-detail responses; reflected details are now bounded and sanitized.

### Prior art

ROP deliberately reuses established distributed-systems mechanisms including:

* Saga / compensating transactions;
* Try-Confirm-Cancel;
* WS-BusinessActivity;
* MicroProfile LRA;
* idempotency keys;
* optimistic concurrency control;
* transactional outbox patterns;
* reconciliation techniques.

ROP does not claim those mechanisms are novel.

The research hypothesis is limited to whether standardized cross-provider representation of reversibility at the Operation/Action boundary creates useful interoperability.

See:

```text
docs/prior-art.md
```

### Deliberately not standardized

ROP v0.1 does not standardize:

* provider-specific reversal implementation;
* provider refund/delete/restore business APIs;
* business-specific semantic equivalence;
* distributed ACID;
* two-phase commit;
* batch reversal;
* cancellation;
* workflow coordination;
* Saga orchestration;
* a public reconciliation endpoint;
* cryptographic receipt signing;
* provider business payloads.

### Current limitations

* Experimental and non-production.
* Single-node SQLite reference implementation.
* No standardized authentication scheme.
* No production distributed deployment model.
* Reconciliation is internal in v0.1.
* Verification is only as trustworthy as the provider-defined contract.
* A malicious or compromised provider can misrepresent its own state.
* Receipt cryptographic signing is not implemented.
* The external-side-effect / missing-receipt window cannot be universally eliminated.
* Generic reversal invocation remains less proven as an interoperability primitive than metadata, receipts, eligibility, and verification.
* `/.well-known/rop` remains experimental and unregistered with IANA.

See:

```text
docs/v0.1-assessment.md
docs/security.md
docs/failure-model.md
docs/open-questions.md
```

### Open questions

At the end of the v0.1 research cycle:

* 10 questions are `RESOLVED_FOR_V0_1`;
* 3 are `DEFERRED`;
* 2 remain `STILL_OPEN`.

The remaining open questions are:

* OQ-6 — protocol-visible retry guidance;
* OQ-10 — Well-Known URI / registration strategy.

### Final assessment

The v0.1 research cycle concluded with:

```text
PUBLISH_EXPERIMENTAL_V0_1
```

ROP v0.1 was considered coherent enough for publication as an explicitly experimental protocol draft.

It does **not** claim:

* universal undo;
* guaranteed rollback;
* exactly-once reversal;
* distributed transaction semantics;
* arbitrary restoration of external side effects.