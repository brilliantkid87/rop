# ROP Open Questions Register

Status: draft for architecture review (Architecture Gate, Master Prompt v0.3 §71).
Rule: unresolved semantic decisions live here; they must not silently become permanent
API contracts. Tentative decisions are provisional and reviewed at each milestone.

Format per entry: question / why it matters / options / tradeoffs / tentative decision /
resolving evidence.

---

## OQ-1 — Should reconciliation become part of public ROP/HTTP?
- **Status (M6):** DEFERRED
- **Why:** clients observing `OUTCOME_UNKNOWN` need a way to nudge or await resolution;
  hiding reconciliation makes unknown states a dead end for them.
- **Options:** (a) internal-only (CLI/admin); (b) public `POST
  .../actions/{id}/reconcile`; (c) no endpoint — verification GET implicitly
  reconciles.
- **Tradeoffs:** (b) adds an expensive, abusable verb to the public surface; (a) keeps
  protocol small but leaves clients stuck; (c) conflates verify with reconcile.
- **Tentative:** (a) for v0.1, with (c) explicitly *not* implied; revisit at Milestone 4.
- **Resolving evidence:** real usage of OUTCOME_UNKNOWN in the demo; how often a client
  needs to trigger vs. just poll.
- **M4 update (2026-08-30):** reconciliation is now implemented as a first-class
  internal domain operation (evidence-gated, idempotent, durable observations). The
  internal-only decision held: no public endpoint and no CLI verb were needed to prove
  the semantics. This question stays open for any future public release.

## OQ-2 — Should Action Receipts eventually be signed?
- **Status (M6):** DEFERRED
- **Why:** forgeable receipts (security.md T2/T3) cap the trust clients can place in
  offline metadata.
- **Options:** none in v0.1; future JOSE/Ed25519 signatures; tamper-evident hash chain
  in the audit log.
- **Tradeoffs:** signing requires key management and canonicalization (the prompt
  forbids premature canonical-byte JSON, §61).
- **Tentative:** design receipts so a `signatures` field can be added without breaking
  parsers (unknown-field tolerance); implement nothing.
- **Resolving evidence:** any deployment where receipts are consumed offline.

## OQ-3 — Should residue have standardized categories?
- **Status (M6):** RESOLVED_FOR_V0_1
- **Why:** residue is first-class (§45); free-form strings limit client reasoning.
- **Options:** (a) free-form provider text; (b) small closed set (`FEE`,
  `AUDIT_RECORD`, `EXTERNAL_OBSERVATION`, ...); (c) open set with registry.
- **Tradeoffs:** closed sets get wrong fast (taxonomy inflation, §44 warning); free
  form is unreadable for machines.
- **Tentative:** (a) free-form in v0.1; collect real examples before standardizing.
- **Resolving evidence:** ≥3 providers' residue descriptions showing stable overlap.

## OQ-4 — Should verification results have a standard confidence model?
- **Status (M6):** RESOLVED_FOR_V0_1
- **Why:** "verified" for an eventually-consistent external read is weaker than a local
  read (§47); clients shouldn't over-trust.
- **Options:** (a) boolean verified/not; (b) verification semantics field
  (`LOCAL_READONLY` / `EXTERNAL_READONLY` / `EVENTUALLY_CONSISTENT` / `EXPENSIVE`);
  (c) numeric confidence (forbidden in spirit by §39's risk-score ban).
- **Tradeoffs:** (b) is honest and small; (a) hides staleness; (c) is fake precision.
- **Tentative:** (b), with `UNKNOWN` verification outcome.
- **Resolving evidence:** demo cases where (b) changes client behavior.

## OQ-5 — Should preflight discovery exist outside OpenAPI (`x-rop`)?
- **Status (M6):** DEFERRED
- **Why:** many providers don't ship OpenAPI; a discovery-document extension (beyond
  capability flags) could carry Operation metadata at runtime.
- **Options:** (a) x-rop only; (b) x-rop + runtime operation metadata in the discovery
  document; (c) per-operation well-known endpoint.
- **Tradeoffs:** (b) duplicates a metadata surface (drift risk); (a) couples ROP to
  OpenAPI adoption.
- **Tentative:** (a) for v0.1; discovery document carries only capability flags (§20).
- **Resolving evidence:** a non-OpenAPI provider attempting adoption.

## OQ-6 — Should the internal retry taxonomy become protocol-visible?
- **Status (M6):** STILL_OPEN
- **Why:** clients need retry guidance; but §37 warns against freezing vocabulary
  prematurely.
