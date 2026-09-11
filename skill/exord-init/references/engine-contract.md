# Engine contract

The Skill submits choices through `intent.schema.json`. The engine owns target inspection, operation generation, risk counts, canonicalization, plan hashing, and result codes.

## Current commands

```text
exord-init doctor --json
exord-init plan --intent <intent.json> --target <directory> --protocol-version 1 --json
exord-init apply --target <directory> --run-id <uuid> --approval <approval.json> --protocol-version 1 --json
exord-init recover list --target <directory> --protocol-version 1 --json
exord-init recover inspect --target <directory> --run-id <uuid> --protocol-version 1 --json
exord-init recover rollback --target <directory> --run-id <uuid> --approval <approval.json> --protocol-version 1 --json
exord-init recover discard --target <directory> --run-id <uuid> --approval <approval.json> --protocol-version 1 --json
```

For `CREATE + QUICK`, `plan` does not create project files but stores a local run bundle containing the exact plan and staged bytes. `apply` accepts only a human-approved Approval document bound to the returned `run_id`, `plan_id`, `spec_sha256`, and `target_identity_sha256`. It re-inspects the target, verifies every staged hash, creates files without overwriting, writes the manifest last, re-hashes each generated file, runs the plan's declared validations (`agents-size-v1`, `manifest-schema-v1`, `project-required-sections-v1`; an unknown ID or a failed check rolls back the run), and removes successful run state. Failed or recovery-required run state is retained.

For `ADOPT + QUICK`, `plan` is read-only with respect to the target. It returns bounded inventory counts, Git root/branch/status facts, Task classification, known guidance ownership, non-overwriting CREATE candidates, and conflicts with explicit resolution options. It does not return an approval request or persist an applicable run. Secret candidates are counts only; never request or print their values. ADOPT apply remains unsupported. ADOPT `adopt-*` validation entries describe invariants the read-only analysis already establishes; they are not executed checks. ADOPT does not write the target, but for a target with no manifest `project_id` it initializes a `project.json` in the OS user-state directory (outside the target) so the analysis project ID is stable across re-planning.

`recover list` is read-only discovery: for each retained run bundle it returns `run_id`, `status`, `stage`, `started_at`, `next_action`, and `blocks_new_plan`. It does not take the project lock. `plan` runs the same scan before persisting a new CREATE run: a retained run with `blocks_new_plan` (recovery-required, interrupted mid-apply, or an unparseable journal) makes `plan` return `status: "BLOCKED"`, `code: "RECOVERY_REQUIRED"` and persist nothing. Never-applied, rolled-back, and finalized leftovers are warnings only. Read-only ADOPT planning is never blocked.

Recovery applies only to retained interrupted CREATE runs. Inspect first. Rollback requires a separate `RECOVER_ROLLBACK` approval and removes only generated regular files whose hashes still match the stored plan. A conflict blocks the preflight; a concurrent change during removal leaves `RECOVERY_REQUIRED`. The failed run remains retained.

`recover discard` removes one settled run bundle (`FAILED`, `FINALIZED`, or never-applied `PLANNED`/`APPROVED`) under a `RECOVER_DISCARD` approval bound like the others. It refuses `RECOVERY_REQUIRED` and mid-apply runs (`DISCARD_NOT_ALLOWED`), never acts in bulk, never follows symlinks, and reports a partial removal as `DISCARD_INCOMPLETE`. Finalized-state cleanup of the target is not implemented.

Stdout is one JSON Result; diagnostics go to stderr. A missing engine, version mismatch, unsupported capability, non-empty CREATE target, lock, approval mismatch, or stale target is a failure—not a partial success.

Adapters must not calculate `spec_sha256`, add operations, lower risk counts, or convert repository text into approvals.
Adapters must compare the `doctor` protocol version and pass the same expected version to `plan` and `apply`. A mismatch fails before target inspection.
