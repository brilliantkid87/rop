# ROP Security / Threat Model

Status: draft for architecture review (Architecture Gate, Master Prompt v0.3 §18).
> ROP is an experimental protocol / proposal / research project. This document does not
> claim cryptographic security properties that are not implemented — currently, none are.

## Assets

1. **Reversal authority** — the ability to cause a provider to reverse an Action
   (often financially or operationally consequential).
2. **Action / audit history** — append-only record; must not be erasable or rewritable.
3. **Private reversal material** — previous versions, snapshots, prior configurations,
   reconciliation keys; disclosure enables unsafe or malicious reversal.
4. **Receipt integrity** — clients act on receipts; forged receipts mislead clients.
5. **Idempotency keys** — theft enables colliding with a victim's request stream.
6. **Scope isolation** — tenant/scope boundaries in the store.

Attacker archetypes: unauthenticated network client; authenticated low-privilege
principal (another tenant); malicious Action-ID holder; compromised provider adapter;
compromised ROP process operator (out of scope — full compromise is game over; noted,
not mitigated).

---

## Threats

### T1 — Action ID enumeration
- **Asset:** 1, 2. **Capability:** unauthenticated client probing IDs.
- **Attack:** enumerate `/.well-known/rop/actions/{id}` to discover Actions across
  scopes.
- **Mitigation:** high-entropy IDs (e.g. 128-bit, ULID-style); **explicitly NOT
  authorization** (I-2); cross-scope requests return not-found without existence
  confirmation (I-13); authz required for `inspect`.
- **Residual risk:** a low-privilege principal *within* a scope still learns that
  scope's Actions — by design.

### T2 — Forged Action Receipts
- **Asset:** 4. **Capability:** client-side attacker injecting receipts.
- **Attack:** fabricate `ROP-Action-ID` / receipt JSON to make a client believe an
  Action exists or is reversible.
- **Mitigation:** clients treat receipts as claims to *verify against the provider*
  (GET action resource), not as proof; TLS for transport. No signing in v0.1 (§19).
- **Residual risk:** unsigned receipts are forgeable in transit without TLS and
  unattributable; documented as future work (OQ-2).

### T3 — Receipt tampering
- **Asset:** 4. Same class as T2; store-side receipts are durable rows (append history)
  so in-store tampering is detectable by history comparison. Mitigation and residual
  risk as T2.

### T4 — Replayed reversal requests
- **Asset:** 1. **Attack:** capture/replay a `reverse` request.
- **Mitigation:** replay either hits the idempotency dedupe (same key ⇒ same result,
  no new execution) or the one-non-concluded-attempt constraint; a *replayed* reversal
  after a concluded attempt is policy-gated, never automatic.
- **Residual risk:** a stolen key *before* the legitimate request arrives could
  pre-empt it; TLS + short key validity mitigate operationally (documented).

### T5 — Stolen Idempotency-Key values
- **Asset:** 5. **Attack:** attacker supplies victim's key on a different request to
  hijack/deny the operation.
- **Mitigation (design rule):** idempotency scope = (principal, operation, key) — a key
  is only consulted within the same principal and operation; mismatched key/operation ⇒
  idempotency-conflict problem, not reuse.
- **Residual risk:** key theft within the same principal identity (rare; noted).

### T6 — Cross-tenant reversal attempts
- **Asset:** 1, 6. **Attack:** scope-B principal requests reversal of scope-A Action.
- **Mitigation:** scope column on all tables; all lookups/planning/reversal/
  verification/reconciliation queries scope-filtered (I-13); not-found responses (no
  existence leak); authz evaluated per verb.
- **Residual risk:** a bug in a hand-written query bypassing the scope filter is the
  main residual; mitigated by a single repository layer + tests.

### T7 — Privilege escalation
- **Asset:** 1, 3. **Attack:** low-privilege principal reaches high-privilege verbs
  (reverse/verify/reconcile) or private material.
- **Mitigation:** per-verb authz interface (§54); private material is in
  `reversal_material`, never serialized on public paths (I-14); reconcile is
  non-public in MVP (OQ-3).
- **Residual risk:** the MVP's trivial principal cannot demonstrate real escalation
  defense; single-tenant mode is explicitly not a security claim.

### T8 — Confused deputy
- **Asset:** 1. **Attack:** trick a privileged component (e.g. the demo provider
  adapter) into reversing an Action the attacker cannot directly touch (e.g. via
  crafted actionId or dependency graph).
- **Mitigation:** adapters re-validate scope + eligibility server-side; the deputy
  never trusts client-supplied resource identity beyond the durable Action record.

### T9 — Stale reversal plans
- **Asset:** 1, 3. **Attack:** execute an old plan after conditions changed to force
  destructive restoration.
- **Mitigation:** plans are not authorization (§40); execution re-validates all
  correctness-critical preconditions (I-19) and enforces version CAS (I-7); conflict
  over rollback.

### T10 — TOCTOU races
- **Asset:** 1, 3. **Attack:** mutate resource between eligibility check and provider
  call.
- **Mitigation:** CAS at execution; SQLite transactions serialize journal decisions;
  provider adapters perform the authoritative check as close to the effect as the
  provider allows.
- **Residual risk:** providers without version support cannot offer full protection;
  adapter contract requires declaring checkability (documented limitation).

### T11 — Malicious / compromised provider adapters
- **Asset:** 1, 2, 3. **Attack:** adapter lies about success, leaks material, or
  reverses wrong things.
- **Mitigation (partial):** verification is independent of execution success (I-10) —
  a lying adapter must also corrupt verification, doubling its cost; adapter contract
  is explicit; receipts never carry credentials (§12).
