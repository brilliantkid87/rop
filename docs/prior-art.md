# Prior Art — ROP

Status: draft for architecture review (Architecture Gate, Master Prompt v0.3 §4, §83).

> ROP is an experimental protocol / proposal / research project. This document surveys
> prior art honestly. Where an existing mechanism suffices, the conclusion MUST be
> "reuse or interoperate", not "reinvent".

## How to read this document

For each mechanism: what problem it solves, where its semantics overlap ROP, where they
differ, whether ROP is actually adding an interoperable primitive over it, and whether
ROP should reuse the existing concept instead.

Note on currency: claims about specification status were verified against public sources
in August 2026 (see references). Claims about the *patterns* (Saga, TCC, outbox, etc.)
are established distributed-systems literature and are not re-sourced per claim.

---

## 1. Saga pattern

- **Problem:** Execute a long-running business process as a sequence of local
  transactions, each with a defined compensating transaction, because distributed ACID
  is unavailable.
- **Overlap:** ROP's `COMPENSATABLE` class and "compensating Operation" concept are
  Saga vocabulary. Both accept that the original effect remains historically true.
- **Difference:** Saga is a *workflow coordination* pattern: the developer owns the
  sequence and defines compensations per step. ROP is per-operation, provider-owned,
  and explicitly does not own the workflow (Master Prompt §81). ROP standardizes
  *discovery and representation* of reversibility; Saga standardizes nothing on the wire.
- **Is ROP adding a primitive?** Yes, a narrow one: machine-readable, operation-level
  reversibility metadata plus receipts that a Saga coordinator (or any client) could
  consume. ROP does not replace Saga and must not require workflow ownership — if it
  does, the abstraction fails its own review criterion (§84).
- **Reuse?** Reuse the terminology (compensation) and the "unknown outcome" humility.
  Do not adopt Saga's coordination role.

## 2. Compensating transactions (general)

- **Problem:** Undo the *business effect* of a committed operation with a new forward
  operation (refund, credit note) rather than restoring old state.
- **Overlap:** This is ROP's `COMPENSATABLE`/`PARTIALLY_COMPENSATABLE` semantics in full.
- **Difference:** None conceptually; ROP only adds standard representation, lifecycle,
  and verification around it.
- **Is ROP adding a primitive?** Only in representation/interoperability, not in
  mechanism. This is the honest boundary of ROP's value.
- **Reuse?** Yes — ROP is compensation with a machine-readable contract on top.

## 3. Try-Confirm-Cancel (TCC)

- **Problem:** Reserve resources before committing, so cancellation is cheap and
  happens *before* the effect (reservation semantics).
- **Overlap:** TCC's Cancel is a reversal of a reserved state.
- **Difference:** TCC is a two-phase *coordination protocol* participants opt into at
  design time. ROP acts on effects that already occurred, after the fact, without
  pre-coordination.
- **Is ROP adding a primitive?** Different primitive. TCC needs provider cooperation
  upfront; ROP describes capability and eligibility of already-executed Actions.
- **Reuse?** No direct reuse; cite TCC as an alternative where reservation is possible
  (and note in ROP docs that pre-reservation is often the better design).

## 4. WS-BusinessActivity (WS-BA, OASIS WS-TX 1.2)

- **Problem:** Coordinate outcome of long-running multi-service business activities
  over SOAP with participant-driven completion (BusinessActivityWithParticipantCompletion
  / CoordinatorCompletion), including compensation.
- **Overlap:** Closest historical ancestor of ROP's lifecycle: explicit participant
  states (e.g. Active, Compensating, Completed, CannotComplete), coordinator/participant
  messages, and the principle that the original work remains done.
- **Difference:** WS-BA is a *coordination protocol between enrolled participants* with
  heavyweight XML/SOAP infrastructure; it assumes a coordinator that owns the activity.
  ROP has no enrollment, no coordinator role over workflows, and is a single
  client↔provider interaction.
- **Status (verified Aug 2026):** WS-AT/WS-BA 1.2 are final OASIS Standards (2009); the
  OASIS WS-TX TC is dormant; the stack is legacy in practice. Modern architectures did
  not adopt it for REST-era APIs.
- **Is ROP adding a primitive?** The *lifecycle semantics* partially duplicate WS-BA,
  but its wire protocol and coordination model are dead and REST-native APIs never had
  an equivalent. The gap ROP targets — plain HTTP/JSON APIs exposing reversibility to
  arbitrary machine clients — remains unfilled by WS-BA.
- **Reuse?** Do not resurrect WS-BA wire semantics. Learn from its participant state
  model and from *why it failed to interoperate* (complexity, coordination
  requirements).

## 5. MicroProfile Long Running Actions (LRA)

- **Problem:** Saga-style coordination for JAX-RS microservices: annotated resources
  join an LRA; compensations run on cancel/close.
- **Status (verified Aug 2026):** Active — LRA 2.0.1 released March 2025; Narayana is
  the reference implementation, integrated in WildFly, Quarkus (`narayana-lra`), Open
  Liberty, Helidon. It is an optional standalone MicroProfile spec (not in the platform
  core), JVM/JAX-RS-centric.
