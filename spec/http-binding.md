# ROP/HTTP Binding — v0.1 (draft)

Status: draft normative HTTP binding for the approved (MODIFY, 2026-08-30)
scope. Core semantics (`spec/core.md`) are transport-neutral; everything
HTTP-specific lives here.

> ROP is an experimental protocol / proposal / research project.

## 1. Discovery

```http
GET /.well-known/rop
```

Response 200:

```json
{
  "protocol": "rop",
  "versions": ["0.1"],
  "binding": "http",
  "capabilities": {
    "receipts": true,
    "planning": true,
    "reversal": true,
    "verification": true
  }
}
```

- `capabilities.receipts` and `capabilities.verification` are Core: a
  conformant provider serves `true`. `planning` and `reversal` are optional
  advertised capabilities (`docs/capability-model.md` §3);
  `reconciliation` is future. Booleans MUST match actual behavior (§20).
- Vague claims ("ROP compatible") MUST NOT substitute for capability
  booleans.

## 2. Action status (Core)

```http
GET /.well-known/rop/actions/{actionId}
```

Response 200 — the receipt-shaped Action document with the live status
(eligibility per OQ-13 option (b): state + `expiresAt`; no implied
invocability):

```json
{
  "actionId": "act_01K...",
  "providerId": "rop-demo",
  "operationId": "resource.create",
  "createdAt": "2026-08-30T00:00:00Z",
  "resourceRef": { "resourceType": "resource", "resourceId": "res_01K..." },
  "reversibility": "REVERSIBLE",
  "guarantee": "EXACT",
  "status": "APPLIED",
  "expiresAt": "2026-08-30T01:00:00Z"
}
```

- Timestamps MUST be RFC 3339; servers SHOULD use UTC (§24). Server time
  governs eligibility; client time MUST NOT (§24).
- Expiration is applied (durably) when observed: an APPLIED Action whose
  window has passed is reported `EXPIRED` (boundary
  `receivedAt >= expiresAt`).
- Wire names are stable camelCase (§62): `actionId`, `operationId`,
  `resourceRef`, `resourceType`, `resourceId`, `reversibility`, `guarantee`,
  `status`, `expiresAt`, `createdAt`, `providerId`, `residue`, `attemptId`,
  `observedAt`, `postconditions`, `evaluatedAt`.

## 3. Plan reversal (optional capability: `planning`)

```http
POST /.well-known/rop/actions/{actionId}/plan-reversal
```

Response 200 — a read-only plan snapshot (`generatedAt`, optional
`basisResourceVersion`, optional `validUntil`, preconditions, expected
reversal, residue, conflicts, manual requirements). Planning MUST NOT cause
external side effects (I-3). A plan MUST NOT be treated as authorization
(I-19). No numeric risk scores (§39).

If `planning` is not advertised: `501` with problem type
`capability-unavailable` (OQ-11 tentative decision, non-retryable semantics).

## 4. Reverse (optional capability: `reversal`)

```http
POST /.well-known/rop/actions/{actionId}/reverse
```

- **Transport success is not semantic success (§28).** A `200` response body
  carries the semantic outcome:

```json
{
  "attemptId": "ra_01K...",
  "actionId": "act_01K...",
  "status": "REVERSED",
  "outcome": "REVERSED",
  "observedAt": "2026-08-30T00:05:00Z",
  "providerRef": "resource-delete:res_01K..."
}
```

`outcome` is one of `REVERSED`, `PARTIALLY_REVERSED`, `REVERSE_FAILED`,
`CONFLICT`, `OUTCOME_UNKNOWN`; `status` mirrors the resulting Action state.
A `200` with `outcome: "OUTCOME_UNKNOWN"` is a legitimate result (§34): the
provider may or may not have executed the reversal. Clients MUST NOT retry
blindly; a retry with the same `Idempotency-Key` replays the recorded result.
Resolution of `OUTCOME_UNKNOWN` is a provider-side reconciliation process; it
has no public endpoint in v0.1 (spec/core.md §12).

- Request-level rejections are problems: `404` `action-not-found`, `410`
  `reversal-expired`, `422` `irreversible-action` / `precondition-failed`,
  `409` `reversal-conflict` / `reversal-already-in-progress` /
  `idempotency-key-conflict`, `403` `authorization-denied`, `501`
  `capability-unavailable`.
- Idempotency (Master Prompt §36): requests MAY carry an `Idempotency-Key`
  header. When present, the key maps durably to one attempt per scope:
  replays (including retries after a lost response) return the recorded
  result without a second execution; concurrent same-key requests converge
  on one attempt; reuse of a key for a different Action in the same scope is
  rejected with `409` `idempotency-key-conflict`; the same textual key in a
  different scope is independent. Idempotency of request handling is not
  exactly-once execution (§35) and is distinct from provider-level
  idempotency. Keys longer than 256 characters are rejected as
  `malformed-payload`.

