# ROP Adversarial Cases

Status: **approved basis** (architecture review decision, 2026-08-30). Cases A1–A20
apply to the optional reversal-invocation capability and to eligibility/verification;
cases A3, A7, A8, A20 also apply to Core-only providers (eligibility, verification,
and uncertainty semantics exist without any ROP `/reverse` endpoint).
Principle: ROP refuses, delays, reconciles, or exposes uncertainty — it never blindly
reverses. Each case names the expected behavior and the enforcing invariant/test.

> ROP is an experimental protocol / proposal / research project.

| # | Case | Expected behavior | Enforced by |
|---|---|---|---|
| A1 | Resource changed after planning (v7 plan, resource now v8) | reversal-conflict at execution; replanning offered; no restore | I-7, I-19; Test `TestStalePlanConflict` |
| A2 | Refund succeeded but response disappeared (lost response) | OUTCOME_UNKNOWN; attempt AWAITING_RECONCILIATION; never REVERSE_FAILED by timeout | I-5; Test `TestLostResponseIsUnknown` |
| A3 | Reversal expired before request | `reversal-expired` problem; EXPIRED absorbing for new attempts; in-flight attempt may finish | I-8; `TestExactExpirationBoundary`, `TestExpiredNoNewAttempt` |
| A4 | Dependent Action exists (child refund on disputed charge, etc.) | `dependency-exists` problem; reversal rejected, not queued | I-12; `TestDependentBlocksReversal` |
| A5 | Authorization revoked between plan and reverse | authorization-denied at execution; plan was never authority | I-2, I-19 |
| A6 | Reversal snapshot/material missing (expired or purged) | `precondition-failed` / material-missing problem; Action reclassified (EXPIRED/MANUAL); no guessed reversal | failure-model §16; `TestMissingMaterialRejected` |
| A7 | Provider capability disappeared (refund disabled after Action-time metadata said COMPENSATABLE) | Action-time metadata still governs eligibility (I-15); execution surfaces provider refusal; history not rewritten | I-15; `TestCapabilityChangeKeepsActionTimeSemantics` |
| A8 | Verification contradicts execution response (provider said OK, state wrong) | verification wins: REVERSE_FAILED recorded with evidence; execution "success" is not postcondition success | I-10; `TestVerificationFailureDistinctFromInvocation` |
| A9 | Cross-scope Action ID supplied | not-found (no existence leak); zero state change; attempt logged | I-13; `TestCrossScopeForbidden` |
| A10 | Replayed reversal request (same key after conclusion) | returns original recorded outcome; no new execution; different key ⇒ blocked by single-attempt constraint or policy-gated | I-6; `TestDuplicateIdempotentRetry`, failure-model §6 |
| A11 | Duplicate concurrent reverse (two different keys, same instant) | unique non-concluded-attempt index ⇒ one wins, other gets `reversal-already-in-progress` | failure-model §6; `TestConcurrentDuplicateReverse` |
| A12 | Malformed protocol payload (bad JSON, wrong types, null where object required) | RFC 9457 problem; no partial state; store untouched | fixture tests; `TestMalformedPayloads` |
| A13 | Unknown enum values in requests (e.g. unknown reversibility class) | treated as unknown — rejected or surfaced as unknown, never coerced to an existing value | §21; `TestUnknownEnumRejected` |
| A14 | Unknown optional JSON fields in requests | ignored by servers per §21; round-trip fixtures keep them for clients | `TestUnknownFieldsIgnored` |
| A15 | Intent row exists with no receipt (effect-without-journal residue) | recovery surfaces inconsistency (I-16); reconciliation path available; never silently deleted | `TestIntentWithoutReceiptRecovered` |
| A16 | Restart while REVERSING (C5/C6 crash points) | C5 ⇒ re-enter execution with same attempt; C6 ⇒ OUTCOME_UNKNOWN + reconcile; never blanket REVERSE_FAILED | I-11; `TestRestartRecoveryMatrix` |
| A17 | Provider adapter lies about success (T11) | verification may catch if it has independent evidence; residual risk documented, not hidden | security.md T11 |
| A18 | Expiry race: reversal request arrives exactly at expiresAt | `receivedAt >= expiresAt` ⇒ expired (server time, boundary-tested) | §24, §52; boundary test |
| A19 | Dependency cycle submitted (A depends on B, B on A) | cycle detected at write; rejected | I-12; `TestDependencyCycleRejected` |
| A20 | Throttled reversal (429 / provider quota) | attempt stays REVERSING with backoff; throttle never becomes semantic failure | I-20, §55 |