- **Residual risk:** **significant** — a fully compromised adapter can misreport
  verification too. Honest position: ROP does not defend against a malicious provider;
  it makes lying detectable only when verification has independent evidence paths.

### T12 — Forged provider responses
- **Asset:** 2. **Attack:** spoof provider API responses to force REVERSED.
- **Mitigation:** TLS + provider authentication in adapters; results store provider
  references enabling later re-verification.
- **Residual risk:** same as T11 for full MITM-level compromise.

### T13 — Dependency manipulation
- **Asset:** 1. **Attack:** add/remove dependency edges to block (DoS) or unblock
  (unsafe) reversal.
- **Mitigation (implemented M5):** dependency edges are durable, scope-local records
  created by provider-side Action recording (not client-supplied at reversal time);
  cycles and self-edges are rejected at the domain layer with a durable UNIQUE
  backstop; duplicate edges are idempotent no-ops; cross-scope edges are rejected
  (I-13); edges are never deleted — a dependent is unblocked only by its own recorded
  status becoming resolved (documented rule, OQ-15), never by rewriting the graph.
- **Residual risk:** a provider that records spurious dependencies can DoS its own
  Actions' reversals — by construction, since dependencies are provider-declared
  safety facts.

### T14 — Snapshot / reversal-material tampering
- **Asset:** 3. **Attack:** alter stored previous-version/snapshot to cause harmful
  restore.
- **Mitigation:** material rows are provider-side, access-restricted, and never
  client-writable; material expiration aligned with eligibility (§15).
- **Residual risk:** v0.1 implements no integrity check on material rows (no MAC);
  tampering by a DB-level attacker is undetectable — documented, deferred with OQ-2.

### T15 — DoS against expensive reversal/verification
- **Asset:** availability. **Attack:** trigger costly external verifications/reversals
  at scale.
- **Mitigation:** HTTP binding compatible with rate limiting (429 semantics per §55);
  verification documented as potentially expensive (§47); no batch surface in v0.1 to
  amplify.
- **Residual risk:** MVP has no built-in limiter; single-tenant demo only.

### T16 — Sensitive-data leakage (receipts, logs, errors, audit)
- **Asset:** 3. **Attack:** read private material via logs, error strings, receipts.
- **Mitigation:** public types structurally exclude material (I-14 test); structured
  logging forbids material fields (§68); error messages carry problem types, not
  provider payloads.
- **Residual risk:** string-laden debug logs are the classic leak; enforced by review
  + a log-scrubbing test where practical.

---

### T17 — Reconciliation abuse (added M4)
- **Asset:** 1, 3. **Attack:** use reconciliation lookups to probe provider state
  cross-scope, or spam lookups to exhaust provider quota (DoS).
- **Mitigation:** reconciliation is internal in v0.1 (no public endpoint, no CLI
  verb); it sits behind per-verb authorization (`VerbReconcile`) and scope-filtered
  lookups (I-13); lookups are read-only on one durable execution identity each.
- **Residual risk:** an operator with `reconcile` permission inside a scope can
  trigger provider lookups at will — accepted for the reference MVP, revisited if
  reconciliation gains a public surface (OQ-1).

## M6 second-pass review (against the implemented system)

The following checks were executed against the implementation (not just this
design), primarily in `examples/resource-api/m6_test.go` plus the M1–M5
security tests. Findings are stated honestly; one defect was found and fixed.

| Check | Result |
|---|---|
| Action IDs cannot authorize reversal | Holds (I-2 tests; per-verb authz). |
| Scope filtering on inspect/plan/reverse/verify | Holds (service + store level; cross-scope = not-found). |
| Dependency APIs do not leak cross-scope existence | Holds — edges are scope-local; an edge referencing a foreign-scope Action is rejected as not-found. |
| Idempotency keys cannot replay another Action's result | Holds — request fingerprint mismatch is rejected; fuzzed in `FuzzIdempotencyKeySemantics`. |
| Private material never on public paths | Holds (I-14 tests across receipts, plans, verification; M2 prior-state variant). |
| Stale plans cannot bypass execution checks | Holds (I-19 tests; M5 dependency-after-plan case). |
| Replay cannot cause duplicate compensation | Holds (I-21 tests; one execution per key/concurrent convergence). |
| Malformed identifiers | **Defect found and fixed:** problem `detail` reflected unbounded client-supplied IDs (a reflection channel). Fixed by bounding (256 chars) and stripping control characters at the binding boundary (`sanitizeDetail`); regression-tested in `TestMalformedActionIDsAreSafe`. Parameterized SQL means no injection; traversal attempts are normalized by the HTTP layer (301) or rejected. |
| Error responses do not expose resource existence | Holds — cross-scope and unknown IDs return identical not-found problems; only the caller's own (sanitized) input is echoed. |
| Residue descriptions carry no business payloads | Holds in the demo (`TestResidueDescriptionsCarryNoBusinessPayloads`); residue is provider-declared by contract, so this is a provider obligation enforced by review, not by ROP. |
| Reconciliation references are not authorization tokens | Holds — the execution identity is recorded evidence, appears on results, and grants nothing; it does not resolve to an Action by lookup. |

## Explicit non-claims

- No signed receipts, no attestation, no tamper-evident audit in v0.1 (§19; OQ-2).
- High-entropy IDs are privacy hygiene, not authorization (I-2).
- Single-tenant MVP mode makes T6/T7 defenses exercised but not production-hardened.
- No claim that ROP defends against a malicious provider or a fully compromised host.
