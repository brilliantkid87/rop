# ROP Failure Model

Status: draft for architecture review (Architecture Gate, Master Prompt v0.3 §78).
Companions: `architecture.md` (crash consistency), `security.md`, `invariants.md`.

> ROP is an experimental protocol / proposal / research project. Unknown outcomes are
> unknown, not failed. ROP does not claim exactly-once execution or universal recovery.

For each failure: durable state, possible external state, safe retry?, outcome known?,
reconciliation/verification path, manual intervention.

Failure taxonomy key — internal retry classes: RETRYABLE (R), NON_RETRYABLE (N),
RECONCILE_REQUIRED (RC), MANUAL_INTERVENTION_REQUIRED (M).

---

## 1. Process crash

See `architecture.md` §9 and the crash-point matrix in §15 below. Summary: the intent
journal + attempt-records bound every crash to a state recovery can classify into
{nothing happened, attempt in flight ⇒ parked for reconciliation, evidence exists ⇒
conclude}. Recovery (M4) parks RUNNING attempts as AWAITING_RECONCILIATION — no durable
marker distinguishes crash-before-call from crash-after-success, so uncertainty is
preserved rather than guessed. No crash converts unknown into failure.

## 2. Database failure (SQLite unavailable/corrupt mid-operation)

- Durable: whatever was committed before failure.
- External: may exist (if crash during external call).
- Retry: R — retry the request after DB recovery; idempotency keys dedupe.
- Known?: depends on committed evidence; recovery re-derives from DB.
- SQLite corrupt beyond recovery ⇒ data loss is real; audit history lost is a
  documented limitation, not an excuse to weaken durability settings (WAL,
  synchronous=FULL for the MVP).

## 3. Network timeout (ROP → provider), request not transmitted

- Durable: attempt RUNNING recorded before the call (transaction A committed).
- External: none (transmission never started — where observable; if unobservable,
  treat as §4).
- Retry: R. Outcome known: no effect happened ⇒ safe to retry with same idempotent
  provider reference.

## 4. Lost response / provider timeout after request transmission

- Durable: attempt RUNNING, no result.
- External: **the effect may have happened.**
- Retry: NOT blind-R. Class RC: safe retry only via idempotent provider reference;
  first action is reconciliation — a read-only provider lookup on the attempt's
  durable execution identity (`rop-rev-<attemptId>`, M4), which never re-invokes the
  side effect. At the ROP request layer, a client retry with the same `Idempotency-Key`
  replays the recorded `OUTCOME_UNKNOWN` instead of re-executing (M3, I-21).
- Known?: no. State: OUTCOME_UNKNOWN (attempt AWAITING_RECONCILIATION, class
  RECONCILE_REQUIRED). This is the canonical §34 case. Reconciliation concludes the
  attempt only on provider evidence the adapter contract marks proven; a negative
  lookup without that contract stays inconclusive.

## 5. Duplicate request (client retries with same Idempotency-Key)

- Durable: original attempt + `idempotency_keys` row (key stored as SHA-256
  hash), unique per `(scope, key_hash)` — implemented in M3 (migration 0002).
- External: at most the original effect.
- Retry: handled by durable dedupe — return the recorded result reconstructed
  from attempt + action state; no new execution (I-6, I-21). A retry after a
  lost HTTP response returns the existing result. A replay of an unobserved
  outcome returns `OUTCOME_UNKNOWN` (I-5), never re-executes.
- Reuse of the same key for a different Action in the same scope: rejected
  (`idempotency-key-conflict`), never executed.
- Known?: whatever the original attempt's outcome was.

## 6. Concurrent duplicate reverse (different keys, same Action)

- Durable: unique partial index admits one non-concluded attempt; loser gets
  `reversal-already-in-progress` problem type.
- Retry: loser may poll/inspect; re-request after conclusion is policy-gated.

## 7. Provider timeout (provider too slow, later succeeds)

- Same durable/external split as §4; provider-side eventual success surfaces during
  reconciliation. Verification may also eventually confirm. Class RC.

## 8. Provider success with lost response (§4 alias, highest-frequency real case)

Covered by §4; persisted idempotency reference + providerRef make reconciliation
deterministic when the provider supports lookup-by-reference.

## 9. Provider partial failure (some effects applied, some not)

- Durable: attempt records observed partial result + residue.
- State: PARTIALLY_REVERSED with explicit residue records; never silently REVERSED.
- Retry: RC/M — resuming partial reversal is provider-specific; residue tells the
  operator what remains.

## 10. Verification failure (postconditions do not hold)

- Durable: verification_result(FAILED) + Action→REVERSE_FAILED (via evidence).
- Distinct from invocation failure (I-10). Retry: N (semantics failed, not transport).
- Manual: possible (provider state inspectable but wrong).

