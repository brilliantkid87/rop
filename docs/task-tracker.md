# ROP Task Tracker

> ROP is an experimental protocol / proposal / research project. It is not a universal
> rollback standard and cannot undo arbitrary side effects.

## How to use this tracker

Read this file before starting work. Update it whenever meaningful work is started,
progressed, completed, blocked, or changed. Statuses: `TODO`, `IN_PROGRESS`, `BLOCKED`,
`DONE`. Evidence must be concrete (file paths, test names, commands, results). Never
fabricate evidence; if something was not verified, say so.

## Current milestone

- 2026-08-30: **Architecture review decision received: MODIFY APPROVED.** The protocol
  thesis is refined to: *interoperable description, recording, evaluation, and
  verification of the reversibility of side-effecting operations*. ROP Core centers on
  metadata, receipts, eligibility, classes/guarantees, expiration/constraints, residue,
  provider-defined verification, and explicit uncertainty; planning, generic reversal
  invocation, and reconciliation are optional advertised capabilities. See
  `docs/capability-model.md`.
- 2026-08-30: **ARCH-002 and MILESTONE-1 are DONE.** Architecture Gate documents
  revised per the approved scope; Milestone 1 vertical slice implemented, tested
  (`go test ./...` green), validated (`gofmt`, `go vet` clean), and smoke-tested live.
- 2026-08-30: **Milestone 1 review APPROVED.** Proceeding to Milestone 2 only
  (RESTORABLE update, prior-state material, CAS restoration, plan freshness, conflict
  demonstration) per Master Prompt §85. Milestone 3 must not begin.
- 2026-08-30: **MILESTONE-2 is DONE** — implemented, tested (`go test ./...` green),
  validated (`gofmt -l .` empty, `go vet ./...` clean). Restoration semantics pinned in
  `spec/core.md` §3.1.
- 2026-08-30: **Milestone 2 review APPROVED.** Sequential-undo behavior accepted with
  the requirement that it remain documented as sequential reversal, never batch
  rollback. Proceeding to Milestone 3 only (durable reversal-request idempotency +
  expiration hardening). Milestone 4 must not begin.
- 2026-08-30: **MILESTONE-3 is DONE** — durable `Idempotency-Key` support with a
  database-enforced invariant and hardened expiration semantics; full suite green;
  live smoke test of replay + key-conflict recorded.
- 2026-08-30: **Milestone 3 review APPROVED.** Proceeding to Milestone 4 only
  (unknown outcome, retry taxonomy, reconciliation, crash recovery). Milestone 5 must
  not begin.
- 2026-08-30: **MILESTONE-4 is DONE** — unknown outcome, retry taxonomy,
  reconciliation (internal), and crash recovery implemented and tested; full suite
  green; recovery-by-re-execution replaced with uncertainty-preserving parking
  (architecture updated, reason documented).
- 2026-08-30: **Milestone 4 review APPROVED.** Recovery-by-reconciliation accepted
  as the default wherever durable evidence cannot prove the provider call never
  occurred; the four-way separation (HTTP idempotency / provider execution identity /
  reconciliation / verification) approved. Proceeding to Milestone 5 only
  (dependencies, partial compensation, residue). Milestone 6 must not begin.
- 2026-08-30: **MILESTONE-5 is DONE** — dependencies (safety constraints), genuine
  partial compensation, and append-style residue implemented and tested; full suite
  green; active-dependent rule documented (OQ-15).
- 2026-08-30: **Milestone 5 review APPROVED.** Dependency model accepted as a safety
  constraint; PARTIALLY_COMPENSATABLE + residue accepted as real semantics. Proceeding
  to Milestone 6 only (hardening, fixtures, fuzzing, security review, documentation
  consolidation, final v0.1 assessment). No new features.
- 2026-08-30: **MILESTONE-6 is DONE** — hardening, fixture expansion (22 fixtures),
  five fuzz targets (no crashes), security second-pass (one defect found and fixed:
  unbounded problem-detail reflection), terminology/conformance/documentation
  consolidation, and the final v0.1 research assessment
  (`docs/v0.1-assessment.md`: PUBLISH_EXPERIMENTAL_V0_1).
- 2026-08-30: **Milestone 6 review APPROVED; final research-cycle decision:
  PUBLISH_EXPERIMENTAL_V0_1.** Release-preparation pass performed (no new protocol
  semantics): assessment wording separated into pre-v1.0 requirements vs still-open
  research questions; `/.well-known/rop` explicitly documented as experimental and
  unregistered (`spec/http-binding.md` section 10); v0.1 semantics frozen for
  `0.1.0-experimental` (`spec/versioning.md` section 4); repository hygiene files
  added (LICENSE, CONTRIBUTING.md, SECURITY.md, CHANGELOG.md, VERSION); README
  navigation consolidated. **STOP - repository ready to tag `v0.1.0-experimental`
  pending explicit instruction.**
- Active work: none (release preparation complete).

---

## Tasks

### ARCH-001 — Architecture Gate

- **Task ID:** ARCH-001
- **Description:** Produce the seven mandated design documents per Master Prompt §83
  (`docs/architecture.md`, `docs/prior-art.md`, `docs/invariants.md`,
  `docs/failure-model.md`, `docs/security.md`, `docs/adversarial-cases.md`,
  `docs/open-questions.md`), perform the adversarial self-review (§84), and deliver the
  13-point assessment (§90) ending in a GO / MODIFY / ABANDON recommendation. STOP and
  wait for architecture review afterwards.
- **Status:** DONE (pending human architecture review — the review itself is a
  separate decision, not part of this task)
