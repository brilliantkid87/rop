# ROP Formal Invariants

Status: draft for architecture review (Architecture Gate, Master Prompt v0.3 §65, §86).
Mapping: "Test" entries name the (future) Go tests that will enforce the invariant.
Until those tests exist, the invariant is **unimplemented**, not satisfied.

> ROP is an experimental protocol / proposal / research project.

## Core invariants (§86) with test mapping

### I-1 — Original Action history is never erased by reversal
Actions are insert-only; status changes append to a history record; reversal
has no code path that deletes or rewrites original Action rows.
- Test: `TestReversalPreservesActionHistory` — reverse an Action, assert original row
  unchanged, status history contains APPLIED→...→REVERSED, row count grew.

### I-2 — Action ID is identity, not authority
No endpoint or code path grants any capability based on ID possession alone; every
handler resolves a principal and evaluates `authz` per verb.
- Test: `TestActionIDWithoutAuthorization` — valid actionId with a principal lacking
  `reverse` ⇒ authorization-denied problem; unguessable IDs are irrelevant.

### I-3 — Planning produces no external business side effects
`PlanFunc` receives read-only state; the planner performs no provider mutations; HTTP
plan-reversal handler is GET-safe semantics on POST but provably read-only internally.
- Test: `TestPlanningHasNoSideEffects` — snapshot resource state + provider call
  recorder; plan twice; assert zero mutating calls and identical state.

### I-4 — Reversal request, execution, and verification are distinct
Separate types, states, and endpoints; a `REVERSING` Action is not `REVERSED`;
invocation success is never reported as postcondition success.
- Test: `TestRequestExecutionVerificationDistinct` — reverse accepted (202/200 with
  attempt), assert Action is REVERSING not REVERSED; verification separately reports.

### I-5 — Unknown outcome is not falsely reported as failure
`OUTCOME_UNKNOWN` can only leave via evidence; no timeout, transport error, or crash
recovery path may map unknown → REVERSE_FAILED (§34, §60).
- Test: `TestLostResponseIsUnknown` — provider succeeds, response lost ⇒ state
  OUTCOME_UNKNOWN; `TestRecoveryNeverFailsUnknown` — restart with RUNNING attempt and
  no evidence ⇒ AWAITING_RECONCILIATION, not REVERSE_FAILED.

### I-6 — Protocol idempotency is not exactly-once execution
Idempotency keys dedupe ROP request handling; docs and API text never claim exactly-
once provider behavior; provider adapters own their idempotency.
- Test: `TestDuplicateIdempotentRetry` — same Idempotency-Key twice ⇒ same attempt,
  single execution; docs lint: no "exactly once" strings in spec (manual check).

### I-7 — Unsafe concurrent restoration is rejected
Execution revalidates preconditions and enforces CAS on resource version; mismatch ⇒
CONFLICT, never destructive restore (§41, §48). `CONFLICT` is an attempt-level
outcome, not an Action state: the transition table records
`REVERSING → APPLIED` (no business mutation) and the reversal result reports
`outcome: CONFLICT` with `status: APPLIED` (`spec/core.md` §4, v0.1.2).
- Test: `TestStalePlanConflict` — plan at v7, mutate to v8, reverse with old plan ⇒
  reversal-conflict; resource unchanged.

### I-8 — Expired Actions cannot begin a new reversal
Eligibility check precedes attempt creation; server-time boundary
`receivedAt >= expiresAt` ⇒ expired; in-flight attempt at expiry MAY finish.
- Test: `TestExactExpirationBoundary` — clock at expiresAt-1ns eligible, at expiresAt
  expired; `TestExpiredNoNewAttempt` — reverse on EXPIRED ⇒ reversal-expired problem;
  `TestInFlightCompletesAfterExpiry`.

### I-9 — Provider-defined semantic equivalence is not guessed by clients
Core carries provider-asserted postconditions/results only; no client-side notion of
"equivalent state".
- Test: type-level (no such API exists) + fixture review.

### I-10 — Verification evaluates semantic postconditions independently of invocation success
Verification reads provider state against postconditions; verification-unknown is
representable and distinct from invocation failure (§46, §47).
- Test: `TestVerificationFailureDistinctFromInvocation` — provider call OK, state wrong
  ⇒ REVERSE_FAILED via verification; `TestVerificationUnknown` — verifier transport
  error ⇒ verification unknown, Action stays REVERSING/OUTCOME_UNKNOWN.

