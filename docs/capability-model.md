# ROP Capability Model and v0.1 Conformance Surface

Status: **approved basis** (architecture review decision, 2026-08-30 — "MODIFY
APPROVED"). This document defines the ROP Core, the optional advertised capabilities,
and the minimum meaningful ROP v0.1 conformance surface. The formal normative
specification will live under `/spec/`; where this document uses RFC 2119 language it
is drafting that specification, not adding project-internal rules.

> ROP is an experimental protocol / proposal / research project. It is not a universal
> rollback standard and cannot undo arbitrary side effects.

---

## 1. Principle

> A provider MUST be able to participate meaningfully in ROP without replacing its
> existing provider-specific refund/delete/restore API with a generic ROP `/reverse`
> endpoint.

Reversal *execution* is the provider's business, wherever and however it already
happens. ROP standardizes the **description, recording, evaluation, and verification**
of reversibility around it — so that machine clients can discover capability, observe
what happened, learn current eligibility, see constraints and residue, and evaluate
provider-defined postconditions — without ROP owning or mandating the act itself.

## 2. ROP Core (required for any ROP v0.1 participation)

A Core-conformant participant exposes **all** of the following:

1. **Discovery document** (`GET /.well-known/rop`): `protocol`, `versions`, `binding`,
   `capabilities` object with explicit booleans. Advertised capabilities MUST match
   actual behavior (§20 of the Master Prompt: no vague claims).
2. **Operation-level reversibility metadata**: for each eligible Operation, a
   reversibility class (`REVERSIBLE`, `RESTORABLE`, `COMPENSATABLE`,
   `PARTIALLY_COMPENSATABLE`, `IRREVERSIBLE`), a guarantee (`EXACT`, `EVENTUAL`,
   `BEST_EFFORT`, `MANUAL`, `NONE`), and where applicable a reversal eligibility
   window (TTL). Static preflight MAY be via OpenAPI `x-rop` (OQ-5).
3. **Action Receipts**: an immutable, public, machine-readable record per successfully
   tracked Action, containing at minimum: provider identity, `actionId`,
   `operationId`, `createdAt` (RFC 3339), resource reference, reversibility class,
   guarantee, `status`, `expiresAt` (when the class is not IRREVERSIBLE), and residue
   metadata (possibly empty). Receipts MUST NOT contain reusable privileged reversal
   credentials or private reversal material.
4. **Action-specific current eligibility**: a read representation of the Action's
   current state (`APPLIED`, `REVERSING`, `REVERSED`, `PARTIALLY_REVERSED`,
   `REVERSE_FAILED`, `OUTCOME_UNKNOWN`, `EXPIRED`, `IRREVERSIBLE`) and its
   `expiresAt`, evaluated by **server** time with the boundary
   `receivedAt >= expiresAt ⇒ expired`.
5. **Residue representation**: provider-declared residual effects (free-form in v0.1,
   OQ-3), present wherever reversal results or Action state are reported.
6. **Provider-defined verification semantics**: a representation of verification
   outcomes (`VERIFIED`, `FAILED`, `UNKNOWN`) evaluated against provider-declared
   postconditions, with a declared semantics class (`LOCAL_READONLY`,
   `EXTERNAL_READONLY`, `EVENTUALLY_CONSISTENT`, `EXPENSIVE`). Verification is
   independent of any reversal execution (invariant I-10); a Core-only provider MAY
   verify via its own compensation having taken place out of band.
7. **Explicit uncertainty representation**: `OUTCOME_UNKNOWN` and verification
   `UNKNOWN` are representable and MUST NOT be coerced into failure states.
8. **Error model**: stable machine-readable problem types (RFC 9457 `type` URIs,
   `urn:rop:problem:*` in the reference binding) for the defined conditions; clients
   MUST NOT need to parse English text.
9. **Schema evolution rules**: unknown optional object fields ignored by clients;
   unknown semantic enum values never coerced; versioned discovery.

## 3. Optional advertised capabilities

Each of the following is advertised as an explicit boolean in the discovery
`capabilities` object. A Core-conformant provider MAY advertise none of them. A
client MUST NOT assume any optional capability exists from Core alone.

| Capability | Discovery flag | Meaning when advertised |
|---|---|---|
| **Planning** | `planning` | `POST .../actions/{actionId}/plan-reversal` returns a read-only reversal plan (freshness fields, preconditions, residue, blocking dependencies, conflicts). Produces no external side effects. |
| **Reversal invocation** | `reversal` | `POST .../actions/{actionId}/reverse` requests reversal through ROP. The provider may still perform reversal through its own API instead; ROP invocation is a convenience, not the definition of reversal. |
| **Reconciliation** | `reconciliation` | (future; not in v0.1 reference — reconciliation is an internal domain operation in v0.1, OQ-1) resolution of `OUTCOME_UNKNOWN` through the protocol. |
| **Dependencies** | (v0.1: structural, not advertised) | Action-to-Action safety constraints: an active dependent Action blocks the parent's reversal with `dependency-exists`. Implemented by the reference implementation on all reverse/plan surfaces; a discovery flag is not needed in v0.1 because blocking is reported per-Action through plans and problems. |
| **Residue** | (v0.1: structural, not advertised) | Provider-declared remaining effects on receipts, plans, and partial outcomes. Always representable in v0.1 wire types; presence is per-Action, not a capability switch. |

Rules:

- If a capability is not advertised, requests to its endpoints MUST fail with a
  stable problem type, not silently (behavior of that problem type: OQ-11).
- Advertising `reversal` does not make ROP a workflow engine: still one Action per
  request, no batch, no coordination (§29, §30, §79 of the Master Prompt).
- A provider whose reversal happens entirely through its own existing API can be
  fully Core-conformant, recording Actions, receipts, eligibility, residue, and
  verification as its out-of-band compensation proceeds.

## 4. Conformance levels (v0.1)

```
Level 1 — ROP Core          (mandatory; description/record/eligibility/verify)
Level 2 — Core + Planning   (optional capability)
Level 3 — Core + Reversal invocation (optional capability)
Level 4 — Core + Reconciliation      (future)
```

Levels are cumulative and *per capability*, not hierarchical gates: a provider may
advertise `reversal` without `planning` (e.g. reversal requires no preconditions worth
planning) and `planning` without `reversal` (plan exists, execution lives in the
provider's own API). Discovery booleans are the authority.

## 5. Minimum meaningful ROP v0.1 conformance surface

"Minimum meaningful" = the smallest set a validator could check for Level 1 (Core)
conformance. A v0.1 Core implementation is conformant if and only if:

1. **Discovery**: serves a parseable discovery document with `protocol: "rop"`, a
   `versions` array, `binding`, and a `capabilities` object whose booleans match
   actual behavior.
2. **Receipt correctness**: every tracked Action yields a receipt with all Core
   required fields, RFC 3339 UTC timestamps, stable camelCase wire names (`actionId`,
   `operationId`, `resourceRef`, `reversibility`, `guarantee`, `status`, `expiresAt`,
   `residue`, `createdAt`), and no private reversal material anywhere on public
   paths (invariant I-14).
3. **Class/guarantee vocabulary**: only the five classes and five guarantees are
   emitted; unknown values received are never coerced (`REVERSIBLE` is never
   interpreted from an unknown token).
4. **Eligibility correctness**: reported status and `expiresAt` agree; the expiry
   boundary is server-time and exact (`receivedAt >= expiresAt ⇒ expired`); an expired
   Action cannot begin a new reversal if `reversal` is advertised (I-8).
5. **Uncertainty honesty**: `OUTCOME_UNKNOWN` / verification `UNKNOWN` are
   representable and never auto-converted to failure (I-5, I-10).
6. **Problem types**: the defined conditions return RFC 9457 problems with stable
   `type` URIs, at minimum: `action-not-found`, `reversal-expired`,
   `irreversible-action`, `reversal-conflict`, `reversal-already-in-progress`,
   `authorization-denied`, `precondition-failed`, `verification-failed`,
   `capability-unavailable`.
7. **Evolution tolerance**: parsers ignore unknown optional fields; documents
   round-trip; golden fixtures for the declared version parse.
8. **No false claims**: the implementation MUST NOT claim stronger guarantees than
   advertised, and MUST NOT require any optional capability for Core interop.

Levels 2–3 add to this: plans carry freshness fields, blocking dependencies, and
produce no side effects (I-3); reversal requests are eligible-gated, dependency- and
conflict-checked (I-7, I-12), idempotency-capable (I-21), and never conflate transport
success with semantic outcome (I-4). Unknown outcomes and residue are representable at
every level.

**Claim language:** an implementation claims conformance as exactly
"ROP v0.1 Core" (Level 1) or "ROP v0.1 Core + `<capability>`" per advertised
optional capability. Vague claims such as "ROP-compatible" are not
conformance claims (Master Prompt §20).

The reference implementation (Milestone 1+) implements Levels 1–3 so the full
hypothesis can be tested end-to-end; fixtures under `spec/fixtures/v0.1/` freeze the
Level 1 wire shapes.

## 6. What this deliberately does NOT standardize

- How a provider reverses anything (its own refund/delete/restore APIs are fine).
- Compensation semantics, idempotency mechanics, OCC mechanics, or reconciliation
  algorithms — established techniques, reused per provider (`prior-art.md`).
- Workflow, batch, cancellation, crypto (v0.1 non-goals, Master Prompt §79–§80).