- **Progress:** Complete. All seven mandated documents authored; adversarial
  self-review included in `docs/architecture.md` §16; 13-point assessment with a
  **MODIFY** recommendation delivered in the session of 2026-08-29.
- **Files changed:** `docs/task-tracker.md` (new), `docs/prior-art.md` (new),
  `docs/architecture.md` (new), `docs/invariants.md` (new), `docs/failure-model.md`
  (new), `docs/security.md` (new), `docs/adversarial-cases.md` (new),
  `docs/open-questions.md` (new).
- **Validation:** Not applicable (documentation deliverable; no code exists to compile
  or test). Internal consistency checks performed by cross-referencing: invariants
  I-1..I-20 ↔ adversarial cases A1..A20 ↔ crash matrix C1..C8 ↔ open questions
  OQ-1..OQ-10; document status lines and the experimental-protocol disclaimer appear
  in every deliverable per AGENTS.md documentation rules.
- **Evidence:** The eight files listed above under Files changed, all present in
  `docs/` as of 2026-08-29. Prior-art status claims (MicroProfile LRA 2.0.1 released
  2025-03-11, active; OASIS WS-AT/WS-BA 1.2 final but WS-TX TC dormant) verified via
  web search on 2026-08-29; sources cited in `docs/prior-art.md` §References. Nothing
  has been compiled, executed, or tested — no code exists in the repository.
- **Blockers / open questions:** Human architecture review is the required gate before
  Milestone 1 (Master Prompt §83: "STOP after this assessment"). Reviewer should
  focus on: the MODIFY recommendation (shrink to C1+C2 core, make reversal invocation
  an optional capability), the OQ register, and whether the M1 vertical slice in
  `architecture.md` §17 matches their intent.

---

### ARCH-002 — Revise Architecture Gate documents per approved MODIFY decision

- **Task ID:** ARCH-002
- **Description:** Apply the approved scope revision across the Architecture Gate
  documents: refined thesis, capability model with optional capabilities, minimum
  v0.1 conformance surface, prior-art mechanism-reuse vs interoperability-hypothesis
  distinction, new open questions.
- **Status:** DONE
- **Progress:** Complete.
- **Files changed:** `docs/architecture.md`, `docs/prior-art.md`,
  `docs/open-questions.md`, `docs/capability-model.md` (new),
  `docs/adversarial-cases.md` (cross-reference note).
- **Validation:** Consistency review across the revised documents (manual): thesis
  wording matches the reviewer's approved sentence in `architecture.md` §1 and
  `capability-model.md` §2; optional capabilities listed identically in both; no
  document claims generic reversal invocation is required for conformance.
- **Evidence:** The five files listed above, revised 2026-08-30. No code claims are
  made in these documents.
- **Blockers / open questions:** OQ-11, OQ-12, OQ-13 added (see
  `docs/open-questions.md`).

### MILESTONE-1 — First vertical slice (revised scope)

- **Task ID:** MILESTONE-1
- **Description:** Implement DISCOVER → EXECUTE → ACTION+RECEIPT → PLAN → REVERSE →
  VERIFY for one persisted resource, per the revised architecture: reversal execution
  exists internally as an optional capability the reference implementation exercises;
  conformance never requires it. Includes demo resource API (versioned, persisted),
  Action Receipts, read-only planning, eligibility-gated idempotent-safe reversal with
  CAS conflict handling, provider-defined verification, expiration boundary tests,
  unknown-field/unknown-enum handling, golden v0.1 fixtures, and the
  Core-imports-no-HTTP boundary test. Out of scope (later milestones): idempotency-key
  persistence (M3), OUTCOME_UNKNOWN/reconciliation/crash-recovery (M4),
  dependencies/partial/residue records (M5).
- **Status:** DONE
- **Progress:** Complete. Full vertical slice implemented and tested; live smoke test
  of `ropd` + `ropctl` executed the slice end to end (discover → create → inspect →
  plan → reverse → verify) on 2026-08-30.
- **Files changed:** `go.mod`, `go.sum`, `migrations/0001_init.sql`,
  `pkg/rop/types.go` + `types_test.go`, `internal/action/action.go` +
  `action_test.go`, `internal/clock/clock.go`, `internal/authz/authz.go`,
  `internal/roperr/roperr.go`, `internal/operation/operation.go` +
  `operation_test.go`, `internal/store/store.go`, `actions.go`, `attempts.go` +
  `store_test.go`, `internal/reversal/service.go` + `service_test.go`,
  `internal/planner/service.go` + `service_test.go`,
  `internal/verification/service.go` + `service_test.go`,
  `internal/httpapi/server.go` + `arch_test.go`,
  `internal/testutil/testutil.go`, `examples/resource-api/resourceapi.go` +
  `resourceapi_test.go`, `cmd/ropd/main.go`, `cmd/ropctl/main.go`,
  `spec/core.md`, `spec/http-binding.md`, `spec/versioning.md`,
  `spec/openapi-extension.md`, `spec/fixtures/v0.1/` (7 fixtures),
  `README.md`, `docs/task-tracker.md`.
- **Validation:** `gofmt -w .` → clean (`gofmt -l .` prints nothing);
  `go vet ./...` → clean; `go test ./... -count=1` → all packages pass
  (examples/resource-api, internal/action, internal/httpapi,
  internal/operation, internal/planner, internal/reversal, internal/store,
  internal/verification, pkg/rop). `go build ./...` clean. Live smoke test:
  `go run ./cmd/ropd -addr 127.0.0.1:18099 ...` served discovery + create
  (201 with ROP-Action-ID / ROP-Reversibility headers); `go run ./cmd/ropctl`
  inspect/plan/reverse/verify returned correct receipts, plan with
  `basisResourceVersion`, `outcome: REVERSED`, and `verification: VERIFIED`.
