# ROP Architecture

Status: **approved with modifications** (architecture review decision, 2026-08-30 —
"MODIFY APPROVED"). Companions: `capability-model.md` (normative capability and
conformance model), `prior-art.md`, `invariants.md`, `failure-model.md`,
`security.md`, `adversarial-cases.md`, `open-questions.md`.

> ROP is an experimental protocol / proposal / research project. It is not a universal
> rollback standard, does not guarantee undo, and cannot reverse arbitrary side effects.

---

## 1. Research hypothesis

**Approved thesis (2026-08-30):**

> Interoperable description, recording, evaluation, and verification of the
> reversibility of side-effecting operations.

Operationalized: ROP succeeds (Master Prompt §88) if an independent client can learn
that an Operation is generally restorable, observe that a concrete Action occurred,
learn its current eligibility, and evaluate provider-defined verification — all
without private knowledge of the provider's compensation logic. Reversal *invocation*
is an optional capability (see `capability-model.md`); a provider MUST be able to
participate meaningfully in ROP without replacing its existing refund/delete/restore
API with a generic ROP `/reverse` endpoint.

The hypothesis decomposes into two ordered claims:

- **C1 (metadata):** machine-readable reversibility metadata (class, guarantee, TTL,
  reverse operation) is valuable pre-execution and cheap to standardize.
- **C2 (receipt/lifecycle):** a standardized Action Receipt + Action state machine +
  eligibility model is valuable because eligibility (expiration, constraints,
  conflicts, unknown outcomes) is inherently *per-Action* and cannot live in metadata.

**C3 (invocation) — previously listed here — is demoted to an optional advertised
capability** per the approved review decision, retained in the reference
implementation so the end-to-end path can be tested, but never a conformance
requirement. ROP does **not** claim novelty for the underlying mechanisms
(`OUTCOME_UNKNOWN`, idempotency, reconciliation, compensation, optimistic
concurrency) — those are established distributed-systems techniques reused as such
(`prior-art.md`); ROP's hypothesis is whether exposing the relevant semantics
consistently at the Operation/Action interoperability boundary creates useful new
value.

## 2. Domain model

Strict separation (Master Prompt §5, AGENTS.md):

- **Operation** — reusable behavior definition (e.g. `payment.charge`). Carries
  *capability* metadata: reversibility class, guarantee, TTL, reverseOperationId.
  Source: provider declaration (`x-rop`, discovery registry).
- **Action** — one concrete execution of an Operation (`act_01K...`). Carries *current
  eligibility*, which is a function of Action state, Action-time metadata, server time,
  dependencies, and provider state — never of Operation metadata alone (§53).
- **Reversal Attempt** — one attempt to reverse one Action (`ra_01K...`). First-class
  entity (§32); history of attempts is append-only; not mutable fields on Action.

Identity: `(ProviderIdentity, ActionId)` is the conceptual key (§10). The HTTP binding
makes provider identity implicit (one base URL); the Core types carry an explicit
provider ID. Resource references are opaque `(resourceType, resourceId)` or opaque
strings; never semantically tied to HTTP routes (§11).

## 3. Semantic equivalence

ROP standardizes *discovery, representation, invocation, lifecycle, reconciliation,
verification, reporting* — never business meaning (§7). "Reversal succeeded" means the
provider-defined postcondition holds (e.g. `refund exists && amount == captured &&
payment.state == REFUNDED`), which never implies "the charge never happened". Clients
MUST NOT guess provider semantics; they may only evaluate the provider's declared
postconditions through verification results.

## 4. Action state machine

States (§33):

```
APPLIED          Action recorded; reversal may be planned/requested
REVERSING        A Reversal Attempt is in flight for this Action
REVERSED         Provider-defined postcondition verified (or provider-confirmed)
PARTIALLY_REVERSED  Some effects compensated; known residue represented
REVERSE_FAILED   Attempt concluded, postcondition not achieved (observed, not assumed)
OUTCOME_UNKNOWN  Attempt dispatched; result not observed; reconciliation required
EXPIRED          Reversal eligibility window passed (server time)
IRREVERSIBLE     Operation class IRREVERSIBLE (or capability since removed)
```

Transition rules (normative summary; full table in `invariants.md` §"transitions"):

- `APPLIED → REVERSING` — only if eligible (state APPLIED, not expired, no blocking
  dependency, class ≠ IRREVERSIBLE, authorization present). Durable write of the
  Reversal Attempt (state=RUNNING) precedes any provider call.