- **Overlap:** LRA has the closest surface similarity to ROP: HTTP-based, timeout
  semantics, compensation callbacks, explicit states.
- **Difference:** LRA is a *coordinator-centric workflow* protocol: a client starts an
  LRA, participants enlist, the coordinator drives compensation of the whole activity.
  ROP has no coordinator, no enlistment, and reverses one Action at a time. LRA is also
  Java-ecosystem-specific in practice.
- **Is ROP adding a primitive?** Yes, if (and only if) ROP stays non-coordinating:
  per-operation, language-neutral, client-initiated reversibility discovery and
  invocation. If ROP grows enlistment/coordinator semantics, it becomes a worse LRA.
- **Reuse?** Study LRA's timeout/recovery design (it has the same lost-response and
  recovery problems ROP does). Do not reuse its coordination model.

## 6. Distributed transactions / two-phase commit (2PC)

- **Problem:** Atomic commit across resource managers with a coordinator and prepared
  participants.
- **Overlap:** Superficial only — both deal with failure between "decided" and
  "applied".
- **Difference:** 2PC is atomic and blocking with a coordinator; ROP explicitly
  disclaims atomicity, batches, and coordination (§29, §30, §79). ROP's `OUTCOME_UNKNOWN`
  state has no analogue in 2PC's in-doubt model (where the coordinator's log resolves
  the outcome deterministically).
- **Is ROP adding a primitive?** Yes — ROP targets exactly the space where 2PC is
  impossible (uncooperative external providers).
- **Reuse?** No. Cite 2PC as the anti-goal.

## 7. Event sourcing

- **Problem:** Persist state as an append-only event log; state is derived by replay.
- **Overlap:** ROP's append-preserving Action history and "audit survives reversal"
  invariant are event-sourcing instincts. A reversal is a new event, not an erasure.
- **Difference:** Event sourcing is a persistence architecture, not an interop
  protocol; it standardizes nothing across providers.
- **Is ROP adding a primitive?** Different layer. ROP's journal could be implemented
  via event sourcing.
- **Reuse?** Optionally as an implementation technique internally; not a protocol
  dependency.

## 8. Durable / temporal workflows (Temporal, Cadence, AWS Step Functions)

- **Problem:** Reliable execution of multi-step logic with durable state, retries, and
  compensation activities.
- **Overlap:** Retry taxonomy, reconciliation, and "unknown outcome" handling are
  problems Temporal solves *within an owned workflow*.
- **Difference:** Temporal owns orchestration, timers, and the code that runs. ROP is a
  protocol between independent parties that share no runtime.
- **Is ROP adding a primitive?** Yes — cross-organization, non-coordinated
  reversibility. But §84 check: if ROP requires workflow ownership, it fails; Temporal
  users would embed ROP semantics, not the reverse.
- **Reuse?** No; treat as complementary consumer.

## 9. Infrastructure rollback (Terraform, cloud deployment rollbacks)

- **Problem:** Return infrastructure to a prior declared state.
- **Overlap:** ROP's `RESTORABLE` class (restore captured prior configuration).
- **Difference:** Rollback is tool-scoped, single-system, and trusts its own snapshots.
  ROP standardizes the *contract* about what restore means, its residue, and its
  freshness/TOCTOU guards; it does not manage snapshots itself.
- **Is ROP adding a primitive?** Marginal. Rollback tools already do the work; ROP's
  contribution is the machine-readable semantics and conflict rules (§41, §48).
- **Reuse?** Reuse the lessons: plans are stale the moment they are printed; compare
  before apply.

## 10. Kubernetes reconciliation

- **Problem:** Continuously drive observed state toward desired state; controllers
  retry, and outcomes converge.
- **Overlap:** Reconciliation as first-class, "unknown is not failed", idempotent
  operations.
- **Difference:** K8s reconciliation is *declarative and level-driven* (desired state
  owned by a controller). ROP reversal is *event-driven on a historical Action*; there
  is no desired state to converge to, only a provider-defined postcondition.
- **Is ROP adding a primitive?** Yes, but a smaller one than it first appears; ROP's
  "reconciliation" (§38) is closer to K8s' *status resync* than to full reconciliation
  loops.
- **Reuse?** Adopt the vocabulary ("reconcile" for resolving unknown outcomes) and the
  humility of level-driven thinking where it fits.

## 11. Idempotency protocols (Idempotency-Key; Stripe et al.)

- **Problem:** Make repeated identical requests safe under retries.
- **Overlap:** ROP §36 mandates idempotent reversal requests; Stripe's
  `Idempotency-Key` with stored first-response is the de-facto standard.
- **Difference:** None in mechanism; ROP adds the requirement that idempotency protects
  *ROP request handling*, not the provider's underlying refund/delete semantics.
- **Is ROP adding a primitive?** No — this is pure reuse.
- **Reuse?** Fully adopt `Idempotency-Key` semantics with durable key storage. Do not
  invent an alternative.

## 12. Optimistic concurrency control (OCC) / HTTP conditional requests (ETag, If-Match)

- **Problem:** Detect concurrent mutation before applying a change.
- **Overlap:** ROP's plan freshness (§40), TOCTOU defense (§41), and concurrent
  mutation conflict (§48) are OCC applied to reversal.