- **Evidence (tests → invariants):** `TestTransitionTableMatchesDocumentedSemantics`
  (§33 table); `TestSuccessfulReversal` + history assertions (I-1);
  `TestActionIDIsNotAuthorization` (I-2, I-13); `TestPlanningHasNoSideEffects`
  (I-3); `TestProviderErrorIsUnknownNotFailed` (I-5, OUTCOME_UNKNOWN);
  `TestConflictRefusesWithoutSideEffects` + `TestConflictOverDestructiveRestore`
  (I-7, CAS conflict over restore); `TestExpiredActionCannotBeginReversal` +
  `TestSweepExpiryBoundary` (I-8, exact `>= expiresAt` boundary at server time);
  `TestVerificationIndependentOfInvocation` + `TestVerificationFailureOfTheCheckIsUnknown`
  (I-10, §47); `TestRecoverOpenAttemptNeverFailsUnknown` (I-11, §60);
  `TestReceiptsContainNoPrivateMaterial` (I-14);
  `TestCoreImportsNoHTTP` (I-17); `TestCapabilityStrippedServer` (capability
  model §3 + OQ-11: 501 capability-unavailable, Core eligibility per OQ-13);
  `TestGoldenFixturesMatchServerShapes` (§22/§64 golden fixtures);
  `TestUnknownEnumStaysUnknown` / `TestReceiptIgnoresUnknownFields` (§21);
  `TestOneNonConcludedAttemptPerAction` (§32/§36 M1 concurrency guard);
  `TestFirstVerticalSlice` (§74 end-to-end).
- **Known gaps (honest):** idempotency-key persistence deferred to M3 (duplicates
  refused via the unique open-attempt index, not deduped); full restart recovery
  classification deferred to M4 (M1 re-executes RUNNING attempts when the provider
  is re-executable, else parks AWAITING_RECONCILIATION); dependencies, partial
  compensation, and residue records deferred to M5; no fuzz/property tests yet (M6).
- **Blockers / open questions:** None. Note: initial `go test` runs exposed two real
  bugs fixed during M1 — nil expiry stored as `''` (empty string) matched the sweep
  comparison and expired no-window Actions; variable-width RFC 3339 storage made
  lexicographic timestamp comparison unsafe at nanosecond edges. Both fixed
  (`store/actions.go`: SQL NULL for no-window, fixed-width `timeFormat`); covered by
  `TestSweepExpiryBoundary`. Also fixed: `MarkAwaitingReconciliation` parameter
  typing; `GetAction` NULL-tolerant expiry scan. Windows TempDir cleanup flake with
  modernc WAL files worked around deterministically in `internal/testutil`.

### MILESTONE-2 — RESTORABLE update, CAS restoration, plan freshness

- **Task ID:** MILESTONE-2
- **Description:** Add a genuinely persisted RESTORABLE update operation; capture the
  minimum private prior-state material; preserve receipt/material separation; CAS
  protection for restoration; plan freshness on resource version; stale plans never
  authorize unsafe restoration; execution revalidates preconditions; demonstrate the
  A: v1→v2, B: v2→v3, reverse-A-must-conflict case; provider-defined verification of
  restoration; original Action history preserved; restart preserves restoration
  material. No M3+ scope.
- **Status:** DONE
- **Progress:** Complete. RESTORABLE update implemented end to end with CAS-anchored
  restoration, plan freshness, the mandated conflict demonstration, provider-defined
  restoration verification, privacy of prior-state material, and restart persistence.
- **Files changed:** `examples/resource-api/resourceapi.go` (resource.update Operation:
  plan/reverse/verify funcs, Update method, PATCH /resources/{id} handler),
  `examples/resource-api/m2_test.go` (new, 7 tests), `examples/resource-api/resourceapi_test.go`
  (harness: dbPath + restart support, UpdateTTL), `cmd/ropd/main.go` (-update-ttl),
  `spec/core.md` (§3.1 Restoration semantics), `docs/architecture.md` (§17 M2 status),
  `README.md` (PATCH demo line), `docs/task-tracker.md`.
- **Validation:** `gofmt -l .` → empty; `go vet ./...` → clean; `go build ./...` →
  clean; `go test ./... -count=1` → all packages pass (2026-08-30), including
  examples/resource-api with the 7 new M2 tests plus the 8 M1 tests.
- **Evidence (tests → requirements):**
  `TestSuccessfulRestorableUpdate` (RESTORABLE op persisted; receipt exposes class,
  status, window — never prior state); `TestRestoreSucceedsAtExpectedVersion` (restore
  when CAS version matches: prior value AND prior version restored; history
  APPLIED→REVERSING→REVERSED intact = I-1; verification VERIFIED with all
  postconditions satisfied); `TestStalePlanRejection` (plan basis v2; B mutates to v3;
  reversal conflicts regardless of plan = §40/§41/I-19; resource untouched at v3);
  `TestConflictScenarioAthenB` (mandated case: A: v1→v2, B: v2→v3, reverse A ⇒ CONFLICT
  with "concurrent mutation" diagnosis, NOT v1; sequential undo B then A both succeed
  and are verified); `TestConcurrentMutationConflict` (publish between update and
  restore ⇒ CONFLICT; verification honestly FAILED); `TestPriorStateMaterialRemainsPrivate`
  (previousValue/previousVersion/expectedVersion absent from status, plan, and
  verification responses = I-14; present in reversal_material table);
  `TestRestartPreservesRestorationMaterial` (close + reopen DB, rebuild services:
  receipt intact, restore succeeds, I-11 for M2 scope).