- **Options:** (a) keep internal; (b) expose problem `type` URIs that imply retry class;
  (c) explicit `retryable: true/false` fields.
- **Tradeoffs:** (b) gives semantics without a new enum family; (c) invites lying.
- **Tentative:** (b) — problem types already encode semantics; no numeric/boolean flags.
- **Resolving evidence:** client retry bugs in the demo that (b) doesn't prevent.

## OQ-7 — Is a plan a resource (GET-able URI) or only a response body?
- **Status (M6):** RESOLVED_FOR_V0_1
- **Why:** affects caching, freshness representation, and whether clients can share
  plans; also touches I-19 (plans are never authority).
- **Options:** (a) response body only; (b) durable plan resource with `validUntil`.
- **Tradeoffs:** (b) risks plans *feeling* like authority; (a) forces replanning (which
  is arguably the correct behavior anyway).
- **Tentative:** (a); plans are ephemeral snapshots.
- **Resolving evidence:** a credible client workflow that needs durable plans.

## OQ-8 — Does TTL belong to the Operation, the Action, or both?
- **Status (M6):** RESOLVED_FOR_V0_1
- **Why:** §5/§53 distinguish capability from eligibility; a TTL on the Operation is a
  default, on the Action it is the truth.
- **Options:** (a) Operation TTL = default, Action stores its own computed
  `expiresAt`; (b) Operation TTL only, evaluated live.
- **Tradeoffs:** (b) breaks I-15 (capability changes rewriting history); (a) stores one
  more column but keeps history honest.
- **Tentative:** (a) — Action carries `expiresAt` computed at creation time.
- **Resolving evidence:** none anticipated against; revisit only if provider semantics
  demand live TTL.

## OQ-9 — How does the demo resource API embed ROP (library vs sidecar)?
- **Status (M6):** RESOLVED_FOR_V0_1
- **Why:** §73 requires the demo to mutate real persisted state with receipts; the
  embedding pattern shapes `internal/` boundaries and the provider abstraction (§76).
- **Options:** (a) in-process Go package (demo imports ROP core); (b) separate ropd
  process the demo calls.
- **Tradeoffs:** (a) simpler MVP, risks Core depending on embedding assumptions;
  (b) exercises the HTTP binding from day one but doubles M1 scope.
- **Tentative:** (a) in-process for M1–M2; the HTTP binding is exercised by ropd itself;
  revisit (b) at M4.
- **Resolving evidence:** whether the provider abstraction (§76) stays clean under (a).

## OQ-10 — `.well-known/rop` path and namespace ownership
- **Status (M6):** STILL_OPEN
- **Why:** `/.well-known/` has registration rules (RFC 8615); `ROP-Action-ID` header
  names should be registered if the protocol ever goes public.
- **Options:** (a) use the path as-is for the experiment; (b) pursue provisional
  registration.
- **Tradeoffs:** (b) is process overhead for a research project; (a) risks collision.
- **Tentative:** (a), with a note in `spec/http-binding.md` that registration precedes
  any public release.
- **Resolving evidence:** decision to publish v1.0 publicly.

## OQ-11 — Unimplemented optional capability: problem semantics
- **Status (M6):** RESOLVED_FOR_V0_1
- **Why:** `capability-model.md` §3 says requests to a non-advertised capability's
  endpoint MUST fail with a stable problem type — but which one and which status?
