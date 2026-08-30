# Contributing to ROP

ROP is an **experimental protocol / research project**. The v0.1 semantics are
**frozen** for `0.1.0-experimental` (`spec/versioning.md` §4); contributions
should respect that.

## Ground rules

1. Read `AGENTS.md` (working rules) and `docs/capability-model.md` (what Core
   vs optional capabilities are) before proposing changes.
2. Do not propose new protocol semantics — new states, classes, guarantees,
   mandatory capabilities, endpoints, residue taxonomy, or dependency
   semantics — for the v0.1 line. Schema evolution rules are in
   `spec/versioning.md`; compatible additions target a future draft.
3. Correctness over convenience: unknown outcomes stay unknown; conflicts are
   preferred over destructive restoration; history is append-preserving. The
   non-negotiable invariants are in `docs/invariants.md` (I-1..I-26).
4. Open questions are tracked in `docs/open-questions.md` with M6 status
   labels. Proposal-level discussion of a question is welcome; silently
   encoding an answer into implementation is not.
5. Every behavioral change needs a test that would fail without it, and an
   update to the relevant spec document and `docs/task-tracker.md` (with
   concrete evidence — see the tracker's own instructions).

## Validation

```bash
gofmt -w .
go test ./...
go vet ./...
```

All three must be clean. The Core packages must not import HTTP
(`TestCoreImportsNoHTTP` enforces this); fixtures under `spec/fixtures/v0.1/`
are frozen and must not change incompatibly.

## Documentation duties

If your change alters behavior described in `spec/`, `docs/`, or `README.md`,
update those documents in the same change. Documentation and implementation
MUST NOT drift apart (Master Prompt §64).
