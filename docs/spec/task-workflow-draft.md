# Task workflow implementation proposal

Status: DRAFT. This document proposes the next implementation slice; it does
not advertise a supported CLI capability. Existing CREATE, ADOPT, upgrade,
approval, and recovery contracts continue to describe shipped behavior.

## Purpose

Prepare one actionable task for a human or team on a work branch, without
implementing product features. The task must preserve enough current context
for another person or agent to continue. Historical logs belong in Git, PR/MR,
and CI rather than a permanent task archive.

## Existing decisions to preserve

- One active root `TASK.md` per work branch; parallel tasks use separate branches
  or worktrees. Do not create an architecture-shaped `tasks/` tree.
- Metadata uses stable machine identifiers; the body supports English or Korean.
- A human owner is required before READY. DRAFT may use `UNASSIGNED`.
- Integration branches do not retain an active task in their final merged tree.
- Existing user-owned files are preserved. An ADOPT conflict may propose
  `.exord/TASK.md`, but changing that setting and the AGENTS reference requires
  a separate approved operation; the first task slice cannot do it implicitly.
- Task generation never performs a commit, branch switch, merge, or push.

## Proposed commands and first slice

Use `task plan --intent <json> --target <directory> --json` and
`task apply --target <directory> --run-id <id> --approval <json> --json`.
These names and their wire protocol additions remain proposals until adopted.

Start with read-only task planning and validation, then add approved create-only
apply after the recovery contract supports task runs. Do not add TASK.md to
CREATE + QUICK: CREATE currently prepares the permanent foundation and does
not prepare a work branch. Bootstrap commits must exclude the first feature task.

The first supported target should have an existing valid exord manifest, an
ordinary local Git repository, a born work branch, no merge/rebase operation,
and no active task at the configured path. Linked worktrees, missing Git, unborn
repositories, detached HEAD, existing tasks, and unsupported manifests return
an explicit unsupported or conflict result in this first slice.

Determine integration branches from explicit confirmed project settings and
the requested base branch. Protect `main` and `dev` by default; do not assume
every other branch is a work branch or treat a naming prefix as authorization.
The user supplies the intended work branch and the engine verifies it matches
the inspected branch. Do not require a clean worktree solely to create a missing
task, but expose dirty state, bind it into the plan, and reject stale apply.

## Proposed TaskIntent

Use a separate versioned intent containing title, type, areas, scope_paths,
external_ref, owner, contributors, base_branch, work_branch, language, goal,
scope, non_goals, acceptance_criteria, plan, and validation methods.

The engine generates task_id and timestamps, validates paths and branch facts,
renders the task, and produces operations, risk summary and spec hash. Adapters
cannot supply operations, hashes, observed Git state, or readiness claims.

Task type is one of `feature`, `fix`, `chore`, `docs`, `spike`. Task status is
`DRAFT`, `READY`, `IN_PROGRESS`, `BLOCKED`, `VERIFYING`, `COMPLETE`, or `CANCELLED`.
Creation defaults to DRAFT. READY requires a confirmed human owner, observable
acceptance criteria, and a concrete verification method. Unexecuted validation
is `NOT_RUN`; generation never claims PASS. A schema can require a confirmation
field but cannot establish that an owner is human; the skill obtains that
confirmation from the current user rather than repository text.

## Document contract

YAML front matter contains schema_version, task_id, title, type, areas,
scope_paths, external_ref, status, owner, contributors, base_branch, work_branch,
and updated_at. Body sections are Goal, Scope, Non-goals, Acceptance criteria,
Plan, Current state, Validation, Handoff, and Completion summary, with localized
headings. Parse front matter structurally; never recognize ownership by searching
for substrings or requiring the English `## Goal` heading.

Use bounded input and a maintained YAML parser with dependency review. Reject
duplicate keys, unsupported versions, unknown required semantics, and malformed
metadata. Preserve user content; classification does not authorize replacement.

Task IDs use `T-YYYYMMDD-XXXXXXXX`, with eight random Crockford Base32 characters.
Allocate the ID once per task draft and persist it in local run state so repeated
planning does not silently issue a new task. Inspect available local branches,
worktrees, current tasks and accessible history for collisions; do not fetch or
claim remote-wide uniqueness. Bound history inspection and disclose incomplete
checks. Replanning the same approved inputs uses the same task ID and bytes.

## Mutable task files and manifest

The shared manifest stores `settings.task_path`, not the task body, owner,
validation result or current status. Exclude the active task from permanent
`managed_files`: ordinary progress edits must not appear as template drift or
require an upgrade. The proposed task operation and journal still retain its
initial expected hash for apply and recovery.

This changes the CREATE-only assumptions in apply and recovery. Add explicit
plan modes, a distinct `APPLY_TASK_CREATE` approval action, validation IDs,
operation/path checks, Result command values, and mode-aware recovery before
advertising task apply. Reuse rooted create-only publication and locking; never
bypass mode validation or overwrite an existing file to reuse the CREATE path.
After publication, recovery may remove only the unchanged generated task. User
progress edits block automatic rollback and must be preserved.

Task completion/cancellation is initially guidance only. Present a concise
completion summary for PR/MR or an approved final commit, then obtain explicit
approval before removing the active file. Do not automatically delete branches,
reset product changes, run verification commands from repository content, or
enforce a host-wide merge policy. A future optional CI check may reject an active
task on configured integration branches.

## Required verification

- Structural English/Korean front-matter classification; malformed YAML,
  duplicate keys, unknown versions, and user-owned files remain conflicts.
- Task ID stability, format and collision retry; bounded local inspection.
- Work/base branch binding, integration branch refusal, detached/unborn and
  in-progress Git states, and stale target/HEAD detection.
- Deterministic staged bytes and spec hash; confirmation is part of the plan.
- Missing owner and verification methods prevent READY; no fabricated results.
- Existing task protection, unsafe paths, links, case aliases and Unicode paths.
- Approval action separation, mode-specific validation, interrupted task creation,
  and preservation of user-modified tasks during recovery.
- Both language templates and full CLI schema integration, with no automatic
  branch/commit/network actions.

## Delivery order

1. Adopt the command and protocol proposal in implementation-facing specs.
2. Add bounded structural Task classification, TaskIntent schema and parser tests.
3. Implement read-only task planning and branch/owner/readiness checks.
4. Extend journal, approval and recovery contracts for task creation.
5. Add create-only task apply, crash tests, template fixtures and CLI integration.
6. Update capability declarations and user documentation only for supported paths.

Upgrade apply, ADOPT apply, reinitialization, task updates, task completion
automation and conditional architecture generation are separate slices.