- **Difference:** ROP adds the semantic rule "a stale plan must yield CONFLICT, not
  destructive restoration", which generic OCC does not encode.
- **Is ROP adding a primitive?** Only the policy, not the mechanism.
- **Reuse?** Mandate reuse: where resources have versions/ETags, reversal MUST use
  conditional/compare-and-swap semantics.

## 13. Transactional outbox / write-ahead logging

- **Problem:** Atomically persist "intent/state change" with local state so effects and
  journal cannot diverge invisibly.
- **Overlap:** ROP's §58 (missing receipt after external effect) and §59 (crash
  consistency) are outbox/WAL problems: journal-before-effect ordering, recovery by
  scanning durable evidence.
- **Difference:** Outbox solves local atomicity of DB-write + message publication; ROP
  must handle the case where the *external effect* is the thing that happened (or
  didn't) and no journal entry exists — outbox reduces but cannot eliminate that (and
  §58 forbids claiming otherwise).
- **Is ROP adding a primitive?** No — reuse the pattern; document its guarantee
  honestly (intent journal ⇒ at-most-one uncertain window, not zero).
- **Reuse?** Yes: intent-journal/outbox is the recommended strategy for the reference
  implementation's action-taking path.

## 14. Recovery and reconciliation systems (database recovery, payment gateways' settlement reconciliation)

- **Problem:** After crashes or lost responses, determine what actually happened and
  converge.
- **Overlap:** ROP §34 (`OUTCOME_UNKNOWN`), §38 (reconciliation), §60 (restart
  contract) are exactly this domain.
- **Difference:** Payment reconciliation is domain-internal; ROP makes the *protocol*
  express "outcome unknown, reconcile" to external clients.
- **Is ROP adding a primitive?** Modestly — mostly standardization of vocabulary and
  lifecycle.
- **Reuse?** Reuse WALredo/recovery principles: recovery must inspect durable evidence,
  never assume failure from missing success (§60).

---

## Findings

**Distinction required by the approved architecture review (2026-08-30):** mechanism
reuse vs ROP's interoperability hypothesis. ROP does **not** claim novelty for any
mechanism in this survey — compensation, idempotency keys, optimistic concurrency,
outbox/intent journals, reconciliation, and explicit unknown-outcome states are all
established distributed-systems techniques, and this document must never describe them
as ROP inventions. ROP's single testable hypothesis is:

> whether exposing these established semantics **consistently at the Operation/Action
> interoperability boundary** — machine-readable across independent providers — creates
> useful new value for clients that have no private knowledge of any provider's
> internals.

Every finding below should be read as "which mechanism ROP reuses", not "what ROP
invented".

1. **No existing standard fills the exact gap ROP targets**: machine-readable,
   per-operation reversibility metadata + receipt + eligibility + verification
   semantics for plain HTTP/JSON APIs, without coordination or enrollment. WS-BA and LRA are the
   closest, but both are coordinator/participant protocols; WS-BA is dormant, LRA is
   JVM-ecosystem and workflow-shaped.
2. **ROP's mechanism layer is entirely prior art.** Compensation (Saga), idempotency
   keys, OCC/ETag, outbox, reconciliation are all established. ROP must be honest that
   its only claim to novelty is *standardized representation and lifecycle across
   independent providers* — the "does that create real interoperability value?" question
   is the central risk (see `architecture.md` self-review).
3. **Highest-risk duplication:** LRA. ROP must visibly diverge (no coordinator, no
   enlistment, single-Action scope) or the protocol is redundant.
4. **Consequence adopted by the approved review:** reversal *invocation* is the least
   novel, least adoptable surface — providers already own refund/cancel endpoints —
   so it is an **optional advertised capability** (`capability-model.md` §3), never a
   conformance requirement. The Core is metadata + receipts + eligibility +
   verification. Planning and reconciliation are likewise optional capabilities.
   This resolves the "subset is enough" question of §87 at the scope level: the subset
   *is* the protocol core; invocation is an add-on the reference implementation
   exercises to test the end-to-end path.

## References

- MicroProfile LRA: https://microprofile.io/specifications/lra/ (LRA 2.0.1, 2025-03-11);
  https://github.com/eclipse/microprofile-lra ; Narayana LRA: https://narayana.io/lra/ ;
  Quarkus LRA guide: https://quarkus.io/guides/lra
- OASIS WS-AtomicTransaction 1.2: https://docs.oasis-open.org/ws-tx/wstx-wsat-1.2-spec.html ;
  WS-Coordination 1.2: https://docs.oasis-open.org/ws-tx/wstx-wscoor-1.2-spec.html
  (WS-TX TC dormant; verified Aug 2026 via public OASIS sources)
- Saga pattern: Garcia-Molina & Salem, "Sagas" (1987); modern summaries: microservices.io
  (Chris Richardson), *Designing Data-Intensive Applications* ch. 9 (Kleppmann)
- RFC 2119 / RFC 8174 (normative language); RFC 9457 (Problem Details);
  RFC 7232 (conditional requests) / RFC 9110 §13
