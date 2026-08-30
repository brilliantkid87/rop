# ROP Core Specification — v0.1 (draft)

Status: draft normative specification for the approved (MODIFY, 2026-08-30)
scope. Normative-language source of truth for semantics; the capability model
in `docs/capability-model.md` defines Core vs optional capabilities and is
reflected here.

> ROP is an experimental protocol / proposal / research project. It is not a
> universal rollback standard and cannot undo arbitrary side effects.

## 0. Normative language

The key words MUST, MUST NOT, REQUIRED, SHALL, SHALL NOT, SHOULD, SHOULD NOT,
RECOMMENDED, MAY, OPTIONAL are to be interpreted as described in RFC 2119 and
RFC 8174 when, and only when, they appear in all capitals. Normative language
MUST NOT be used for implementation preferences.

## 1. Scope and thesis

ROP standardizes the **interoperable description, recording, evaluation, and
verification of the reversibility of side-effecting operations**. It does not
standardize how reversal is performed; a provider's existing refund, delete,
or restore API remains the reversal mechanism. ROP does not standardize
provider business semantics (Master Prompt §7).

ROP v0.1 MUST NOT define batch reversal, cancellation, reversal-of-reversal,
workflow coordination, or cryptographic receipt integrity.

## 2. Domain model

- An **Operation** is a reusable behavior definition. Operation metadata
  describes *capability*.
- An **Action** is one concrete execution of an Operation. Action state
  determines *current eligibility*. Implementations MUST NOT evaluate a
  historical Action's eligibility solely from current Operation metadata (§53
  of the Master Prompt): Action-time metadata is authoritative for history.
- A **Reversal Attempt** is one attempt to reverse an Action. It is a
  first-class entity and MUST NOT be represented as mutable fields on Action.
- Action identity is `(provider, actionId)`. An `actionId` MUST NOT be assumed
  globally unique across providers (§10).

## 3. Reversibility classes and guarantees

Reversibility class (exactly five; unknown received values MUST remain
unknown and MUST NOT be coerced):

```text
REVERSIBLE  RESTORABLE  COMPENSATABLE  PARTIALLY_COMPENSATABLE  IRREVERSIBLE
```

`REVERSIBLE` MUST NOT be interpreted as "the original Action never happened".

### 3.1 Restoration semantics (RESTORABLE)

For a `RESTORABLE` Action the provider captures, as **private reversal
material**, the minimum prior state required for a safe restore (for example
the prior value and version). Implementations:

- MUST anchor restoration on a compare-and-swap against the resource version
  the Action produced; if any other Action mutated the resource since, the
  restoration MUST be refused with the `reversal-conflict` problem rather
  than silently overwriting the intervening state (Master Prompt §41, §48).
- MUST revalidate correctness-critical preconditions at execution time; a
  plan — however fresh it looked — is never authorization (invariant I-19).
- MUST define the restored state explicitly (the reference implementation
  restores both the captured prior value and the captured prior version) and
  expose verification postconditions for the restoration (§7).
- MAY treat a successful restoration as re-enabling earlier reversals whose
  CAS anchors match the restored version: undo of B (v2→v3) restores v2,
  which is exactly the precondition for undoing A (v1→v2). This sequential
  undo is sound because each restoration is itself version-anchored and
  verified; it is not batch rollback (§30).
- MUST keep the captured prior state private: it belongs to reversal
  material, never to receipts, plans, or verification responses (§5, §13).

Guarantee (exactly five; unknown received values remain unknown):

```text
EXACT  EVENTUAL  BEST_EFFORT  MANUAL  NONE
```

Implementations MUST NOT claim a stronger guarantee than the provider
declared.

## 4. Action states

```text
APPLIED  REVERSING  REVERSED  PARTIALLY_REVERSED  REVERSE_FAILED
OUTCOME_UNKNOWN  EXPIRED  IRREVERSIBLE
```

- State transitions MUST be validated against the normative transition table
  (`docs/invariants.md`). Illegal transitions are implementation errors.
- `OUTCOME_UNKNOWN` MUST NOT be converted into `REVERSE_FAILED` without
  evidence (§34). Unknown is a legitimate outcome.