- `REVERSING → REVERSED | PARTIALLY_REVERSED | REVERSE_FAILED | OUTCOME_UNKNOWN` —
  set only from *observed evidence* (provider response or verification result).
- `OUTCOME_UNKNOWN → REVERSED | PARTIALLY_REVERSED | REVERSE_FAILED | REVERSING` —
  only via reconciliation/verification evidence. Unknown is a terminal-until-reconciled
  state; it is NEVER mapped to REVERSE_FAILED by timeout alone (§34, §60).
- `APPLIED → EXPIRED` — server-time transition; boundary `receivedAt >= expiresAt` is
  expired (§24, §52). EXPIRED is absorbing for *new* reversals; an in-flight attempt at
  expiry MAY finish (reversal-began-before-expiry semantics).
- All transitions validated in one centralized transition table (single Go function);
  invalid transitions are programmer errors, not runtime choices.

## 5. Reversal Attempt model

Fields (§32): `attemptId`, `actionId`, `requestedAt`, `requester` (if available),
`idempotencyKey` (hash — keys themselves are not stored in the clear), `executionState`
(PENDING / RUNNING / AWAITING_RECONCILIATION / CONCLUDED), `providerRef` (provider's
transaction/reference ID for the compensation), `observedResult`, `error`,
`verificationStatus`. Multiple attempts per Action are representable; `REVERSING` on the
Action is a projection of "exists a non-concluded attempt".

## 6a. Dependencies, partial compensation, and residue (implemented in M5)

**Dependencies** (`internal/dependency`) are durable, scope-local safety
constraints, never workflow ownership: "B depends on A" gates A's reversal
while B's effect stands; ROP refuses (`dependency-exists`, 409) but never
executes dependent reversals, orders work, or schedules anything. The
documented active-dependent rule: `REVERSED` / `PARTIALLY_REVERSED`
dependents are resolved and stop blocking; all other statuses block
(OQ-15 — a documented decision, provider-overridable in future versions).
Cycles (direct, indirect, self) are rejected at the domain layer with a
durable `UNIQUE(parent, dependent)` backstop; duplicate edges are idempotent;
cross-scope edges are rejected. Planning exposes blocking dependencies via
`Plan.BlockingDependencies`; reversal execution re-checks independently, so a
dependency created after planning still blocks (I-19).

**Partial compensation** is demonstrated by a genuine persisted scenario,
`resource.notify`: the Action produces a mutable effect (notification state,
withdrawable) and an immutable effect (the delivery record). Its reversal
withdraws the former and returns `PARTIALLY_REVERSED` with residue — never
`REVERSED`. Verification evaluates the *partial* postcondition (withdrawn +
delivery record remains) and never upgrades the outcome.

**Residue** is append-style (`action_residue`: DECLARED / DISCOVERED /
VERIFIED) — planning exposes declared residue; discovery during reversal adds
history without overwriting; receipts aggregate records. Residue is NOT
evidence of reversal failure.

## 6. Reconciliation (implemented in M4, internal)

First-class (§38): `OUTCOME_UNKNOWN` is resolved only by reconciliation — a read-only
provider lookup on the attempt's durable execution identity (`rop-rev-<attemptId>`,
assigned before any provider call; distinct from the HTTP `Idempotency-Key`). The
internal reconciliation service (`internal/reconciliation`) is evidence-gated: every
round appends a durable observation (`PROVEN_REVERSED` / `PROVEN_NOT_REVERSED` /
`INCONCLUSIVE`) *before* any transition; the attempt concludes only when the provider
adapter's contract marks the evidence proven. A negative lookup without a
contract-guaranteed conclusion stays inconclusive — "provider did not find it" is not
"the reversal never happened" (§34). Reconciliation never re-invokes the reversal side
effect, is idempotent (a concluded attempt replays its recorded result), and there is
deliberately no public `/reconcile` endpoint or CLI verb in v0.1 (OQ-1). Verification
remains distinct (OQ-14 documents that verification evidence alone does not conclude an
unknown attempt in the reference implementation).

## 7. Planning

Planning (§39) is read-only: it runs provider `PlanFunc` over current durable state and
MUST NOT cause external side effects. A plan is a snapshot: `generatedAt`,
`basisResourceVersion` (where available), `validUntil`. It lists preconditions,
expected reversal, dependencies, known residue, known conflicts, manual requirements —
concrete facts, never numeric risk scores. A plan is not authorization (§40): reversal
execution re-validates all correctness-critical preconditions against live state; a
stale plan yields CONFLICT and replanning, never blind restoration (§41).

