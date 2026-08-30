# Independent Implementer Notes — ROP v0.1

Status: M6 (2026-08-30). This document answers one question honestly:

> Could an engineer who has never seen the Go implementation build an
> interoperable ROP v0.1 participant using only the specification and fixtures?

Answer: **for Core, yes with care; for a few specific points, not yet
confidently** — each is listed below with its resolution status.

> ROP is an experimental protocol / proposal / research project.

## Confidently implementable from the documents alone

1. **Discovery document** — shape, vocabulary, and capability flags are fully
   specified (`spec/http-binding.md` §1) and frozen by fixtures
   (`discovery.json`, `discovery-minimal.json`).
2. **Reversibility classes, guarantees, statuses, outcomes** — closed
   vocabularies with exact semantics (`spec/core.md` §3–§4) and unknown-value
   tolerance rules demonstrated by fixtures (`unknown-enum-value.json`).
3. **Action Receipt / status resource** — required fields, wire names, and
   RFC 3339 timestamps are specified and frozen (`receipt-*.json`).
4. **Expiration semantics** — the boundary rule, server-time authority, and
   "expiration stops new reversals, not in-flight ones" are normative
   (`spec/core.md` §11) and fixture-supported.
5. **Problem model** — every condition has a stable `urn:rop:problem:*` type
   (`spec/http-binding.md` §6, `problem-*.json`); status-code mapping is
   audited (`spec/http-binding.md` §8).
6. **Reversal outcomes and uncertainty** — `OUTCOME_UNKNOWN`,
   `PARTIALLY_REVERSED`, and their client obligations are normative
   (`reversal-result-*.json`).
7. **Residue** — the append-style lifecycle (DECLARED / DISCOVERED /
   VERIFIED) and field semantics are specified (`spec/core.md` §12.3).

## Not yet confidently implementable (gaps, honestly listed)

1. **How a provider declares per-Operation verification postconditions on the
   wire.** `x-rop` covers reversibility metadata (class/guarantee/TTL), but
   the postconditions themselves — and the per-operation presence of
   verification — are only demonstrated by the reference implementation.
   Resolution: future `x-rop` extension field or a capability-declared
   postcondition catalog; deferred (M6 finding).
2. **Action ID format constraints.** IDs are opaque strings, but URL-safety
   bounds (charset/length) are not specified; the reference implementation
   bounds echoed input, but an interoperable participant cannot know what ID
   forms are legal. Resolution: recommend specifying "printable ASCII, ≤ 255
   bytes, MUST be URL-path-safe" before any public release.
3. **Business-endpoint receipt embedding.** The `ROP-Action-ID` /
   `ROP-Reversibility` headers are specified, but when a *provider's own*
   business API should emit them (which endpoints count as Action-taking) is
   inherently provider-specific. Documented as such — an implementer must
   decide per endpoint; the spec cannot standardize a provider's business
   surface.
4. **Reconciliation wire contract.** Intentionally absent in v0.1
   (internal-only, OQ-1). An implementer knows from the spec that
   `OUTCOME_UNKNOWN` resolves provider-side; they cannot implement the
   provider side from these documents alone. This is a stated v0.1 boundary,
   not an accident.
5. **Error `detail` language.** The rule "clients MUST NOT parse English
   text" is stated, but nothing prevents a provider from emitting only
   English `detail`. A SHOULD on machine-located `type` plus locale-neutral
   wording would strengthen this; deferred to a future draft.
6. **`.well-known/rop` and `ROP-*` namespace registration** (OQ-10) —
   required before any public, non-experimental release.

## Verdict

A Core (Level 1) participant is implementable today from the specification,
fixtures, and conformance list, with the ID-format caveat above. Implementing
Levels 2–3 (planning, reversal invocation) is implementable from the
documents; implementing a *reconciling provider* is not — that contract is
deliberately unstandardized in v0.1 and marked as such.