### I-11 — Process restart does not erase correctness-critical state
Startup recovery inspects durable evidence; never blanket-maps REVERSING →
REVERSE_FAILED (§60).
- Test: `TestRestartRecoveryMatrix` — for each crash point in `failure-model.md` §9,
  restart and assert correct recovered state.

### I-12 — Dependencies that make reversal unsafe are detected
Active dependent Action ⇒ reversal rejected (`dependency-exists`); dependency cycles
(self, direct, indirect) rejected at write time with a durable UNIQUE backstop (§49).
Dependencies are safety constraints: ROP never executes dependent reversals.
Planning exposes blockers (`Plan.BlockingDependencies`); execution re-checks
independently, so a dependency created after planning still blocks (I-19).
The documented active-dependent rule (OQ-15): REVERSED / PARTIALLY_REVERSED
dependents are resolved and stop blocking; all other statuses block.
- Tests: `TestDependentBlocksReversal`, `TestResolvedDependentUnblocks`,
  `TestDependencyAfterPlanConflicts`, `TestDirectCycleRejected`,
  `TestIndirectCycleRejected`, `TestSelfDependencyRejected`,
  `TestDuplicateDependencyIsIdempotent`, `TestCrossScopeDependencyRejected`,
  `TestDependencyStateSurvivesRestart`.

### I-13 — Unauthorized cross-scope reversal is forbidden
All queries scope-filtered; attempts to address another scope's actionId ⇒ not-found
(not existence leak).
- Test: `TestCrossScopeForbidden` — principal in scope B requests scope A's action ⇒
  action-not-found + no state change.

### I-14 — Private reversal material is not unnecessarily exposed
Receipt/response types structurally cannot carry reversal material; `reversal_material`
never serialized to public paths.
- Test: `TestReceiptsContainNoPrivateMaterial` — marshal all public responses over a
  populated store; assert none of the private keys appear.

### I-15 — Capability changes do not silently rewrite historical Action semantics
Eligibility consults Action-time metadata + current state; current Operation metadata
is advisory only (§53).
- Test: `TestCapabilityChangeKeepsActionTimeSemantics` — operation TTL changes after
  Action creation; Action eligibility unchanged.

### I-16 — Missing receipt after an external side effect is treated as a real consistency problem
Intent-journal rows without receipts are surfaced at recovery as inconsistencies to
reconcile; no code path claims they cannot happen (§58).
- Test: `TestIntentWithoutReceiptRecovered` — seed intent row, no Action; restart ⇒
  recovery marks inconsistency, reconciliation path available.

### I-17 — Transport binding details do not define Core semantics
Core packages (`internal/*` except `httpapi`) import no HTTP packages; protocol JSON
names are the only wire vocabulary.
- Test: `TestCoreImportsNoHTTP` (AST/package-identity check).

### I-18 — Reversal is not cancellation
No reversal path acts on an in-progress Operation; no cancellation surface in v0.1.
- Test: type-level + API surface review.

### I-19 — Stale plans do not bypass execution-time preconditions
All correctness-critical preconditions re-checked at execution regardless of plan
freshness (§40, §42).
- Test: `TestExecutionIgnoresStalePlanTrust` — plan claims preconditions OK; backend
  state changed ⇒ conflict/precondition-failed at execution.

### I-20 — Retry behavior follows failure semantics rather than generic retry loops
Errors carry internal retry taxonomy; policy dispatches on class; 429/throttle is
RETRYABLE and never semantic failure (§37, §55).
- Test: `TestRetryClassification` — table-driven over failure classes;
  `TestThrottleNotSemanticFailure`.

### I-21 — Durable request idempotency (added M3)
A reversal request carrying an `Idempotency-Key` maps durably to exactly one
Reversal Attempt per `(scope, key)`: replays return the recorded result
(never a second execution), concurrent same-key requests converge on one
attempt, the same key in a different scope is independent, and reuse of a key
for a different Action in the same scope is rejected. The invariant is
enforced by a database constraint (`UNIQUE(scope, key_hash)` on
`idempotency_keys`), not only by application checks. Raw keys are not
persisted. ROP request idempotency is not exactly-once execution (I-6).
- Tests: `TestIdempotentReplaySameKey`, `TestConcurrentSameKeyConverges`,
  `TestIdempotencySurvivesRestart`, `TestSameKeyDifferentActionRejected`,
  `TestSameKeyDifferentScopeIsSafe`, `TestReplayOfUnknownOutcomeIsUnknown`,
  `TestIdempotencyUniqueConstraintAtDatabaseLevel`,
  `TestHTTPIdempotentRetryAfterLostResponse`,
  `TestHTTPIdempotencyConflictOnDifferentAction`.

