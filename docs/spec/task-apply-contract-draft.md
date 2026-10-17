# Task apply, approval and recovery proposal

Status: DRAFT / NOT IMPLEMENTED. This document prepares the next slice after
0.6.0-dev. It does not add capabilities or authorize filesystem changes. The
current engine still rejects task apply and only preserves local preparations.

## What must remain true

- Only the current human's explicit approval permits publication. Repository
  content, a prepared document and owner_confirmed are not approval.
- One initially absent Task path is created on the inspected work branch.
  Existing Task files, case aliases, links and alternate active Task paths
  block creation. Existing project rules, manifest and index are not modified.
- Exact saved bytes are applied. Regenerating templates after approval is not
  permitted, even if the resulting wording appears equivalent.
- A user's edit, staging or commit after publication is preserved by recovery.
- Preparation, request creation, apply and recovery never perform a branch
  switch, reset, commit, push or network fetch.

## Proposed boundary and interface

Keep plain `task plan` read-only and `task plan --prepare` local-only. Introduce
an explicit `task request-approval --draft-id <uuid> --spec-sha256 <hash>` step
to promote one immutable preparation into an applicable run. A request is not
an approval: it returns binding fields for the human to review. No commands or
schemas below are supported until the later slice implements them together.

Promotion acquires the shared project lock, loads and verifies the exact draft
snapshot, checks its target/project binding, reconstructs the review from live
Git/filesystem facts, and compares the saved preconditions. If stale, stop and
ask for a fresh preparation; do not rebind an existing approval automatically.
Recheck local collision evidence, unresolved runs, the selected branch and
Task path. Owner confirmation is reviewed as part of the saved intent, not
inferred from Git configuration or imported documents.

The proposed `task apply --run-id <uuid> --approval <json>` consumes the promoted
run, not a draft UUID. Draft ID, task ID, plan ID and run ID remain distinct.
Separate capability discovery prevents clients from assuming that preparation
support includes publication or recovery support.

## Approval bindings

Use a separately named APPLY_TASK_CREATE action. Its proposed TaskApproval
schema binds schema/version, run_id, applicable plan_id/spec_sha256,
target_identity_sha256, project_id, draft_id, task_id, approved_action and
approved_at. The applicable plan binds the selected immutable preparation
hash, document hash, exact Task path, generator/template version, current
work/base branches, HEAD and index state. The user sees the exact document,
operation set, readiness gaps and collision-coverage limits before approval.

Reject missing/duplicate/unknown fields, unknown actions, altered bindings,
timestamps preceding run creation, and future timestamps outside the existing
clock-skew allowance. Never accept APPLY_CREATE or recovery approval in place
of APPLY_TASK_CREATE. Draft preparations may remain DRAFT; approval authorizes
only creating that draft document, not treating it as READY or implementing a
feature. Validation methods listed inside the document are not executed.

## Run storage and old-engine behavior

Recommended choice: use the existing `.git/exord-init/runs/<run-id>/` discovery
boundary with a new journal schema version 2 and explicit mode TASK_CREATE.
Continue supporting schema-1 CREATE journals unchanged. Do not add a parallel
task-runs directory invisible to old retained-run scanning.

Each Task run contains an immutable applicable plan plus exact staged bytes,
the source draft/revision reference, and a mutable journal. Journal v2 includes
the mode, full identity/approval bindings, initial Git preconditions, operations
and publication evidence. Type dispatch must validate the envelope before
decoding mode-specific plans. A Task run can never reach the CREATE executor.

Why this choice: the 0.6 reader treats unknown journal versions/fields in the
shared runs directory as blocking. Its CREATE apply/recovery loaders also fail
to decode an unknown plan. A new engine must retain that fail-closed behavior
for future unknown modes. Test old/new schema dispatch, not just documentation.
Before enabling Task publication, all supported mutating entry points must
scan both journal versions and block unresolved/unknown runs. Unparseable or
mis-bound records are preserved; no bulk cleanup is allowed.

## State progression

