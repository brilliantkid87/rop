# AGENTS.md

## Project

ROP — Reversible Operations Protocol

ROP is an experimental protocol for machine-readable reversibility of side-effecting operations.

## Required Reading

Before planning, modifying code, or making architectural decisions, read:

1. ROP — Reversible Operations Protocol.md.
2. `/docs/task-tracker.md`
3. Relevant documents under `/docs/` and `/spec/`.

The Master Prompt is the primary source of project goals, constraints, protocol principles, non-goals, architecture requirements, and success criteria.

Do not begin implementation before understanding it.

If this file conflicts with the Master Prompt, follow the Master Prompt unless a newer explicit project decision documents otherwise.

Do not silently reinterpret or weaken requirements from the Master Prompt.

## Principles

* Correctness over convenience.
* Keep the protocol small.
* Do not turn ROP into a workflow engine.
* Do not claim exactly-once execution.
* Treat unknown outcomes as unknown, not failed.
* Prefer conflicts over unsafe rollback.
* Verification is separate from reversal execution.
* Action IDs are identifiers, not authorization.
* Preserve original Action history.
* Provider-defined semantics must not be guessed by clients.

## Architecture

Maintain a strict distinction between:

* **Operation** — reusable behavior definition.
* **Action** — one concrete execution of an Operation.
* **Reversal Attempt** — one attempt to reverse an Action.

Keep ROP Core transport-neutral.

HTTP-specific behavior belongs in the ROP/HTTP binding.

Before substantial implementation, complete the Architecture Gate required by the Master Prompt.

## Task Tracking

Maintain:

```text
/docs/task-tracker.md
```

Read the tracker before starting work.

Update it whenever meaningful work is:

* started;
* progressed;
* completed;
* blocked;
* changed.

Each task should record:

* task ID;
* description;
* status;
* progress;
* files changed;
* validation;
* evidence;
* blockers or open questions.

Use:

```text
TODO
IN_PROGRESS
BLOCKED
DONE
```

Evidence must be concrete.

Examples:

* file paths;
* test names;
* commands executed;
* command results;
* API responses;
* fixtures;
* migrations;
* relevant logs;
* commit hashes when available.

Never fabricate evidence.

If something was not tested or verified, explicitly say so.

Before ending substantial work, update `/docs/task-tracker.md`.

The tracker should allow another engineer or agent to quickly understand:

* what was done;
* what is in progress;
* what remains;
* what is blocked;
* what evidence supports completed work.

## Development

Use Go.

Before considering implementation work complete, run when applicable:

```bash
gofmt -w .
go test ./...
go vet ./...
```

Record validation results in `/docs/task-tracker.md`.

Do not weaken tests merely to make them pass.

Keep changes focused.

Avoid unrelated refactors.

## Safety

Never blindly restore stale state.

Re-check correctness-critical reversal preconditions at execution time.

Do not expose private reversal material in public Action Receipts.

Do not erase Action or audit history after reversal.

Do not convert uncertain distributed outcomes into false failures.

## Scope

Do not add unless explicitly required:

* workflow DSLs;
* schedulers;
* AI/LLM features;
* dashboards;
* cloud integrations;
* multi-language SDKs;
* batch rollback;
* speculative abstractions unrelated to the current milestone.

## Documentation

Keep `/docs/`, `/spec/`, implementation, tests, and task evidence consistent.

ROP must be described as an:

* experimental protocol;
* proposal;
* research project.

Do not claim ROP is a universal rollback standard or can undo arbitrary side effects.

If implementation behavior changes a protocol assumption, update the relevant documentation.

## Working Rule

Before starting a task:

1. Read the Master Prompt.
2. Read `/docs/task-tracker.md`.
3. Read relevant `/docs/` and `/spec/` files.
4. Identify the current task and constraints.
5. Mark the task `IN_PROGRESS`.
6. Perform the work.
7. Validate the work.
8. Record concrete evidence.
9. Mark it `DONE`, `BLOCKED`, or leave it `IN_PROGRESS` accurately.

Do not mark work `DONE` without evidence.

If the design reveals that ROP duplicates an existing standard, violates a core invariant, depends on impossible guarantees, or cannot provide meaningful interoperability, document the finding instead of forcing implementation forward.