## 11. Verification uncertainty (verifier's own calls failed; eventually-consistent state)

- Durable: verification_result(UNKNOWN); Action unchanged (REVERSING/OUTCOME_UNKNOWN).
- Never recorded as reversal failure (I-10, §47). Retry: RC after backoff.

## 12. Concurrent reversal attempts across restarts

- Unique non-concluded-attempt index prevents duplicates; a RUNNING attempt found at
  restart with no provider evidence ⇒ re-enter execution with the same attempt id
  (idempotent) rather than orphaning it.

## 13. Concurrent resource mutation (someone else changed the resource)

- CAS/version check at execution ⇒ reversal-conflict; plan was stale (I-7, I-19).
- Durable: nothing mutated; conflict recorded on attempt for audit.

## 14. Expired reversal (eligibility passed before request)

- Rejected with `reversal-expired` problem; EXPIRED is absorbing for new attempts (I-8).
- In-flight attempt at expiry: completes (documented §52 semantics).

## 15. Crash-point matrix (§59; each row must have a recovery test — I-11)

| # | Crash point | Durable state | Possible external state | Recovery behavior | Outcome |
|---|---|---|---|---|---|
| C1 | before Action journal write (local path: intent row uncommitted) | none | none (local path external effect is in same txn) | nothing to do | known |
| C2 | after journal commit, before external effect (external-provider case) | Action APPLIED (+INTENT row) | none yet | outbox-style dispatch resumes | known |
| C3 | after external effect, before receipt persist | intent row only | **effect exists** | recovery scans INTENT ⇒ reconcile; I-16 inconsistency surfaced | unknown→reconcile |
| C4 | before HTTP response (client hasn't seen receipt) | Action persisted | effect exists | client retry by idempotency returns receipt; receipt never duplicated | known server-side |
| C5 | after reversal attempt creation, before provider call | attempt RUNNING + execution identity | none | recovery parks ⇒ OUTCOME_UNKNOWN; reconciliation lookup (identity never used) proves non-execution under a guaranteeing contract | unknown→reconcile (M4; supersedes re-execute) |
| C6 | after provider reversal succeeds, before result persisted | attempt RUNNING + execution identity | **reversal applied** | recovery parks ⇒ OUTCOME_UNKNOWN; reconciliation lookup proves execution ⇒ REVERSED (never REVERSE_FAILED) | unknown→reconcile |
| C7 | during verification | attempt result recorded or not; verification row partial/absent | reversal applied | verification re-runnable (read-only) | known-ish; re-verify |
| C8 | during reconciliation | reconciliation record absent/partial | reversal applied or not | reconciliation re-runnable (read-only lookups) | remains unknown until evidence |

## 16. Missing reversal material (§78)

- Private material expired/purged or lost while Action still shows eligible.
- Reversal request ⇒ `precondition-failed`/`reversal-material-missing` problem;
  Action eligibility is corrected (EXPIRED or MANUAL), never guessed around.
- Test: `TestMissingMaterialRejected`.

## 17. Authorization change (§78)

- Principal lost `reverse` between plan and execution ⇒ authorization-denied at
  execution; the plan was never authorization (I-2, §40). Class N until policy changes.

## 18. Capability change (§78)

- Provider removed reversal capability after Action creation: Action-time metadata
  still governs (I-15); execution may still fail with provider-side refusal ⇒
  NON_RETRYABLE; current capability is reported, history not rewritten.

## 18a. Dependency conflicts (M5)

- A dependency created after planning blocks reversal at execution: the stale
  plan never bypasses it (I-19). Durable state unchanged; conflict recorded in
  the problem response (`dependency-exists`, 409).
- Resolving the dependent (per the documented active-dependent rule, OQ-15)
  re-enables the parent's reversal; nothing is scheduled automatically.
- Residue discovered during reversal is durable append-history: a crash after
  recording residue but before conclusion leaves the evidence chain intact for
  recovery (I-25).

## 19. Throttling (§55)

- HTTP 429 / provider quota: attempt stays REVERSING (PENDING backoff); transport
  throttle is never semantic failure; retry honors backoff + classification (I-20).

---

## Summary guarantee statement (what we actually claim)

1. Every crash leaves the system in a state recovery can classify without guessing.
2. Unknown outcomes persist until evidence arrives (reconciliation/verification).
3. Retries are safe only with idempotency (client key + provider reference).
4. Conflicts stop unsafe restoration; residue is reported, never hidden.
5. The one unreduced window: an external effect that occurred before any durable
   intent/receipt write (C3) is detectable only by reconciliation scanning, and only
   if an intent row exists — the protocol cannot close this window (§58), and no doc
   claims otherwise.