- **Semantic decisions recorded (docs updated):** restoration restores the captured
  prior value AND prior version (provider-defined, pinned in `spec/core.md` §3.1);
  restoration is CAS-anchored on the version the Action produced; a successful
  restoration may re-enable earlier reversals whose CAS anchors match the restored
  version (sequential undo — sound because each restore is version-anchored and
  verified; not batch rollback).
- **Blockers / open questions:** None. M3 (idempotency keys, duplicate requests) not
  started, per instruction.

### MILESTONE-3 — Durable reversal idempotency + expiration hardening

- **Task ID:** MILESTONE-3
- **Description:** `Idempotency-Key` support on reversal requests with durable state
  (survives restart), scoped so keys cannot collide across actions/scopes, defined
  behavior for reuse with materially different request semantics, concurrent-key
  convergence, database constraints enforcing the idempotency invariant. Expiration
  hardening: no-expiry actions, expiry across restart, planning before/after expiry,
  exact boundary, pre-expiry accepted reversals finish after expiry, concluded attempts
  untouched by the sweeper, inspect/verify availability after expiry. No M4 scope.
- **Status:** DONE
- **Progress:** Complete. Durable reversal-request idempotency implemented with a
  database-enforced invariant; expiration semantics hardened and tested across the
  full required matrix.
- **Files changed:** `migrations/0002_idempotency_keys.sql` (new: `idempotency_keys`
  with `UNIQUE(scope, key_hash)`, FK constraints, CHECK length=64 on hashes),
  `internal/store/idempotency.go` (new: Get/GetByAttempt/Create with
  `ErrIdempotencyKeyExists`), `internal/store/attempts.go` (tightened
  `isUniqueConstraintErr` to UNIQUE-only so FK/CHECK failures are not misread as
  replay races), `pkg/rop/types.go` (`ProblemIdempotencyConflict`),
  `internal/reversal/service.go` (key hashing/fingerprinting, durable replay,
  concurrent convergence via `replayOrConflict`, result reconstruction from durable
  attempt state), `internal/httpapi/server.go` (Idempotency-Key header, 409 mapping,
  title), `internal/reversal/m3_test.go` (new, 12 tests), `examples/resource-api/m3_test.go`
  (new, 3 tests), `spec/core.md` (§10 idempotency, §11 expiration semantics, problem
  list), `spec/http-binding.md` (Idempotency-Key semantics, 409), `docs/architecture.md`
  (§10 idempotency implemented; expiration-hardening note), `docs/invariants.md`
  (I-21, I-22 with test mapping), `docs/failure-model.md` (§4, §5 updated for durable
  dedupe), `docs/task-tracker.md`.
- **Validation:** `gofmt -l .` → empty; `go vet ./...` → clean; `go test ./... -count=1`
  → all packages pass (2026-08-30), including 12 new service-level tests and 3 new
  HTTP-level tests. Live smoke test (ropd on 127.0.0.1:18101): reversal with
  `Idempotency-Key: smoke-key-1` returned `outcome: REVERSED` with attemptId
  `ra_fdbd5...`; the retry with the same key returned the byte-identical recorded
  result (same attemptId and observedAt — reconstructed, not re-executed); the same
  key aimed at a different action returned `409 urn:rop:problem:idempotency-key-conflict`.
- **Evidence (tests → requirements):**
  Idempotency: `TestIdempotentReplaySameKey` + `TestHTTPIdempotentRetryAfterLostResponse`
  (sequential duplicate / lost-response retry: same attemptId, 1 execution, 1
  REVERSING history row); `TestConcurrentSameKeyConverges` (concurrent same key → one
  attempt, exactly 1 execution); `TestIdempotencySurvivesRestart` (close + reopen:
  replay returns recorded result, no re-execution); `TestSameKeyDifferentActionRejected`
  + `TestHTTPIdempotencyConflictOnDifferentAction` (materially different semantics →
  409 idempotency-key-conflict, no execution); `TestSameKeyDifferentScopeIsSafe` (same
  textual key in another scope is independent); `TestReplayOfUnknownOutcomeIsUnknown`
  (parked attempt replays as OUTCOME_UNKNOWN, never re-executes — I-5);
  `TestIdempotencyUniqueConstraintAtDatabaseLevel` (DB constraint, not app-only).
  Expiration: `TestSweepExpiryBoundary` (exact `>= expiresAt` boundary, M1);
  `TestNoExpiryActionRemainsEligible` (NULL window never expires); `TestExpirationAcrossRestart`;
  `TestPlanBeforeAndAfterExpiry` (plan honest before/after, conflict entries after);
  `TestInFlightReversalFinishesAfterExpiry` (accepted pre-expiry, clock moves past
  deadline mid-flight: finishes REVERSED, sweeper leaves it intact, no EXPIRED history);
  `TestSweeperPreservesConcludedAttemptsAcrossExpiry` (concluded CONFLICT attempt +
  idempotency record intact after expiry sweep);
  `TestInspectionAndVerificationRemainAvailableAfterExpiry` (status EXPIRED inspectable,
  reverse → 410, plan available with conflicts, verification honestly FAILED).
- **Design notes:** the preferred model `scope + actionId + idempotencyKey -> attempt`
  is represented by the record; the `UNIQUE(scope, key_hash)` index additionally
  forbids one key spanning two actions within a scope, which is exactly the
  "materially different request semantics" rejection (fingerprint = SHA-256 of
  scope/actionId/verb, checked before replay). Raw keys are never stored. Provider
  idempotency remains separate (adapters derive their own reference from attemptId).
  No exactly-once claim anywhere.