## 8. Persistence (SQLite)

Tables (§72): `operations`, `actions`, `action_dependencies`,
`reversal_material`, `reversal_attempts`, `idempotency_keys`,
`verification_results`, plus `schema_migrations` and an `intent_journal`
(outbox-style, see §9). Migrations are ordered SQL files under `migrations/`.

- Actions are insert-only; status changes are durable updates *plus* an append-only
  status-history record (audit survives reversal, §56). A reversal does not delete or
  overwrite original rows.
- `reversal_material` (private: previous resource version, snapshot ref, prior
  configuration, provider transaction ref, reconciliation key) is provider-side state,
  stored separately from receipts and never serialized into any public response (§13).
- Retention (§15): three independent clocks — reversal eligibility (TTL), audit
  retention (default 90 days), private material retention (tracks eligibility, default
  24h). No table requires indefinite storage; expiry jobs may purge private material
  but MUST NOT purge audit records.
- Constraints: FK from attempts→actions; partial-unique index enforcing at most one
  non-concluded attempt per action (idempotent retry dedupes on this);
  CHECK constraints on enum columns protect against invalid states at rest.

Data minimization (§14): store identity, auditability, reversal-safety, and
reconciliation keys only; never full business payloads by default. Each column added to
a table must carry a comment stating why it is required.

## 9. Transaction boundaries and crash consistency (§57–§60)

Honest statement: for the reference implementation, business state (the demo resource
API) and the ROP journal share one SQLite database, so "business state + Action journal"
writes are locally atomic via a single transaction. For *external* providers this is
impossible and MUST NOT be pretended.

Ordering rules:

1. **Action-taking path:** inside one transaction, persist business change + Action +
   Receipt + private reversal material + intent-journal entry (state=INTENT). Commit.
   Then perform any external effect (none in the local demo). The intent journal bounds
   the §58 "effect without receipt" window to "external effect done, journal write
   lost" — a real, unreduced-by-protocol residue that startup recovery scans for
   (INTENT rows older than a threshold ⇒ reconcile). ROP does not claim to eliminate
   it (§58).
2. **Reversal path:** transaction A persists the Reversal Attempt (RUNNING) and sets
   Action→REVERSING. Then call the provider. Then transaction B records the observed
   result and (optionally) verification. Crash points analyzed exhaustively in
   `failure-model.md`; the headline cases:
   - crash before A commits: nothing happened; retryable.
   - crash after A, before provider call: attempt RUNNING with the execution
     identity persisted. No durable marker distinguishes this from the next
     case, so recovery preserves uncertainty: the attempt is parked as
     AWAITING_RECONCILIATION and the Action becomes OUTCOME_UNKNOWN;
     reconciliation resolves it through a provider lookup on the execution
     identity (M4 behavior, superseding the earlier re-execute sketch).
   - crash after provider success, before B: identical recovery path — parked
     as OUTCOME_UNKNOWN and reconciled (never REVERSE_FAILED, §60). The
     provider's record of the execution identity proves what happened.

## 10. Concurrency, idempotency, retries

- **Idempotency (§36, implemented in M3):** reversal requests accept
  `Idempotency-Key`. The mapping `(scope, actionId, keyHash) -> attemptId` is durable
  (`idempotency_keys` table, migration 0002) and registered in the same transaction
  that creates the attempt, so key registration and execution start commit atomically.
  A `UNIQUE(scope, key_hash)` index enforces the invariant at the database level:
  replays return the recorded result (reconstructed from attempt + action state —
  including `OUTCOME_UNKNOWN` replays of parked attempts), concurrent same-key requests
  converge on one execution, and reuse of a key for a different Action in the same scope
  is rejected (`idempotency-key-conflict`). Keys are stored only as SHA-256 hashes
  (data minimization, §14). Idempotency protects ROP handling only; provider adapters
  own their own idempotency (they send a deterministic provider idempotency reference
  derived from attemptId). No exactly-once claim is made (§35).
- **TOCTOU/concurrent mutation (§41, §48):** reversal against a resource whose current
  version ≠ plan's basis version (or whose state was mutated by another Action since)
  yields `409` problem `reversal-conflict`; the provider adapter enforces
  compare-and-swap where the resource model has versions. CONFLICT over destructive
  restoration, always.