- Expiration is evaluated by server time with the exact boundary
  `receivedAt >= expiresAt ⇒ expired` (§24). Expired Actions MUST NOT begin a
  new reversal; an in-flight attempt at expiry MAY finish (§52).
- Original Action history MUST be preserved across reversal (append-only
  journal; §56). Reversal MUST NOT erase or rewrite history.

## 5. Receipts

A tracked Action SHOULD produce an immutable public Action Receipt containing
at minimum: `providerId`, `actionId`, `operationId`, `createdAt` (RFC 3339,
UTC), `resourceRef` (`resourceType` + `resourceId`, opaque), `reversibility`,
`guarantee`, `status`, `expiresAt` (when a window applies), and residue
representation where applicable. Receipts MUST NOT contain reusable privileged
reversal credentials or private reversal material (§12, §13). The public
receipt/private material distinction is structural: private material (previous
resource versions, snapshots, provider transaction references) lives only in
provider-side storage (§13).

## 6. Capabilities (normative summary)

- The following are **Core** and mandatory for v0.1 conformance: discovery
  document, Operation metadata, Action Receipts, current Action
  status/eligibility, verification semantics, uncertainty representation,
  problem types, evolution rules.
- The following are **optional advertised capabilities**: `planning`,
  `reversal` (generic invocation), `reconciliation`. A provider MAY advertise
  none of them and remain conformant. Requests to a non-advertised
  capability's endpoint MUST fail with a stable problem type, not silently
  (OQ-11). Dependencies and residue are structural in v0.1: representable
  everywhere, no capability switch.
- `reversal` advertising does not require the provider to route its actual
  reversal through ROP.

## 7. Verification

Verification evaluates provider-defined postconditions and MUST be
independent of reversal invocation success (§46). Every verification result
MUST carry a semantics class (`LOCAL_READONLY`, `EXTERNAL_READONLY`,
`EVENTUALLY_CONSISTENT`, `EXPENSIVE`) and MAY be `UNKNOWN` when the
verification's own evaluation fails (§47). Verification SHOULD NOT create
business side effects.

## 8. Evolution

- Clients SHOULD ignore unknown optional object fields (§21).
- Clients MUST NOT silently interpret an unknown semantic enum value as an
  existing value; unknown values remain unknown.
- Discovery MUST expose supported protocol versions. Fixtures for released
  versions are frozen under `spec/fixtures/v0.1/` (§22, §64); parsers SHOULD
  be tested against them (golden compatibility).

## 9. Problem types

Errors MUST be machine-readable (§63; RFC 9457 on the HTTP binding). The
stable v0.1 problem types:

```text
action-not-found  reversal-expired  irreversible-action  reversal-conflict
reversal-already-in-progress  authorization-denied  precondition-failed
verification-failed  capability-unavailable  idempotency-key-conflict
dependency-exists
```

Clients MUST NOT need to parse human-readable messages to understand
semantics. Transport failure MUST be distinguishable from ROP semantic
outcome (§28, §63).

## 10. Reversal-request idempotency

Reversal requests SHOULD support an `Idempotency-Key` (Master Prompt §36):

- Idempotency state MUST be durable and MUST survive process restart.
- Two equivalent requests carrying the same key MUST NOT create two
  independent reversal executions; a replay returns the recorded result
  (reconstructed from durable attempt state if the original response was
  lost). A replay of an attempt whose outcome is unobserved returns
  `OUTCOME_UNKNOWN` — never a re-execution, never a fabricated outcome.
- The idempotency record MUST map `(scope, actionId, idempotencyKey)` to the
  reversal attempt/result, with a database uniqueness constraint enforcing
  the invariant (not only application checks).
- Keys MUST NOT collide across authorization scopes: the same textual key in
  a different scope is an independent record. Within one scope, reuse of a
  key for a materially different request (a different Action) MUST be
  rejected with the `idempotency-key-conflict` problem type.
- ROP request idempotency is not exactly-once provider execution (§35), and
  it is distinct from any provider-level idempotency the provider's own
  compensation API may require. Raw keys SHOULD NOT be persisted; store a
  hash.

