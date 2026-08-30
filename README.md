# ROP — Reversible Operations Protocol

> ROP is an **experimental protocol / proposal / research project**. It is not a
> universal rollback standard, does not guarantee undo, and cannot reverse arbitrary
> side effects.

## What ROP is

ROP explores whether the reversibility of side-effecting operations can be exposed as
a small, interoperable, machine-readable protocol primitive — without becoming a
distributed transaction manager or workflow engine.

**Approved thesis (2026-08-30 architecture review, MODIFY):** interoperable
*description, recording, evaluation, and verification* of the reversibility of
side-effecting operations. How a provider actually reverses something — its existing
refund, delete, or restore API — stays the provider's business.

**Release:** `0.1.2-experimental` (implementation/release version; see `VERSION`
and `CHANGELOG.md`; clarification-only patch — protocol wire version remains
`0.1`, wire-compatible with `0.1.0`/`0.1.1`). Independently validated by a
Rust clean-room implementation: `docs/clean-room-rust-v0.1.md`. The **protocol version** advertised on the wire is `0.1` —
the distinction is defined in `spec/versioning.md` §4, which also records the
freeze of v0.1 semantics. The `/.well-known/rop` URI is **experimental and
currently unregistered**; IANA registration is future governance work
(`spec/http-binding.md` §10).

## What ROP is not

Not a distributed transaction manager, two-phase commit, Saga replacement, workflow
engine, scheduler, batch rollback mechanism, undo system, or universal rollback
standard. ROP does not claim exactly-once execution, does not erase history, and
treats unknown outcomes as unknown — never as failure.

## Core concepts

- **Operation** — a reusable behavior definition (e.g. `payment.charge`); carries
  *capability* metadata: reversibility class, guarantee, eligibility window.
- **Action** — one concrete execution of an Operation; carries *current eligibility*,
  evaluated by server time. Operation metadata never rewrites Action history.
- **Reversal Attempt** — one attempt to reverse an Action; a first-class entity with
  a durable provider execution identity.
- **Reversibility classes** — `REVERSIBLE` (provider can satisfy the semantic
  inverse), `RESTORABLE` (a captured prior state can be restored, version-anchored),
  `COMPENSATABLE` (a forward operation offsets the effect), `PARTIALLY_COMPENSATABLE`
  (some effects compensated, others remain as residue), `IRREVERSIBLE`.
- **Guarantees** — `EXACT`, `EVENTUAL`, `BEST_EFFORT`, `MANUAL`, `NONE`.
- **Eligibility** — whether a new reversal may begin; `receivedAt < expiresAt` by
  server time. Expiration never invalidates a reversal already accepted.
- **Action Receipts** — immutable public records of tracked Actions: identity,
  class, guarantee, status, expiry window, residue. Private reversal material
  (previous values, versions, snapshots) is stored provider-side and never exposed.
- **Unknown outcomes** — a lost response is `OUTCOME_UNKNOWN`, not failure; a
  provider-side reconciliation process resolves it through read-only lookups on the
  execution identity.
- **Dependencies** — durable, scope-local safety constraints ("B depends on A"):
  an active dependent blocks A's reversal. ROP refuses unsafe reversal; it never
  executes dependent reversals automatically.
- **Residue** — provider-declared remaining effects (immutable audit records, fees,
  delivered notifications). Residue is *not* evidence that reversal failed; for
  `PARTIALLY_COMPENSATABLE` Actions the outcome stays `PARTIALLY_REVERSED`,
  distinguishable from full reversal.

## Conformance

A participant claims **ROP v0.1 Core** by serving: a discovery document, Operation
reversibility metadata, durable Action Receipts, current Action eligibility,
provider-defined verification semantics, uncertainty representation, stable problem
types, and evolution tolerance (unknown fields ignored, unknown enums never coerced).

Optional advertised capabilities — never required for conformance:
`planning` (read-only reversal plans), `reversal` (generic invocation through ROP),
`reconciliation` (future). Dependencies and residue are structural in v0.1.
See `docs/capability-model.md`.

## Relation to prior art

The mechanisms are established prior art — compensation (Saga), idempotency keys,
optimistic concurrency, outbox journals, reconciliation; ROP claims no novelty there
(`docs/prior-art.md`). Closest relatives: MicroProfile LRA and WS-BusinessActivity —
both coordinator/participant workflow protocols; ROP has no coordinator, no
enlistment, and no workflow scope. The research question is whether exposing these
semantics consistently at the Operation/Action interoperability boundary creates value
that API documentation cannot. A negative result is acceptable.

## Current limitations

- Single-node SQLite reference implementation; no HA story.
- Reconciliation is internal: no public endpoint or wire contract in v0.1.
- Verification is only as good as the provider's declared postconditions.
- A malicious provider can lie to its own protocol surface; ROP makes lying expensive
  (independent verification), not impossible.