- **Expiration hardening (M3):** the sweeper transitions only `APPLIED → EXPIRED`
  (`expires_at <= now`, server time, SQL NULL = no window); it structurally cannot
  corrupt in-flight or concluded attempts because the transition table has no exit
  from `REVERSING`/`OUTCOME_UNKNOWN`/concluded states to `EXPIRED`. A reversal accepted
  before expiry finishes under its own semantics (§52 recommended interpretation,
  tested with a clock that moves past the deadline mid-flight). Expired Actions remain
  inspectable; planning and verification stay available and honest (plan carries an
  explicit conflict; verification reports actual postcondition state).

- **Retry taxonomy (§37, implemented in M4):** providers classify failures via
  `operation.ProviderFailure` (`RETRYABLE` / `NON_RETRYABLE` / `RECONCILE_REQUIRED` /
  `MANUAL_INTERVENTION_REQUIRED`); the class is recorded durably on the attempt.
  Behavior follows semantics: RETRIABLE (failed before any effect) leaves the Action
  APPLIED and re-requestable — by an explicit new request, never an automatic loop;
  NON_RETRIABLE (definite rejection) concludes REVERSE_FAILED terminally; anything
  unobservable, including unclassified errors and transport timeouts, parks the attempt
  for reconciliation. Not exposed as protocol enums (pending OQ-6). Transport-level
  throttling (429) is RETRYABLE and never evidence of semantic reversal failure (§55).

## 11. Time semantics (§24, §52)

RFC 3339 UTC everywhere; clock injected via `internal/clock` for deterministic tests.
Server time is authoritative for creation, expiry, and acceptance. Boundary:
`receivedAt < expiresAt` ⇒ eligible; `receivedAt >= expiresAt` ⇒ expired — tested
exactly at the boundary. Expiry stops *new* reversals; an attempt already RUNNING at
expiry completes under its own semantics (documented choice from §52's recommended
interpretation).

## 12. HTTP binding (ROP/HTTP v0.1) — and its honest limits

Endpoints (§28): `GET /.well-known/rop` (discovery, §20), `GET
/.well-known/rop/actions/{actionId}`, `POST .../plan-reversal`, `POST .../reverse`,
`GET .../verification`. Errors as RFC 9457 with stable problem `type` URIs (§63).
Receipt metadata on business responses as headers (`ROP-Action-ID`, `ROP-Reversibility`)
plus an embeddable JSON receipt (§12). Transport success ≠ semantic success: `200`
from reverse means "attempt accepted/observed result recorded", with the semantic
outcome in the body (`status: OUTCOME_UNKNOWN` is a possible 200).

**Status after approved review:** this binding's planning and reversal-invocation
endpoints are **optional advertised capabilities** (`capability-model.md` §4), not
conformance requirements — the invocation surface competes with every provider's
existing compensation API, so the protocol is structured so that a world where C3
fails can shed it and keep C1+C2 as the Core. The reference implementation exercises
the full path (including internal reversal execution) to test the hypothesis; the
specification never makes `/reverse` mandatory.

## 13. Schema evolution (§21–§23)

Wire JSON: camelCase stable names (§62: `actionId`, `attemptId`, `expiresAt`, ...).
Clients SHOULD ignore unknown optional object fields; clients MUST treat unknown enum
values as unknown (error or "unknown" passthrough — never coerced to an existing
value). Versioning rules in `spec/versioning.md` (to be authored in the spec phase);
v0.1 fixtures frozen under `spec/fixtures/v0.1/` and used in golden tests. Discovery
exposes `versions: ["0.1"]`; multi-version support is designed-for, not built.

## 14. Authorization and scope (§16, §17, §54)

Authorization is an interface (`internal/authz`) evaluated per verb: inspect, plan,
reverse, verify, reconcile. Possession of an Action ID grants nothing. The reference
implementation runs single-tenant with a trivial principal, but scope is a column from
day one (`actions.scope`) and all queries filter by it, so cross-scope access is a
compile-shaped invariant, not a TODO. Private reversal material never leaves provider
state through any public path.

## 15. Observability (§68)

Structured logging (slog) with `actionId` / `attemptId` / provider ref correlation;
no sensitive reversal material in logs; no logging vendor in the protocol.

## 16. Adversarial self-review (§84)

Strongest objections, unhidden:

1. **"You reinvented Saga/LRA."** Rebuttal: no coordinator, no enlistment, no workflow;
   scope is one Action and one provider interaction (`prior-art.md` §1, §5). Residual
   risk: if dependency reasoning (§49) grows, the line blurs — mitigated by keeping
   dependency support to "reject reversal when an active dependent exists" + cycle
   detection.