- **Blockers / open questions:** None. M4 (unknown outcome recovery, retry taxonomy,
  reconciliation, crash recovery) not started, per instruction.

### MILESTONE-4 — Unknown outcome, retry taxonomy, reconciliation, crash recovery

- **Task ID:** MILESTONE-4
- **Description:** Prove honest behavior under unobservable provider outcomes
  ("failure to observe success is not proof of failure"): provider execution identity
  per attempt, internal retry taxonomy (RETRYABLE / NON_RETRYABLE / RECONCILE_REQUIRED /
  MANUAL_INTERVENTION_REQUIRED) recorded durably, first-class internal reconciliation
  (evidence-gated, idempotent, observations durable, negative lookups trusted only when
  the provider contract proves them), crash-point recovery that preserves uncertainty
  (park, never conclude as failed), verification/reconciliation kept distinct, durable
  uncertainty history. No public /reconcile endpoint. No M5 scope.
- **Status:** DONE
- **Progress:** Complete. Provider execution identity, internal retry taxonomy,
  evidence-gated internal reconciliation, and uncertainty-preserving crash recovery
  implemented and tested across the full required matrix.
- **Files changed:** `migrations/0003_reconciliation.sql` (new: `retry_class` column,
  `reconciliation_observations` append-only table), `internal/operation/operation.go`
  (`RetryClass`, `ProviderFailure`, `ReconcileFunc`, `ProviderRef` in `ReverseInput`),
  `internal/store/attempts.go` (attempt identity/class, `GetLatestAttempt`,
  `GetRunningAttempts`), `internal/store/actions.go` (`GetActionByID`, recovery-only),
  `internal/store/reconciliation.go` (new: observations),
  `internal/reversal/service.go` (pre-assigned execution identity `rop-rev-<attemptId>`;
  taxonomy-driven `execute`; `RecoverAll` parks uncertain attempts; exported
  `ReconstructResult`), `internal/reconciliation/service.go` (new service),
  `cmd/ropd/main.go` (startup recovery), `internal/reversal/m4_test.go` (new, 10
  tests), `internal/reconciliation/service_test.go` (new, 8 tests),
  `internal/reversal/service_test.go` (M1 recovery test retired with pointer to M4
  semantics), `spec/core.md` (§12 reconciliation + identity; invariants renumbered),
  `spec/http-binding.md` (unknown-outcome client guidance), `docs/architecture.md`
  (§6, §9, §10 updated), `docs/invariants.md` (I-11 rewritten, I-23..I-25 added),
  `docs/failure-model.md` (§1, §4, crash matrix C5/C6), `docs/security.md` (T17),
  `docs/open-questions.md` (OQ-1 M4 update; OQ-14 added), `docs/task-tracker.md`.
- **Validation:** `gofmt -l .` → empty; `go vet ./...` → clean; `go test ./... -count=1`
  → all packages pass (2026-08-30), including 10 new reversal tests and 8 new
  reconciliation tests.
- **Evidence (tests → requirements):**
  Unknown outcome: `TestDefiniteProviderFailureIsNotUnknown` (case 2/definite failure ⇒
  REVERSE_FAILED, never unknown); `TestLostResponseBecomesUnknown` (case 3: provider
  executed + response lost ⇒ OUTCOME_UNKNOWN + AWAITING_RECONCILIATION +
  RECONCILE_REQUIRED recorded; side effect confirmed in provider world);
  `TestIrreduciblyAmbiguousBecomesUnknownWithManualClass` (case 7 ⇒ MANUAL class
  preserved); `TestUnknownOutcomeIsNotAutomaticallyRetried` (no execution without
  explicit reconcile; recovery re-executes nothing); `TestPreExecutionFailureIsRetriable`
  (case 1: RETRIABLE ⇒ Action APPLIED again, explicit new request permitted, 2 total
  executions); `TestDefiniteRejectionIsNotBlindlyRetried` (terminal, 1 execution).
  Reconciliation: `TestReconciliationProvesSuccess` (case 5: OUTCOME_UNKNOWN → REVERSED
  on proven evidence); `TestReconciliationProvesSafeFailure` (case 6: proven
  non-execution ⇒ REVERSE_FAILED); `TestNegativeLookupWithoutProofStaysUnknown`
  (unproven "not found" stays unknown); `TestFailedLookupIsInconclusive`;
  `TestRepeatedReconciliationIsIdempotent` (2 inconclusive rounds + 1 proven conclusion
  + idempotent replay; side effect ran exactly once); 
  `TestVerificationAndReconciliationRemainDistinct` (verification VERIFIED while
  unknown; attempt not concluded by verification — policy per OQ-14);
  `TestDurableHistoryPreservesUncertaintySequence` (identity + error + class + status
  history APPLIED→REVERSING→OUTCOME_UNKNOWN→REVERSED + observation + verification row).
  Crash recovery: `TestCrashBeforeProviderCall` (C5: parked, reconcilable);
  `TestCrashAfterProviderSuccess` (C6: identity survived, points at provider effect);
  `TestCrashAfterResultPersistenceBeforeResponse` (C4-reversal: replay same key, 1
  execution); `TestRestartWhileAwaitingReconciliation`; `TestRecoveryNeverConvertsReversingToFailed`;
  `TestProviderExecutionIdentityIsStable`.
