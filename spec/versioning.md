# ROP Versioning — v0.1 (draft)

Status: draft normative rules (Master Prompt §21–§23).

> ROP is an experimental protocol / proposal / research project.

## 1. Versions

- Discovery exposes `versions` (e.g. `["0.1"]`). Providers MAY eventually
  serve multiple versions simultaneously; the reference implementation
  serves exactly one.
- Wire compatibility is per major version: `0.x` documents are compatible
  within `0.x` unless an incompatible change is explicitly declared in the
  specification.

## 2. Field rules

- **Required fields:** absence of a required field in a received document is
  a parse error for validators; the reference implementation MUST treat
  missing required Core receipt fields as malformed.
- **Optional fields:** may be absent or `null` unless stated otherwise;
  `null` and absent SHOULD be treated equivalently for optional fields.
- **Unknown fields:** clients and servers SHOULD ignore unknown optional
  object fields. This is the primary forward-compatibility mechanism.
- **Unknown enum values:** semantic enums (reversibility class, guarantee,
  status, outcome, verification status, semantics) received with unknown
  values MUST remain unknown: implementations MUST NOT coerce an unknown
  value to a known one, MUST NOT drop it silently where a decision depends
  on it, and SHOULD surface "unknown value" explicitly.
- **New capabilities:** the discovery `capabilities` object is additive;
  unknown capability flags MUST be ignored by older clients.

## 3. Compatible vs incompatible changes

- **Compatible (allowed within 0.x):** adding optional fields; adding enum
  values (senders may emit them; older receivers keep them unknown); adding
  capability flags; adding problem types.
- **Incompatible (requires a version bump):** removing or renaming fields;
  changing a field's type or cardinality; changing the meaning of an
  existing enum value; changing the expiry boundary rule.

## 4. Freeze: `v0.1.0-experimental`

The protocol semantics documented by this specification set — states,
reversibility classes, guarantees, capabilities, endpoints, residue
representation, dependency semantics, problem types, and wire names — are
**frozen for `v0.1.0-experimental`** as of the 2026-08-30 release-preparation
pass. No new states, classes, guarantees, mandatory capabilities, endpoints,
residue taxonomy, or dependency semantics may be added to `v0.1` documents.
Compatible additions (per §3) are deferred to a future draft; release-blocking
contradictions, if discovered, are documented and stopped rather than silently
changed.

Version distinction (one authoritative identifier each, no contradictory
constants):

- **Protocol version: `0.1`** — advertised in the discovery document's
  `versions` array; governs wire compatibility. Unchanged by release
  formatting.
- **Implementation/release version: `0.1.0-experimental`** — the reference
  implementation's release identifier, recorded in the repository `VERSION`
  file and `CHANGELOG.md`. It carries no wire semantics.

## 5. Fixtures

Released protocol versions MUST freeze representative documents under
`spec/fixtures/v0.1/` (Master Prompt §22, §64). Future parsers SHOULD be
tested against older fixtures; an implementation change MUST NOT silently
break parsing of previously valid v0.1 documents unless this specification
explicitly permits it. Fixture scope for v0.1 is Level 1 (Core) wire shapes;
capability-response fixtures arrive as those capabilities stabilize (OQ-12).