## 5. Verification (Core)

```http
GET /.well-known/rop/actions/{actionId}/verification
```

Response 200:

```json
{
  "actionId": "act_01K...",
  "status": "VERIFIED",
  "semantics": "LOCAL_READONLY",
  "postconditions": [
    { "id": "resource-absent", "description": "created resource no longer exists", "satisfied": true }
  ],
  "evaluatedAt": "2026-08-30T00:06:00Z"
}
```

- `status` is `VERIFIED`, `FAILED`, or `UNKNOWN`; `UNKNOWN` is used when the
  verification's own evaluation could not be performed (§47) and MUST NOT be
  reported as reversal failure (I-10).
- Verification failure MUST be distinguishable from reversal invocation
  failure (§46).
- For IRREVERSIBLE Actions the provider defines no postconditions: `422`
  `irreversible-action`.

## 6. Errors

RFC 9457 `application/problem+json` with stable `type` URIs
(`urn:rop:problem:*`):

```json
{
  "type": "urn:rop:problem:reversal-expired",
  "title": "reversal expired",
  "status": 410,
  "detail": "reversal window for action act_01K... expired at ..."
}
```

Clients MUST NOT parse `detail` to understand semantics.

## 7. Receipt headers on business responses

Business endpoints that create tracked Actions SHOULD include:

```http
ROP-Action-ID: act_01K...
ROP-Reversibility: REVERSIBLE
```

The full receipt is available at the Action status resource. Business
response headers MUST NOT contain private reversal material (I-14).

The Action status document (§2) carries residue where it exists: an
append-style, provider-declared list of remaining effects with their
lifecycle flags (`expected`, `providerDefined`, `manualRemediable`). Residue
is not evidence that reversal failed.

## 8. HTTP status semantics (M6 audit)

Every status used by the binding has a semantic reason; status codes are not
added for completeness, and transport status is never conflated with semantic
outcome (§28).

| Status | Used for | Semantic reason |
|---|---|---|
| 200 | discovery, Action status, plan-reversal, reverse, verification | The request was handled; for `reverse`, the semantic outcome — including `REVERSE_FAILED`, `CONFLICT`, `OUTCOME_UNKNOWN` — is in the body (§4). |
| 201 | business creation endpoints that track Actions (provider-defined surface) | A resource was created; carries receipt headers. |
| 400 | malformed payload, oversized `Idempotency-Key` | The request could not be parsed or violates input bounds. |
| 403 | `authorization-denied` | The principal lacks the verb in this scope; Action ID possession grants nothing (I-2). |
| 404 | `action-not-found` (and business resources) | No such Action *in this scope* — also used for cross-scope requests so existence is never leaked (I-13). |
| 409 | `reversal-conflict`, `reversal-already-in-progress`, `idempotency-key-conflict`, `dependency-exists` | A state or safety conflict: stale plan/CAS, concurrent attempt, key reuse with different semantics, active dependent. |
| 410 | `reversal-expired` | The eligibility window has passed, permanently. |
| 422 | `irreversible-action`, `precondition-failed`, `verification-failed` | The request was syntactically valid but semantically unsatisfiable. |
| 500 | `internal-error` | Implementation fault; MUST NOT be used to express semantic outcomes. |
| 501 | `capability-unavailable` | A known optional capability this provider does not offer (OQ-11; non-retryable). |

Deliberately unused in v0.1, with reasons:

- **202** — the reference reversal executes synchronously; reserved for a
  future asynchronous acceptance model.
- **401** — no authentication scheme is defined in v0.1; authorization
  failures use 403.
- **412** — conditional-request semantics are represented semantically as
  `reversal-conflict` (409), not as HTTP precondition headers.
- **429** — rate limiting is a deployment concern (§55); throttling is
  RETRYABLE and never a semantic outcome.
- **502/503/504** — provider transport failures are never echoed as transport
  errors: they park attempts as `OUTCOME_UNKNOWN` for reconciliation (§34).

## 9. Registration note

## 10. Well-Known URI status (experimental, unregistered)

`/.well-known/rop` is an **experimental, currently unregistered** Well-Known
URI. This specification does NOT imply that `rop` is an IANA-registered
Well-Known URI, and no registry entry exists today (OQ-10). Implementers of
this experimental draft deploy the path as-is. Provisional IANA registration
under RFC 8615 — and review of the `ROP-*` header namespace — is future
release/governance work, to be initiated only once the public specification
has a stable URL. The endpoint is not changed merely to avoid this issue.