## 11. Expiration semantics

Expiration controls whether a new reversal may begin; it does not invalidate
a reversal already accepted before the deadline (Master Prompt §52):

- `receivedAt < expiresAt` — a new reversal may begin;
  `receivedAt >= expiresAt` — it may not. Server time only (§24).
- An Action without an expiry window never expires.
- Expiry is derived from durable state and server time, so it applies
  identically across restarts.
- An expiry sweeper MUST only transition `APPLIED → EXPIRED`: it MUST NOT
  touch in-flight (`REVERSING`, `OUTCOME_UNKNOWN`) or concluded attempts, and
  an accepted reversal may finish after the deadline has passed.
- An expired Action remains inspectable; planning and verification remain
  available where they are semantically meaningful, and MUST report the
  expired state honestly (a plan for an expired Action carries an explicit
  conflict entry; verification reports actual postcondition state).

## 12. Dependencies, partial compensation, and residue

### 12.1 Dependencies are safety constraints, not workflow ownership

An Action-to-Action dependency records that reversal of the **parent** may be
unsafe while the **dependent**'s effect still stands: "B depends on A".
Implementations MUST treat dependencies exclusively as safety constraints:
they inform planning and gate reversal — ROP MUST NOT execute dependent
reversals automatically, order work, or schedule anything (Master Prompt
§49; no reverse-topological execution, no Saga orchestration).

- Dependencies MUST be durable and scope-local; an edge referencing an
  Action outside the scope is rejected (I-13).
- Dependency cycles (direct or indirect) MUST be rejected at write time: a
  graph that makes safe reversal reasoning impossible is not accepted.
  Duplicate edges are the same fact and MUST be handled safely
  (idempotent).
- v0.1 behavior when an active dependent exists: reversal of the parent is
  rejected with the `dependency-exists` problem. Execution MUST re-check
  dependencies independently of any plan; a dependency created after
  planning still blocks (I-19).

**Active-dependent rule (documented reference decision, OQ-15):** a
dependent is *active* — and blocks its parent — while its own effect still
stands: status in `APPLIED`, `REVERSING`, `OUTCOME_UNKNOWN`,
`REVERSE_FAILED`, `EXPIRED`, `IRREVERSIBLE`. Dependents in `REVERSED` or
`PARTIALLY_REVERSED` have been compensated and stop blocking. This rule is
deliberately documented rather than accidental; providers MAY adopt stricter
rules in future versions.

### 12.2 Partial compensation

A `PARTIALLY_COMPENSATABLE` Action produces multiple effects, of which
reversal compensates some but cannot remove all. The reversal outcome MUST
be reported as `PARTIALLY_REVERSED` — never `REVERSED` — and the remaining
effects MUST be represented as residue. Implementations MUST NOT label an
ordinary successful REVERSIBLE or RESTORABLE reversal as partial merely to
exercise the enum.

### 12.3 Residue

Residue is first-class, provider-declared evidence of what remains after or
despite reversal (Master Prompt §45). **Residue is not evidence that reversal
failed**: a reversal may satisfy its provider-defined semantic contract while
residue remains. For `PARTIALLY_COMPENSATABLE` Actions, however, the
`PARTIALLY_REVERSED` semantic outcome MUST remain distinguishable from full
reversal.

- A residue item describes concretely what remains, whether it was expected
  (known before reversal), that it is provider-defined, and — when known —
  whether manual intervention can change it. Sensitive business payloads
  MUST NOT be used as residue descriptions (§13, §14).
- Residue is recorded append-style with its lifecycle stage: `DECLARED`
  (known before reversal, exposed to planning), `DISCOVERED` (recorded
  during reversal), `VERIFIED` (confirmed during verification). Planning is
  NOT required to know every future residue item; later discovery adds
  history, it never overwrites.
- Verification for a `PARTIALLY_COMPENSATABLE` Action evaluates the
  provider-defined *partial* postcondition — the compensated effects are
  gone AND the declared residue remains — and MUST NOT upgrade the outcome
  to `REVERSED`.

