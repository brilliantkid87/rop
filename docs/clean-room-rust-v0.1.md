# Clean-Room Rust Validation of ROP v0.1 — Findings Summary

Status: v0.1.2-experimental (2026-08-30). This document records the independent
clean-room validation of the ROP v0.1 specification and its findings, so that
future readers have the full evidence trail.

> ROP is an experimental protocol / proposal / research project.

## 1. Methodology

An independent Rust implementation of ROP v0.1 was built in a separate
workspace (`rop-rust`) under clean-room conditions: the implementer worked
**only** from the published public specification set and fixtures — never from
the Go reference implementation's source code. Interoperability was then
validated black-box: the Rust client exercised a live Go reference server over
HTTP and compared wire behavior against the frozen fixtures.

The Rust repository is a separate workspace; this document records its results
but this repository contains no Rust code, and the regression rerun is
performed separately in that workspace.

## 2. Allowed source set — and the input-set omission (A1)

The intended allowed input set for the clean room was:

- `spec/core.md`, `spec/http-binding.md`, `spec/versioning.md`,
  `spec/openapi-extension.md`, `spec/interoperability.md`;
- `spec/fixtures/v0.1/` (22 fixtures);
- `docs/capability-model.md`, `docs/invariants.md`;
- `README.md`.

**A1 — CLOSED: INPUT_SET_OMISSION.** The first clean-room run omitted
`docs/invariants.md` and `docs/capability-model.md` from its workspace input.
Both documents **were already present in the published ROP v0.1 repository**;
their absence was an input-set error on the clean-room side, not a ROP
publication defect. Once the omitted documents were added to the clean-room
workspace, no Rust implementation behavior needed to change. **A1 must not be
cited as a ROP specification defect.** No ROP specification change was made
for A1.

## 3. Rust implementation scope and results

- All **22 published v0.1 fixtures** parsed successfully.
- **70 tests passed** during live black-box interoperability validation
  against the Go reference server.
- All required interoperability scenarios passed.
- **Zero protocol divergence** between the Rust implementation and the Go
  reference implementation was observed.
- No Go reference protocol bug was observed.

**Conclusion: ROP v0.1 Core is independently implementable from the published
specification set.** This result is limited to one second implementation and
one validation campaign; it does not demonstrate broad industry
interoperability.

## 4. Findings A1–A15 and final classifications

| Finding | Classification (v0.1.2) | Disposition |
|---|---|---|
| A1 | CLOSED — INPUT_SET_OMISSION | See §2. Not a ROP defect; no change. |
| A2 | CLARIFICATION_APPLIED | `malformed-payload` audited: it was emitted by the reference implementation but missing from the normative problem vocabulary. Added explicitly to `spec/core.md` §9 with usage definition and HTTP 400 binding; listed in `docs/capability-model.md` and cross-referenced in `spec/http-binding.md`. No equivalent duplicate problem type was created. |
| A3 | CLARIFICATION_APPLIED | `CONFLICT` documented in `spec/core.md` §4 as an *attempt-level outcome*, not an Action lifecycle state: on conflict the Action remains in / returns to `APPLIED` with no business mutation; the result's `status` field mirrors the resulting Action state. No new Action state added. |
| A4 | CLARIFICATION_APPLIED | `receivedAt` documented in `spec/core.md` §11 as the provider/server-observed receipt time: not client authority, not client-supplied, not required as a public wire field. Eligibility rule unchanged. |
| A6 | CLARIFICATION_APPLIED | Plan wire members fully specified in `spec/http-binding.md` §3 — per-member camelCase name, type, optionality, semantics, and absence behavior (covering the members the report flagged as insufficiently specified). No new plan functionality. |
| A15 | CLARIFICATION_APPLIED | `expiresAt` wording conflict resolved: present **only when an eligibility window applies**; a non-IRREVERSIBLE Action MAY have no window; absence means the Action does not expire solely due to time. `docs/capability-model.md` corrected to match `spec/core.md`. No behavior change. |
| A16 | CLARIFICATION_APPLIED (pre-publication, v0.1.2) | The Rust report noted that `spec/core.md` §4 (as clarified for A3) states a conflict returns the Action to `APPLIED`, while the normative transition table in `docs/invariants.md` lacked the corresponding `REVERSING → APPLIED` row. Fixed: the row was added (attempt concluded as observed CONFLICT, no business mutation) and invariant I-7 was updated to state that `CONFLICT` is an attempt-level outcome, not an Action state. Documentation only; no Go or Rust behavior change. |
| A5, A7–A14 | PENDING_RECONCILIATION | The original finding texts reside in the `rop-rust` workspace and were not available in this repository when this document was written. Per the review instruction, they must be classified (CLARIFICATION_REQUIRED / ALLOWED_IMPLEMENTATION_FREEDOM / DEFER_TO_FUTURE_VERSION / NOT_AN_ISSUE) against the clarified v0.1.2 documents during the Rust regression rerun. To unblock that reconciliation, an independent audit of the same specification surface was performed in this repository and is recorded in §5; findings that correspond to A5/A7–A14 should be mapped against it. |