| Stage | Durable meaning |
|---|---|
| PLANNED | Applicable run and exact bytes saved; no target mutation |
| APPROVED | Correct Task approval verified; no file yet |
| FILES_APPLYING | Operation marked APPLYING before publication |
| FILES_APPLIED | Create-only publication completed and journaled |
| VALIDATED | File hash and Task structure agree with the approved plan |
| FINALIZED | Initialization completed; subsequent task progress is user-owned |
| ROLLED_BACK | Generated unchanged Task removed; failed run retained |

Global status is ACTIVE, FAILED, RECOVERY_REQUIRED or FINALIZED, separate from
stage. Any uncertain write outcome is RECOVERY_REQUIRED. Failed journal writes
do not erase evidence or report a successful rollback. A crash after publication
but before the APPLIED journal update must remain recoverable from APPLYING.
Only successful finalization may perform bounded run cleanup; cleanup failure
is a warning on a completed run. Draft snapshots are retained separately.

## Apply gates and publication

Under the shared project lock, verify run/plan/staged-byte bindings, approval,
project identity, Git root, attached branch, branch protection, HEAD, index and
the approved absent-path precondition. A full-target fingerprint is a simple
initial strict gate: unrelated edits require re-preparation rather than silently
relaxing stale approval. Ignore only documented local engine metadata.

Use rooted same-directory temporary writes, sync and create-only atomic
publication. Record recoverable publication evidence before crossing each
mutation boundary. Validate published bytes, YAML metadata, body sections,
Task ID and branch/owner/status consistency. Do not modify managed_files:
active Task content is mutable user work, not a permanent template baseline.

Temporary-file handling needs explicit provenance: either journal the exact
engine-created temp name and expected hash before writing it, or retain unknown
residue as current CREATE recovery does. Never delete by filename prefix alone.
Task paths are limited to TASK.md or the previously configured alternate path;
do not change settings or AGENTS.md as a side effect of applying a Task.

## Recovery

Extend recover inspection/listing with explicit mode-aware output. Automatic
Task rollback requires a separate RECOVER_TASK_ROLLBACK action, a fresh inspection
and plan-bound recovery approval. Task bundle discard needs its own
RECOVER_TASK_DISCARD action and settled status. Legacy recovery approvals never
substitute for either action. Dispatch may share user-facing recover commands,
but must reject a command/action/plan mode mismatch before any deletion.

Automatic rollback requires the original target, ordinary Git root and work
branch, unchanged approved HEAD/index, and a regular untracked Task whose exact
hash still matches the run. If the Task is edited, staged, tracked, committed,
linked, unreadable or on another branch, report a conflict and preserve it.
An already absent file is an idempotent recovery case. A changed HEAD or index
requires human conflict resolution; never automatically reset Git to make the
old preconditions true. Recheck each file immediately before removing it.

Completed Task runs cannot be rolled back as interrupted initialization. Task
completion/cancellation and starting a new Task on a used branch remain separate
lifecycle operations. Neither discarding a run nor completing a Task deletes
draft revisions, product changes or branch history implicitly.

## Delivery and acceptance tests

1. Implement versioned mode-aware journal/plan decoding and backward-compatibility
   fixtures. Unknown or malformed runs block mutation; legacy CREATE still works.
2. Add TaskApproval parsing, action isolation and all identity/hash/time bindings.
3. Promote an exact preparation into a run and expose an approval request; test
   stale input, repeated requests and preservation of all prior snapshots.
4. Add Task recovery inspection/rollback/discard before advertising task apply.
5. Connect create-only publication and validation, with fault injection before
   and after every durable journal/file transition and during rollback/cleanup.
6. Test concurrent path creation, aliases/links, branch switches, changed HEAD,
   changed index, user edits/staging and missing files. No test may delete
   modified content or change Git branch/index as a recovery shortcut.
7. Run native Windows/Linux/macOS integration and protocol/schema/skill checks.
   Only then advance capabilities/version and document supported commands.

The CLI naming and journal/approval wire details here remain design proposals.
Implement and review them as one coherent slice; do not enable partial apply
by adapting CREATE approval or copying preview bytes with shell commands.