### I-22 — Expiration stops new reversals, not visibility or in-flight work (added M3)
The sweeper only transitions `APPLIED → EXPIRED`; it structurally cannot
corrupt in-flight or concluded attempts (no transition to `EXPIRED` exists
from those states). A reversal accepted before the deadline finishes after
it. Expired Actions remain inspectable; planning and verification remain
available and report honestly.
- Tests: `TestInFlightReversalFinishesAfterExpiry`,
  `TestSweeperPreservesConcludedAttemptsAcrossExpiry`,
  `TestNoExpiryActionRemainsEligible`, `TestExpirationAcrossRestart`,
  `TestPlanBeforeAndAfterExpiry`,
  `TestInspectionAndVerificationRemainAvailableAfterExpiry`.

### I-26 — Residue is not evidence of reversal failure (added M5)
A reversal may satisfy its provider-defined semantic contract while residue
remains; residue records are append-style (DECLARED / DISCOVERED / VERIFIED)
and never overwrite one another. For PARTIALLY_COMPENSATABLE Actions the
outcome is PARTIALLY_REVERSED and remains distinguishable from full reversal;
verification evaluates the partial postcondition without upgrading the
outcome. An ordinary REVERSIBLE/RESTORABLE reversal must not be labeled
partial or acquire residue.
- Tests: `TestPartialCompensableScenario` (includes the partial-verification case),
  `TestResidueVisibleAfterRestart`, `TestNoPartialLabelOnFullReversal`.

## State-transition table (centralized; normative)

| Source | Event | Destination | Durable writes | Retry/notes |
|---|---|---|---|---|
| APPLIED | reversal requested (eligible) | REVERSING | attempt(RUNNING), status-history | guarded by unique non-concluded-attempt index |
| APPLIED | expires (server time) | EXPIRED | status-history | eligibility sweep |
| REVERSING | provider success + verification pass | REVERSED | attempt result, status-history | — |
| REVERSING | provider success + partial residue | PARTIALLY_REVERSED | attempt result, residue record | — |
| REVERSING | provider observed failure | REVERSE_FAILED | attempt result, status-history | NON_RETRYABLE unless classified RETRYABLE |
| REVERSING | provider refuses on correctness-critical precondition (result `outcome: CONFLICT`) | APPLIED | attempt concluded (observed CONFLICT), status-history | no business mutation; conflict over destructive restore (I-7); `CONFLICT` is an attempt-level outcome, NOT an Action state — the Action is back in APPLIED and may be re-requested when eligible |
| REVERSING | response lost / unresolvable | OUTCOME_UNKNOWN | attempt(AWAITING_RECONCILIATION) | reconciliation only; never auto-fail |
| REVERSING | expires mid-flight | REVERSING (unchanged) | — | in-flight attempt MAY finish (§52) |
| OUTCOME_UNKNOWN | reconciliation evidence: succeeded | REVERSED (or PARTIALLY_REVERSED) | attempt result, status-history | — |
| OUTCOME_UNKNOWN | reconciliation evidence: failed | REVERSE_FAILED | attempt result | MANUAL if ambiguous |
| OUTCOME_UNKNOWN | reconciliation evidence: retry-safe | REVERSING | new/updated attempt | RETRYABLE with idempotent key |
| REVERSE_FAILED | new eligible attempt permitted by policy | REVERSING | new attempt | RESERVED, policy-gated, not automatic: NOT a v0.1 transition — the reference implementation leaves REVERSE_FAILED terminal, and any future policy path requires an explicit protocol decision |
| EXPIRED | reversal requested | (rejected) | problem only | I-8 |
| IRREVERSIBLE | reversal requested | (rejected) | problem only | I-8 analogue for class |

Invalid transitions are rejected at the single centralized validator; the table above
is the specification, the Go table is its mirror, and a test asserts they match.

## Non-goals preserved

Nothing in this document defines batch, cancellation, workflow, or crypto semantics;
those remain outside v0.1 (§79, §80).