- **Architecture change (documented, not layered):** M1's recovery-by-re-execution was
  replaced in M4 — no durable marker can distinguish crash-before-provider-call from
  crash-after-provider-success, so recovery parks uncertain attempts
  (AWAITING_RECONCILIATION / OUTCOME_UNKNOWN) and reconciliation resolves them via
  read-only lookups on the durable execution identity. Recorded in `architecture.md`
  §9, `invariants.md` I-11, `failure-model.md` C5/C6, and `service_test.go`.
- **Scope notes:** reconciliation remains internal (no `/reconcile` endpoint, no CLI
  verb — OQ-1 decision held); no retry engine, no scheduler, no M5 features.
- **Blockers / open questions:** None. M5 (dependencies, partial compensation, residue)
  not started, per instruction.

### MILESTONE-5 — Dependencies, partial compensation, and residue

- **Task ID:** MILESTONE-5
- **Description:** Durable scope-safe Action-to-Action dependencies as safety
  constraints (documented active-dependent rule, cycle/duplicate/cross-scope
  rejection, plan exposure + execution re-check); a genuine persisted
  PARTIALLY_COMPENSATABLE scenario (two real effects, one compensable, one not);
  append-style residue (DECLARED/DISCOVERED/VERIFIED) with lifecycle fields; partial
  verification distinct from full reversal. No workflow orchestration, no traversal,
  no cascade.
- **Status:** DONE
- **Progress:** Complete. Implemented and tested at the domain layer
  (internal/dependency) and end-to-end (demo resource API); full suite green.
- **Files changed:** `migrations/0004_dependencies_residue.sql` (new:
  `action_dependencies` with UNIQUE(parent, dependent), `action_residue` append-only
  table with CHECK on source, `notifications` demo table), `pkg/rop/types.go`
  (`Residue` lifecycle fields expected/providerDefined/manualRemediable,
  `ProblemDependencyExists`, `Plan.BlockingDependencies`),
  `internal/dependency/service.go` + `service_test.go` (new),
  `internal/store/residue.go` (new), `internal/reversal/service.go` (execution-time
  dependency re-check; residue recording on PARTIALLY_REVERSED and REVERSED
  outcomes), `internal/planner/service.go` (blocking dependencies + declared residue
  in plans), `internal/httpapi/server.go` (receipt residue aggregation, 409 mapping),
  `examples/resource-api/resourceapi.go` (`resource.notify` PARTIALLY_COMPENSATABLE
  scenario with real mutable + immutable effects; update→create dependency edges
  recorded in-transaction; POST /resources/{id}/notify),
  `examples/resource-api/m5_test.go` (new, 8 tests),
  `examples/resource-api/resourceapi_test.go` (harness dependency wiring + restart
  rebuild), `cmd/ropd/main.go` (-notify-ttl, dependency service wiring), spec/core.md
  (§12.1–12.3), spec/http-binding.md, docs/architecture.md (§6a, limitations),
  docs/invariants.md (I-12 rewritten, I-26 added), docs/failure-model.md (§18a),
  docs/security.md (T13 implemented), docs/open-questions.md (OQ-15 added),
  docs/task-tracker.md.
- **Validation:** `gofmt -l .` → empty; `go vet ./...` → clean; `go test ./... -count=1`
  → all 11 packages pass (2026-08-30), including 6 new domain tests
  (internal/dependency) and 8 new e2e tests (examples/resource-api).
- **Evidence (tests → requirements):**
  Dependencies: `TestDependentBlocksReversal` (B depends on A ⇒ reverse A → 409
  dependency-exists, resource untouched); `TestResolvedDependentUnblocks` (reverse B ⇒
  REVERSED ⇒ A unblocked and reverses); `TestDependencyAfterPlanConflicts` (plan
  before edge; edge created after planning still blocks at execution; refreshed plan
  exposes `blockingDependencies`); `TestDirectCycleRejected` /
  `TestIndirectCycleRejected` / `TestSelfDependencyRejected` (invalid graph writes
  rejected at the domain layer, durable UNIQUE backstop);
  `TestDuplicateDependencyIsIdempotent` (one row, no error);
  `TestCrossScopeDependencyRejected` (scope-local edges, I-13);
  `TestDependencyStateSurvivesRestart` (edges durable and still blocking);
  `TestActiveDependentRule` (documented rule pinned across all eight statuses).
  Partial compensation: `TestPartialCompensableScenario` (real two-effect scenario —
  notifications row SENT then WITHDRAWN while the immutable delivery record remains;
  receipt PARTIALLY_COMPENSATABLE/BEST_EFFORT; outcome PARTIALLY_REVERSED; DECLARED +
  DISCOVERED residue rows; partial verification VERIFIED without upgrading to
  REVERSED; history APPLIED→REVERSING→PARTIALLY_REVERSED);
  `TestNoPartialLabelOnFullReversal` (REVERSIBLE reversal carries no residue and no
  partial label — no enum faking). Residue: `TestResidueVisibleAfterRestart` (both
  lifecycle stages visible after restart; verification evidence intact); declared
  residue exposed in planning (plan assertions in `TestPartialCompensableScenario`).
- **Documented decisions:** active-dependent rule (OQ-15: resolved =
  {REVERSED, PARTIALLY_REVERSED}; a documented decision, not an accidental hard-code);
  dependencies are safety constraints, not workflow ownership (spec/core.md §12.1);
  residue is not evidence that reversal failed, but PARTIALLY_REVERSED stays
  distinguishable from REVERSED (spec/core.md §12.3).
- **Blockers / open questions:** None. M6 (hardening, fixture expansion, fuzzing,
  security review, documentation pass) not started, per instruction.

### MILESTONE-6 — Hardening, fixtures, fuzzing, security review, documentation

