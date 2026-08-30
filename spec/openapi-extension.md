# OpenAPI Extension `x-rop` — v0.1 (draft)

Status: draft experimental extension for static preflight discovery
(Master Prompt §25, §27). Operation metadata describes *capability*; it MUST
NOT be interpreted as proof that every future concrete Action remains
reversible (§25).

> ROP is an experimental protocol / proposal / research project.

## 1. Placement

`x-rop` is an OpenAPI Operation Object extension. It is deliberately small;
workflow orchestration MUST NOT be encoded in OpenAPI (§27).

## 2. Schema

```yaml
x-rop:
  reversibility:
    class: REVERSIBLE|RESTORABLE|COMPENSATABLE|PARTIALLY_COMPENSATABLE|IRREVERSIBLE
    guarantee: EXACT|EVENTUAL|BEST_EFFORT|MANUAL|NONE
    ttl: PT24H          # RFC 3339 duration; REQUIRED when class != IRREVERSIBLE
    reverseOperationId: refundPayment   # Operation that compensates this one
```

Minimal irreversible declaration:

```yaml
x-rop:
  reversibility:
    class: IRREVERSIBLE
```

## 3. Rules

- `class` and `guarantee` use exactly the v0.1 vocabularies (`spec/core.md`
  §3); unknown values in consumed documents remain unknown (spec/versioning.md).
- `ttl` is the *default* reversal eligibility window for Actions of this
  Operation; the concrete Action's `expiresAt` (computed at Action creation,
  OQ-8) is authoritative for eligibility.
- `reverseOperationId` names the compensating Operation; it does not imply
  ROP invokes it (reversal invocation is an optional capability,
  `docs/capability-model.md` §3).
- A machine client MAY use `x-rop` to discover the irreversible boundary
  before execution (§26); ROP does not implement an autonomous runtime (§26).
