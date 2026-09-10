# Recovery

Use this workflow only for a retained CREATE run. Recovery is not an ADOPT or REINITIALIZE substitute.

Run `recover list --target <dir>` first to enumerate retained bundles and their safe next action. A `plan` that returns `BLOCKED` names the runs that must be resolved first.

1. Run `recover inspect` with the exact target and run ID.
2. Verify the returned run, plan, spec hash, target identity, disposition, and every operation.
3. If disposition is `CONFLICT`, preserve the target and ask the user to resolve the listed paths. Do not retry rollback automatically.
4. If disposition is `ROLLBACK_READY`, explain that empty directories and the failed run bundle will remain.
5. Obtain a new Approval whose `approved_action` is exactly `RECOVER_ROLLBACK` and whose bindings match the inspection result.
6. Invoke `recover rollback` once. On failure, inspect again rather than looping.

Rollback removes only journaled CREATE files that remain regular files with the stored SHA-256. It never deletes modified, unreadable, linked, or type-conflicting paths. The original `APPLY_CREATE` approval cannot authorize recovery. `recover list` discovery is available; discard, cleanup, resume, upgrade, and directory removal are not implemented in the current slice.