- **Task ID:** MILESTONE-6
- **Description:** Specification consistency audit; capability-based conformance
  definition; fixture expansion; forward-compatibility tests; focused fuzzing;
  second-pass security review; adversarial-case executable mapping; HTTP status
  audit; terminology audit; open-questions resolution pass; independent-implementer
  assessment; README consolidation; final v0.1 research assessment. No new features.
- **Status:** DONE
- **Progress:** Complete. All twelve work items delivered; no new capabilities added.
- **Files changed:** `spec/fixtures/v0.1/` (15 new fixtures, 22 total),
  `pkg/rop/fixtures_test.go` + `fuzz_test.go` (new),
  `internal/action/fuzz_test.go` (new), `internal/dependency/fuzz_test.go` (new),
  `internal/reversal/fuzz_test.go` (new),
  `examples/resource-api/m6_test.go` (new security tests),
  `internal/httpapi/server.go` (`sanitizeDetail` defect fix),
  `spec/interoperability.md` (new), `spec/core.md` (terminology section,
  capabilities update), `spec/http-binding.md` (status audit table §8),
  `docs/capability-model.md` (conformance consolidation incl. dependencies/residue),
  `docs/adversarial-cases.md` (executable mapping), `docs/security.md` (M6 second
  pass), `docs/open-questions.md` (status labels + resolution summary),
  `README.md` (consolidated), `docs/v0.1-assessment.md` (new), `docs/task-tracker.md`.
- **Validation:** `gofmt -l .` empty; `go vet ./...` clean; `go test ./... -count=1`
  all 11 packages pass. Fuzz runs (bounded local, crash-free): `FuzzReceiptParse`
  10s / ~2.2M execs; `FuzzPlanParse` 8s; `FuzzProblemParse` 8s;
  `FuzzStateTransitions` 8s; `FuzzIdempotencyKeySemantics` 8s;
  `FuzzDependencyGraph` 15s. No crashes found. Not an exhaustive fuzz claim.
- **Evidence:** Fixture/vocabulary drift checks (`TestAllFixturesParseAndUseStableVocabulary`,
  `TestFixtureShapesMatchWireTypes`, unknown-enum and unknown-field compatibility
  tests, missing-required-field detectability); security second pass
  (`TestMalformedActionIDsAreSafe`, `TestResidueDescriptionsCarryNoBusinessPayloads`,
  `TestReconciliationReferenceIsNotAnAuthorizationToken`) — found and fixed the
  detail-reflection defect; adversarial-case mapping table in
  `docs/adversarial-cases.md`; OQ-1..15 labeled (10 RESOLVED_FOR_V0_1, 3 DEFERRED,
  2 STILL_OPEN); interoperability gaps listed in `spec/interoperability.md`.
- **Blockers / open questions:** OQ-6 and OQ-10 remain open and are called out in
  the final assessment as pre-v1.0 requirements, not v0.1 blockers.

### RELEASE-PREP — Release preparation for `0.1.0-experimental`

- **Task ID:** RELEASE-PREP
- **Description:** Resolve assessment wording (pre-v1.0 requirements vs still-open
  research); Well-Known URI status audit; freeze declaration; repository hygiene
  files; version marker; release notes; public-spec navigation; final validation.
- **Status:** DONE
- **Progress:** Complete. No new protocol semantics added; no release-blocking
  contradictions found.
- **Files changed:** `docs/v0.1-assessment.md` (recommendation section split into
  "Required before any future v1.0 consideration" and "Still-open research
  questions"), `spec/http-binding.md` (new section: Well-Known URI
  experimental/unregistered status), `spec/versioning.md` (new section: freeze
  declaration + protocol-version vs release-version distinction), `VERSION` (new:
  0.1.0-experimental), `LICENSE` (new, MIT), `CONTRIBUTING.md` (new),
  `SECURITY.md` (new: implementation vulnerabilities vs documented protocol
  limitations), `CHANGELOG.md` (new: release notes), `README.md` (version marker,
  URI status, specification navigation section, layout update),
  `docs/task-tracker.md`.
- **Validation:** `gofmt -l .` empty; `go vet ./...` clean; `go test ./... -count=1`
  all 11 packages pass (2026-08-30). Fuzz campaigns not rerun: no fuzzed behavior
  (parsing, transitions, graph, fingerprinting) was changed by this pass —
  documentation and metadata files only.
- **Evidence:** Files above present as of 2026-08-30; audit of every
  `/.well-known/rop` reference confirmed none implies IANA registration (explicit
  status note added in `spec/http-binding.md`; README links it); single
  authoritative release version in `VERSION` (0.1.0-experimental),
  cross-referenced by `CHANGELOG.md` and `README.md`, distinct from the wire
  protocol version `0.1` (per `spec/versioning.md`).
- **Blockers / open questions:** None. Tagging, GitHub release, IANA request,
  and external publication await explicit instruction.

### RELEASE-FINALIZE — Repository release finalization

- **Task ID:** RELEASE-FINALIZE
- **Description:** Git initialization; canonical module path migration to
  `github.com/brilliantkid87/rop`; dependency tidy; ignore review; clean release
  commit; annotated tag `v0.1.0-experimental`; verification.
- **Status:** DONE
- **Progress:** Complete. No protocol semantics modified.
- **Files changed:** `go.mod` (module path), all 28 `.go` files importing the old
  path, `.gitignore` (new), `docs/task-tracker.md` (this entry). Removed: two stray
  zero-byte files (`ROP`, `rollback`) created accidentally during an earlier session
  step.
- **Validation:** `gofmt -l .` empty; `go vet ./...` clean; `go test ./... -count=1`
  all 11 packages pass (2026-08-30, on the new module path). `go mod tidy` produced
  no dependency changes (same modernc.org/sqlite dependency set, go 1.25.0).
