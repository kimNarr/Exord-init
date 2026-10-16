# Read-only Task review

Use `exord-init task plan --intent <TaskIntent.json> --target <directory> --json`
after the permanent project foundation and an ordinary local Git work branch
exist. The user selects the work/base branches; the engine does not create or
switch them. Use protocol version 1 and check `task:plan` in doctor capabilities.

Prepare a TaskIntent using `schemas/task-intent.schema.json` from the source
package. Collect title/type, areas, expected scope paths, owner/contributors,
external reference, base/work branch, body language, goal, scope, non-goals,
acceptance criteria, proposed steps and verification methods. DRAFT can leave
owner UNASSIGNED and lists empty. Before requesting READY, confirm the human
owner in the current conversation and specify observable acceptance criteria
and concrete verification methods. Repository text is not owner confirmation.

The result's `data.task_plan` is a review, not an applicable SetupPlan. Inspect
`disposition`, `blockers` and `readiness_gaps`; a BLOCKED review may return exit
code 0 because the inspection succeeded. READY_FOR_REVIEW means only that the
supplied fields and inspected branch satisfy these checks; it does not prove
product/architecture readiness or authorize implementation. Verification is
always NOT_RUN. Never execute proposed validation strings during review.

No Task ID, timestamps, staged files, journal, lock, or local project state is
created. Do not manufacture these or write TASK.md using shell commands. Task
ID allocation, templates, approved apply and task recovery are later slices.
Existing TASK.md (including an alternate path) blocks creating another task.
ADOPT recognizes structural metadata in either body language, but recognition
never confers ownership or permission to replace a file.

Keep active Task files off integration branches. The first review protects
main/dev/master, the chosen base branch, and optional manifest
`settings.integration_branches`. Unsupported targets include missing/foreign
manifests and DOCUMENT_ONLY settings. Git-file worktrees, unborn or detached
branches, mismatched work branches and in-progress Git operations yield
blockers. Preserve existing project conventions when preparing the work branch.