- **Options:** (a) `404` + `action-not-found` (pretend the endpoint does not exist);
  (b) `501` + `capability-unavailable` (explicitly signals "known capability, not
  offered"); (c) `405`.
- **Tradeoffs:** (a) risks confusing a capability probe with a typo'd path; (b) is
  explicit but some clients treat 5xx as retryable server fault; (c) is method-level
  and imprecise.
- **Tentative:** (b) with a non-retryable semantics; frozen after M1 HTTP-binding
  experience.
- **Resolving evidence:** client behavior in the M1 demo against a stripped-capability
  instance (M1 test fixture runs discovery with `reversal: false`).

## OQ-12 — Conformance fixtures for partial implementations
- **Status (M6):** RESOLVED_FOR_V0_1
- **Why:** `capability-model.md` §4 allows providers without `reversal`/`planning`;
  golden fixtures must let a validator test Level 1 without Levels 2–3.
- **Options:** (a) one fixture set per level; (b) Level 1 fixtures only, level
  behavior asserted via discovery booleans.
- **Tradeoffs:** (a) more fixture maintenance; (b) may under-specify capability
  response shapes.
- **Tentative:** (b) for v0.1: `spec/fixtures/v0.1/` freezes Core (Level 1) wire
  shapes; capability responses get fixtures in M2/M3 as those capabilities stabilize.
- **Resolving evidence:** first attempt to validate a Core-only mock provider.

## OQ-13 — Eligibility read without reversal capability
- **Status (M6):** RESOLVED_FOR_V0_1
- **Why:** Core requires current-eligibility reporting, but `eligibility` was
  originally framed in terms of reversal; a provider with no `reversal` capability
  still must report status/expiresAt meaningfully.
- **Options:** (a) eligibility always defined as "would reversal be possible if the
  capability existed"; (b) eligibility defined as provider-declared constraints only
  (expiry, state), never implying invocability.
- **Tradeoffs:** (a) misleads clients when `reversal` is not advertised; (b) is
  honest but less actionable.
- **Tentative:** (b) — eligibility reports state + expiresAt + constraints; whether
  reversal can be *requested through ROP* is decided solely by discovery flags.
- **Resolving evidence:** client confusion observed in M1 demo usage.

## OQ-14 — Is verification evidence alone sufficient to conclude an unknown attempt?
- **Status (M6):** RESOLVED_FOR_V0_1
- **Why:** M4 requires an explicit position: verification ("does the postcondition
  hold?") and reconciliation ("what happened to the execution?") are distinct; but when
  verification shows the postcondition holds during an OUTCOME_UNKNOWN attempt, the
  temptation is to conclude REVERSED from it.
- **Options:** (a) verification evidence may conclude unknown attempts;
  (b) only reconciliation's proven provider evidence concludes attempts; verification
  informs operators, never concludes state.
- **Tradeoffs:** (a) resolves uncertainty faster but conflates two concepts and lets a
  weak or stale postcondition check rewrite execution history; (b) preserves the
  separation and the evidence chain, at the cost of an extra reconciliation round.
- **Tentative:** (b) for the reference implementation — verification evidence alone
  does not conclude an unknown attempt; the required M4 test
  (`TestVerificationAndReconciliationRemainDistinct`) pins this behavior.
- **Resolving evidence:** a deployment where reconciliation is unavailable but
  verification is authoritative would argue for (a); none observed yet.

## OQ-15 — What counts as an "active dependent Action"?
- **Status (M6):** RESOLVED_FOR_V0_1
- **Why:** M5 requires an explicit rule, not "dependent exists ⇒ block forever":
  whether a dependent in a terminal state (e.g. REVERSED) stops blocking its parent
  determines when a reversal becomes safe again.
- **Options:** (a) any dependent blocks forever until its record is deleted;
  (b) status-based rule: dependents in REVERSED / PARTIALLY_REVERSED are resolved and
  stop blocking; all other statuses block; (c) provider-declared per-edge resolution
  conditions.
- **Tradeoffs:** (a) is safe but makes reversals permanently blocked after ordinary
  use; (b) is small, testable, and matches compensation semantics (a compensated
  dependent no longer endangers the parent), but REVERSE_FAILED / EXPIRED /
  IRREVERSIBLE dependents keep blocking (their effect stands); (c) is the most
  precise but pushes provider-specific logic into the protocol prematurely.
- **Tentative:** (b), implemented in `internal/dependency.ResolvedStatuses` and
  pinned by `TestActiveDependentRule`; (c) is the natural extension if providers need
  edge-specific semantics.
- **Resolving evidence:** real dependency scenarios where (b) unblocks a reversal
  that is still unsafe, or blocks one that is safe.

---

## M6 resolution summary

- **RESOLVED_FOR_V0_1:** OQ-3, OQ-4, OQ-7, OQ-8, OQ-9, OQ-11, OQ-12, OQ-13,
  OQ-14, OQ-15 — each decided by implemented, tested behavior.
- **DEFERRED:** OQ-1 (public reconciliation surface), OQ-2 (receipt signing),
  OQ-5 (preflight beyond OpenAPI) — hooks preserved, evidence insufficient.
- **STILL_OPEN:** OQ-6 (protocol-visible retry guidance), OQ-10 (`.well-known`
  and header namespace registration) — both affect future interoperability,
  neither blocks the v0.1 experiment; both are called out in
  `docs/v0.1-assessment.md`.

## Retired questions

None yet. Questions are retired (moved here with their resolution) only by an explicit,
documented project decision.