- The missing-receipt window (effect before durable record) is bounded by an intent
  journal, never closed.
- Interoperability value is demonstrated within one implementation; it remains
  contingent on independent adoption.

## Reading the specification

An independent reader can implement the experimental protocol from these
documents alone (see `spec/interoperability.md` for an honest gap list):

- [`spec/core.md`](spec/core.md) — normative semantics: domain model, classes,
  guarantees, states, receipts, idempotency, expiration, dependencies, partial
  compensation, residue, reconciliation, terminology.
- [`spec/http-binding.md`](spec/http-binding.md) — the ROP/HTTP binding:
  endpoints, wire shapes, problem types, status-code semantics, Well-Known URI
  status.
- [`spec/versioning.md`](spec/versioning.md) — evolution rules, the
  `v0.1.0-experimental` freeze, protocol-vs-release version distinction.
- [`spec/openapi-extension.md`](spec/openapi-extension.md) — static preflight
  discovery via `x-rop`.
- [`spec/interoperability.md`](spec/interoperability.md) — what an independent
  implementer can and cannot yet build from the documents.
- [`spec/fixtures/v0.1/`](spec/fixtures/v0.1/) — 22 frozen wire fixtures,
  used by automated compatibility tests.
- [`docs/prior-art.md`](docs/prior-art.md) — Saga, TCC, WS-BusinessActivity,
  MicroProfile LRA, and why ROP diverges.
- [`docs/architecture.md`](docs/architecture.md) — the reference architecture
  and its adversarial self-review.
- [`docs/security.md`](docs/security.md) — threat model and the M6 second-pass
  review against the implemented system.
- [`docs/v0.1-assessment.md`](docs/v0.1-assessment.md) — the final research
  assessment and the publication recommendation.
- [`docs/clean-room-rust-v0.1.md`](docs/clean-room-rust-v0.1.md) — independent
  Rust clean-room validation results and findings.

## Repository layout

```text
cmd/ropd/       reference server: demo resource API + ROP Core under /.well-known/rop
cmd/ropctl/     CLI: inspect / plan / reverse / verify (no batch commands, by design)
internal/       ROP Core (transport-neutral; imports no HTTP — enforced by a test)
pkg/rop/        wire vocabulary: enums with unknown-value tolerance, receipt, plan, problems
examples/resource-api/  demo provider: REVERSIBLE create, RESTORABLE update,
                PARTIALLY_COMPENSATABLE notify, IRREVERSIBLE publish
spec/           draft normative specs (core, http-binding, versioning, openapi-extension,
                interoperability) + frozen v0.1 fixtures
docs/           architecture, capability model, invariants, failure model, security,
                prior art, adversarial cases, open questions, v0.1 assessment, task tracker
migrations/     SQLite schema migrations
VERSION         authoritative release version (0.1.0-experimental)
CHANGELOG.md    release notes
LICENSE / CONTRIBUTING.md / SECURITY.md / AGENTS.md
```

## Running the demo

```bash
go run ./cmd/ropd -addr 127.0.0.1:8080 -db ropd.db -migrations migrations

curl -s http://127.0.0.1:8080/.well-known/rop
curl -si -X POST http://127.0.0.1:8080/resources -d '{"value":"hello"}'   # REVERSIBLE create
curl -si -X PATCH http://127.0.0.1:8080/resources/res_... -d '{"value":"v2"}'  # RESTORABLE update
curl -si -X POST http://127.0.0.1:8080/resources/res_.../notify -d '{"channel":"email"}'  # partial
curl -si -X POST http://127.0.0.1:8080/resources/res_.../publish   # IRREVERSIBLE boundary

go run ./cmd/ropctl -server http://127.0.0.1:8080 inspect  act_...
go run ./cmd/ropctl -server http://127.0.0.1:8080 plan     act_...
go run ./cmd/ropctl -server http://127.0.0.1:8080 -h       # reverse / verify likewise
```

A capability-stripped Core-only instance demonstrates partial conformance:

```bash
go run ./cmd/ropd -no-planning -no-reversal   # optional endpoints → 501 capability-unavailable
```

## Validation

```bash
gofmt -w .
go test ./...
go vet ./...
```

## Key invariants (non-negotiable; see docs/invariants.md)

Original Action history survives reversal · an Action ID is identity, never
authorization · planning has no side effects · unknown outcome is not failure ·
protocol idempotency is not exactly-once · expired Actions cannot begin a new reversal
· conflicts are preferred over destructive restoration · private reversal material is
never exposed publicly · dependencies are safety constraints, not workflow · Core
semantics are transport-neutral.