## 5. Supplementary independent ambiguity audit (this repository)

Performed alongside the A2/A3/A4/A6/A15 clarifications so the reconciliation
in the `rop-rust` workspace has a reference point. Classifications use the
required vocabulary.

| Audit item | Classification | Rationale |
|---|---|---|
| Action ID format constraints (charset/length/URL-path safety) | DEFER_TO_FUTURE_VERSION | Named pre-v1.0 requirement (`spec/interoperability.md`); constraining IDs in a patch would be a wire-adjacent semantic change. |
| `providerId` value format | ALLOWED_IMPLEMENTATION_FREEDOM | Opaque by design (Master Prompt §10); clients must not parse it. |
| Per-Operation verification postcondition wire declaration | DEFER_TO_FUTURE_VERSION | Named pre-v1.0 requirement; verification results are already fully specified on the wire. |
| Error `detail` human language | ALLOWED_IMPLEMENTATION_FREEDOM | Semantics live in the problem `type`; clients MUST NOT parse `detail`. A locale-neutral wording SHOULD may be added in a future draft. |
| `Idempotency-Key` on non-reversal endpoints | ALLOWED_IMPLEMENTATION_FREEDOM | The spec defines the key for reversal requests; other endpoints MAY ignore it. Servers not honoring it there is wire-visible but harmless. |
| `plan-reversal` request body | ALLOWED_IMPLEMENTATION_FREEDOM | The endpoint takes no input; bodies are ignored per unknown-field tolerance. |
| Verification endpoint side-effect freedom | NOT_AN_ISSUE | Already normative: verification MUST NOT create business side effects (`spec/core.md` §7); GET is safe by definition in the binding. |
| `residue[].manualRemediable` absence | NOT_AN_ISSUE | Already specified: optional boolean; absence = unknown ("if known", `spec/core.md` §12.3). |
| HTTP method selection per endpoint | NOT_AN_ISSUE | Fully specified in `spec/http-binding.md` §1–§5. |
| Asynchronous acceptance (202) | DEFER_TO_FUTURE_VERSION | Documented as deliberately unused in v0.1 (`spec/http-binding.md` §8). |

## 6. Final recommendation

**SPEC_NEEDS_CLARIFICATION** — resolved by the v0.1.2-experimental
clarification-only patch. No protocol semantic change was required: the Rust
implementation interoperated with the Go reference without modification, and
every applied clarification documents existing or already-permitted behavior.
The post-clarification Rust regression rerun returned
**SPEC_SUFFICIENT_AFTER_CLARIFICATION**, with one remaining documentation
inconsistency (A16 — transition-table omission) fixed before tagging.

## 7. Statement

ROP v0.1 Core proved **independently implementable** from the published
specification set by a second implementation in a different language under
clean-room conditions. This is evidence of specification quality; it is not a
claim of broad industry interoperability, which would require more independent
implementations and real adoption.