2. **"The client gains nothing over reading the provider's API docs."** This is the
   strongest objection, and partially true. What docs cannot give: (a) *pre-execution*
   machine-checkable discovery; (b) per-Action eligibility (expiration, state) — docs
   describe the Operation; (c) uniform unknown-outcome/verification semantics. Where
   docs suffice, ROP adds nothing — which is exactly why C3 is flagged weak and the
   recommendation is MODIFY (shrink to C1+C2 as the protocol core; C3 as optional
   capability).
3. **"Impossible guarantees."** None claimed: no exactly-once (§35), no atomicity
   across systems (§57), missing-receipt window explicitly open (§58). The one claim
   under strain is verification ("VM absent") for eventually-consistent providers —
   verification results therefore carry semantics ("eventually consistent", "external
   read-only") and can be UNKNOWN (§47).
4. **"Where can state become ambiguous?"** Exactly at: crash-after-provider-success-
   before-journal-write (both directions), lost provider responses, and concurrent
   attempts under a crashed writer. All map to OUTCOME_UNKNOWN + reconciliation, and
   none are papered over (`failure-model.md`).
5. **"Side effects escaping the journal."** In the local demo, none (single
   transaction). For external providers: inherent; bounded only by intent-journal +
   reconciliation, and §58 forbids claiming more.
6. **"Stale information causing destructive reversal."** Defended twice: execution-time
   precondition re-check (§42) and version CAS (§48); conflict is preferred to rollback.
7. **"HTTP leaking into Core."** Core types carry no status codes/URLs; the binding is
   a separate package. Enforcement: Core packages import nothing HTTP (verified by a
   lint/test in Milestone 1).
8. **"Standardizing provider-specific semantics."** Avoided by construction: the
   protocol carries provider-asserted postconditions and results; it never defines what
   "equivalent" means (§7).
9. **"Could it be dramatically smaller?"** Yes — see the recommendation: drop
   planning-as-required and RESTORABLE-snapshot demonstration from M1; make reverse
   invocation optional capability; keep discovery + receipts + state machine +
   verification as the irreducible core.

## 17. Proposed smallest first vertical slice (§74, revised per approved scope)

```
DISCOVER (GET /.well-known/rop)
  ↓
EXECUTE (POST /resources — demo API, real SQLite persistence, versioned rows)
  ↓
ACTION + RECEIPT (durable Action, immutable public receipt, private material)
  ↓
PLAN (read-only, freshness fields)
  ↓
REVERSE (idempotent, eligibility-checked, CAS-guarded)
  ↓
VERIFY (provider-defined postconditions; separate from execution)
```

Scope discipline for M1: one Operation (`resource.create`, REVERSIBLE), one demo
resource type, REVERSIBLE + IRREVERSIBLE classes exercised, expiration boundary tested,
unknown-JSON-field and golden-fixture tests included. Deferred to later milestones per
§85: RESTORABLE updates + conflicts (M2), idempotency keys + expiration edge tests
already in M1 where cheap, unknown outcome/reconciliation/crash recovery (M4),
dependencies/partial/residue (M5), hardening (M6).

**M2 status (2026-08-30, delivered):** `resource.update` is a genuinely persisted
RESTORABLE Operation (`PATCH /resources/{id}` in the demo). Restoration anchors on CAS
against the version the Action produced, with the captured prior value/version held as
private reversal material; stale plans report staleness via `basisResourceVersion`
conflicts and never authorize execution; the A: v1→v2 / B: v2→v3 / reverse-A conflict
case is demonstrated and tested. Restoration semantics are pinned in
`spec/core.md` §3.1 (including the sequential-undo property).

## 18. Known limitations

- Single-node SQLite; no HA story (correct for a research MVP, documented not excused).
- Verification is only as good as the provider's postconditions; garbage in, garbage out.
- Interop value is contingent on adoption; the protocol cannot be validated by one
  implementation (success criterion §88 is "in principle", and the docs must keep
  saying so).
- The missing-receipt window (§58) is reduced by the intent journal, never closed.
- No cancellation, no batch, no reversal-of-reversal, no crypto (v0.1 non-goals).
- Dependencies are single-level safety gates in v0.1 (no traversal, no cascading
  reversal, no automatic topological compensation — deliberate non-goals confirmed by
  M5: the safety-constraint framing stayed small and interoperable).

## 19. Open questions

Tracked in `open-questions.md` (OQ-1..OQ-10), including: public reconcile surface,
receipt signing, residue taxonomy, verification confidence, side-effect classification,
retry-taxonomy exposure, plan as resource vs response, TTL for capabilities vs actions,
demo-API embedding pattern, and `ROP/` well-known path registration.