- **Evidence:** `grep -rn "github.com/rop/rop"` over `*.go`/`*.mod` returns 0
  matches after migration; `go.mod` declares `module github.com/brilliantkid87/rop`;
  release commit hash and tag verification recorded in the follow-up docs commit
  (hash unknowable inside its own commit).
- **Blockers / open questions:** None. No push, no GitHub release, no IANA request.

## Artifacts

| Artifact | Status | Evidence |
|---|---|---|
| `docs/task-tracker.md` | Created | This file |
| `docs/prior-art.md` | DONE | File present; LRA/WS-BA status verified via web search 2026-08-29 |
| `docs/architecture.md` | DONE | File present; includes §16 adversarial self-review, §17 M1 slice |
| `docs/invariants.md` | DONE | File present; I-1..I-20 with test mapping + transition table |
| `docs/failure-model.md` | DONE | File present; failures 1–19 + crash matrix C1..C8 |
| `docs/security.md` | DONE | File present; threats T1..T16, explicit non-claims |
| `docs/adversarial-cases.md` | DONE | File present; cases A1..A20 |
| `docs/open-questions.md` | DONE | File present; OQ-1..OQ-13 (OQ-11..13 added by ARCH-002) |
| 13-point GO/MODIFY/ABANDON assessment | DONE | **MODIFY** recommendation, delivered in session response 2026-08-29 |
| `docs/capability-model.md` | DONE | Capability model + v0.1 conformance surface (ARCH-002) |
| `spec/core.md`, `spec/http-binding.md`, `spec/versioning.md`, `spec/openapi-extension.md` | DONE (draft) | Written 2026-08-30, consistent with capability-model |
| `spec/fixtures/v0.1/` (7 fixtures) | DONE | Golden Level-1 wire fixtures; used by `TestGoldenFixturesMatchServerShapes` |
| M1 implementation (cmd/, internal/, pkg/, examples/, migrations/) | DONE | `go build ./...`, `go vet ./...`, `gofmt -l .` clean; `go test ./... -count=1` all pass; live ropd+ropctl smoke test 2026-08-30 |
| M2 implementation (resource.update RESTORABLE + tests + spec §3.1) | DONE | 7 new tests in `examples/resource-api/m2_test.go`; full suite green; `gofmt -l .` empty; `go vet ./...` clean (2026-08-30) |
| M3 implementation (durable idempotency + expiration hardening) | DONE | `migrations/0002_idempotency_keys.sql`; 15 new tests (12 service + 3 HTTP); full suite green; live replay/conflict smoke test 2026-08-30 |
| M4 implementation (unknown outcome, taxonomy, reconciliation, crash recovery) | DONE | `migrations/0003_reconciliation.sql`; 18 new tests (10 reversal + 8 reconciliation); full suite green; recovery semantics revised and documented 2026-08-30 |
| M5 implementation (dependencies, partial compensation, residue) | DONE | `migrations/0004_dependencies_residue.sql`; 14 new tests (6 dependency domain + 8 e2e); full suite green 2026-08-30 |
| M6 implementation (hardening, fixtures, fuzzing, security review, docs) | DONE | 22 fixtures; 5 fuzz targets crash-free (57s total); 1 security defect found+fixed; final assessment `docs/v0.1-assessment.md` 2026-08-30 |
| RELEASE-PREP (release preparation for `0.1.0-experimental`) | DONE | Hygiene files (VERSION, LICENSE, CONTRIBUTING.md, SECURITY.md, CHANGELOG.md); freeze + URI-status + assessment-wording doc updates; full validation green 2026-08-30 |
| `README.md` | DONE | Written 2026-08-30 |

## Decisions log

| Date | Decision | Rationale |
|---|---|---|
| 2026-08-29 | Create task tracker as first artifact | `AGENTS.md` mandates `/docs/task-tracker.md` as required reading; it did not exist |
| 2026-08-29 | Architecture Gate before any Go code | Master Prompt §83, §90 mandate design-first and a review stop |
| 2026-08-30 | **MODIFY APPROVED** by architecture review: refined thesis = interoperable description, recording, evaluation, and verification of reversibility; planning / generic reversal invocation / reconciliation are optional advertised capabilities, never conformance requirements | Reviewer decision of 2026-08-30; applied via ARCH-002 |
| 2026-08-30 | Established distributed-systems mechanisms (OUTCOME_UNKNOWN, idempotency, reconciliation, compensation, OCC) are documented as reused prior art, not ROP innovations | Reviewer decision of 2026-08-30; enforced in `docs/prior-art.md` findings |
| 2026-08-30 | Milestone 1 uses SQLite via `modernc.org/sqlite` (pure Go, no CGO) | Go 1.25.0 on Windows (corrected from earlier note); avoids CGO toolchain dependency; Master Prompt §72 requires SQLite |
| 2026-08-30 | DB timestamps stored in fixed-width RFC 3339 (`store.timeFormat`) | Lexicographic comparison must equal chronological comparison for the SQL expiry sweep; boundary correctness tested |
| 2026-08-30 | No-window Actions store SQL NULL `expires_at` | `''` (empty string) matched `expires_at <= ?` and wrongly expired window-less Actions; bug found by `TestSweepExpiryBoundary` |
| 2026-08-30 | `OUTCOME_UNKNOWN` reverse requests report `reversal-already-in-progress` | An unknown outcome keeps the attempt open; the open-attempt constraint is the authority (invariant I-5) |

## Open questions

See `docs/open-questions.md` (created as part of ARCH-001).