## Executable status (M6)

The adversarial catalog is now executable. Mapping of cases to automated
tests (all passing; see `docs/task-tracker.md` for the full evidence list):

| Cases | Executable evidence |
|---|---|
| A1 (stale plan), CAS conflicts | `TestConflictRefusesWithoutSideEffects`, `TestConflictOverDestructiveRestore`, `TestStalePlanRejection` |
| A2 (lost response => unknown) | `TestLostResponseBecomesUnknown`, `TestReplayOfUnknownOutcomeIsUnknown` |
| A3 (expiry), A18 (boundary race) | `TestSweepExpiryBoundary`, `TestExpiredActionCannotBeginReversal`, `TestInspectionAndVerificationRemainAvailableAfterExpiry` |
| A4 (dependency blocks) | `TestDependentBlocksReversal`, `TestDependencyAfterPlanConflicts`, `TestDependencyStateSurvivesRestart` |
| A5 (authorization revoked) | `TestActionIDIsNotAuthorization`, `TestReconciliationRequiresAuthorization` |
| A9 (cross-scope) | `TestGetActionIsScopeFiltered`, `TestCrossScopeDependencyRejected`, `TestSameKeyDifferentActionRejected` |
| A10/A11 (replay, duplicates) | `TestIdempotentReplaySameKey`, `TestConcurrentSameKeyConverges`, `TestHTTPIdempotentRetryAfterLostResponse`, `TestHTTPIdempotencyConflictOnDifferentAction` |
| A12-A14 (malformed/unknown input) | `FuzzReceiptParse`, `FuzzPlanParse`, `FuzzProblemParse`, `TestUnknownEnumStaysUnknown`, `TestMalformedActionIDsAreSafe` |
| A15 (intent without receipt) | covered structurally by the intent-journal design; crash recovery evidence in the C5/C6 tests (`TestCrashBeforeProviderCall`, `TestCrashAfterProviderSuccess`) |
| A16 (restart while REVERSING) | `TestCrashBeforeProviderCall`, `TestCrashAfterProviderSuccess`, `TestRestartWhileAwaitingReconciliation`, `TestRecoveryNeverConvertsReversingToFailed` |
| A6/A17 (missing material, lying adapter) | design-documented (failure-model 16, security T11); execution-time refusal paths tested via `TestConflictOverDestructiveRestore` |
| A7/A8 (capability change, verification contradicts execution) | `TestCapabilityChangeKeepsActionTimeSemantics`, `TestVerificationFailureDistinctFromInvocation`, `TestConcurrentMutationConflict` |
| A19 (cycles) | `TestDirectCycleRejected`, `TestIndirectCycleRejected`, `TestSelfDependencyRejected` |
| Privacy (I-14) | `TestReceiptsContainNoPrivateMaterial`, `TestPriorStateMaterialRemainsPrivate`, `TestResidueDescriptionsCarryNoBusinessPayloads`, `TestReconciliationReferenceIsNotAnAuthorizationToken` |

## Cross-cutting refusal rules

1. Eligibility is computed at execution time from durable state + server time; no
   client-supplied eligibility is ever trusted (A1, A3, A5).
2. Absence of evidence is never evidence of failure (A2, A16).
3. Existence information is not leaked cross-scope (A9).
4. Conflicts stop the world; residue is reported; history is never rewritten
   (A1, A7, A8).