Reconciliation is a first-class domain behavior for resolving
`OUTCOME_UNKNOWN` attempts (Master Prompt §38). v0.1 keeps it **internal**:
there is no public `/reconcile` endpoint (OQ-1).

- **Provider execution identity:** every Reversal Attempt carries a durable,
  stable provider reference assigned before any provider call. Providers own
  the semantics of that reference; reconciliation asks "what happened to
  provider operation X?" through a read-only provider lookup — it NEVER
  re-invokes the reversal side effect merely because the result is unknown.
- **Observations are durable and append-only:** each reconciliation round
  records an observation (`PROVEN_REVERSED`, `PROVEN_NOT_REVERSED`, or
  `INCONCLUSIVE`) before any state transition, so the evidence chain survives
  crashes.
- **Transitions are evidence-gated:** the attempt moves to `REVERSED` or
  `REVERSE_FAILED` only when the provider adapter's contract marks the
  evidence as **proven**. A negative lookup ("not found") is NOT proof of
  non-execution unless the provider contract explicitly guarantees that
  conclusion; unproven evidence keeps the attempt
  `AWAITING_RECONCILIATION` and the Action `OUTCOME_UNKNOWN` (§34).
- **Reconciliation is idempotent:** repeated rounds are safe; a concluded
  attempt replays its recorded result without new provider interaction.
- **Reconciliation and verification are distinct:** reconciliation asks
  "what happened to the reversal execution?"; verification asks "does the
  provider-defined semantic postcondition currently hold?" (§7). Verification
  MAY supply information useful to an operator, but the reference
  implementation does not conclude an unknown attempt from verification
  evidence alone (OQ-14).
- **Classification is internal:** the failure taxonomy (RETRYABLE /
  NON_RETRYABLE / RECONCILE_REQUIRED / MANUAL_INTERVENTION_REQUIRED, Master
  Prompt §37) drives behavior and is recorded durably on attempts; it is not
  a protocol enum. An unclassified provider error is treated as
  unobservable (reconciliation required), never as semantic failure.
- **Crash recovery preserves uncertainty:** after a restart, an attempt found
  in-flight is parked as `AWAITING_RECONCILIATION` / `OUTCOME_UNKNOWN` —
  recovery MUST NOT convert `REVERSING` into `REVERSE_FAILED` (§60), and MUST
  NOT silently discard persisted attempts.

## 13. Terminology

Used consistently across this specification and the reference implementation:

- **Operation** — a reusable behavior definition; carries capability metadata.
- **Action** — one concrete execution of an Operation; carries current eligibility.
- **Reversal Attempt** — one attempt to reverse an Action; first-class entity.
- **reversal** — the generic protocol verb: acting on the effects of an Action
  that already occurred. The semantic class of the underlying behavior is given
  by the Action's reversibility class.
- **restoration** — reversal of a `RESTORABLE` Action: a previously captured
  valid state is restored (version-anchored; see §3.1).
- **compensation** — reversal of a `COMPENSATABLE` Action: a new forward
  operation offsets the original effect; the original remains historically true.
- **partial compensation** — reversal of a `PARTIALLY_COMPENSATABLE` Action:
  some effects compensated, others remain as residue; outcome is
  `PARTIALLY_REVERSED`, never `REVERSED`.
- **residue** — provider-declared remaining effects after or despite reversal;
  NOT evidence that reversal failed (§12.3).
- **verification** — evaluating provider-defined postconditions against current
  state (§7); independent of reversal execution.
- **reconciliation** — resolving an `OUTCOME_UNKNOWN` attempt through read-only
  provider lookups on the execution identity (§12.4); independent of verification.
- **eligibility** — whether a new reversal may begin for an Action, evaluated
  by server time against state, expiry, and constraints (§11).

The words *undo*, *rollback*, *reverse*, *restore*, and *compensate* MUST NOT
be used interchangeably in normative text: `reverse` is the protocol verb; the
others name specific semantic classes or are non-ROP vocabulary.

## 14. Invariants

The invariants in `docs/invariants.md` (I-1..I-20) are normative for
implementations. A conformance checker MAY test any of them; the minimum
conformance surface in `docs/capability-model.md` §5 is normative for "ROP
v0.1 Core conformant".
