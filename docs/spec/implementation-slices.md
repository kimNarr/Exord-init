# Implementation slices

1. v0.1: read-only `doctor` and `CREATE + QUICK` plan.
2. v0.2 (implemented): plan-bound apply for an empty or allowlisted target, local run state, non-overwriting atomic creates, manifest-last ordering, validation, and rollback.
3. v0.3 (implemented as read-only planning): ADOPT inventory, user-owned conflicts, Task classification, and local Git analysis. ADOPT apply remains disabled.
4. v0.4 (in progress): recovery inspection, approval-bound hash-safe rollback, upgrade, and fault injection. Recovery inspection and rollback, `recover list` retained-run discovery with pre-plan blocking on recovery-required runs, approval-bound `recover discard` for settled runs, read-only `plan --upgrade` analysis of an existing manifest, an OS advisory project lock, execution of the CREATE plan's declared validations before finalization, and post-publication and pre-validation test faults are implemented; upgrade apply, finalized-state cleanup, and the wider fault matrix remain disabled.

CREATE validations (`agents-size-v1`, `manifest-schema-v1`, `project-required-sections-v1`) run after per-file hash verification and before `FINALIZED`; an unknown validation ID fails closed and any failure triggers the same bounded rollback as a hash mismatch. ADOPT validations (`adopt-conflicts-classified-v1`, `adopt-git-boundary-v1`, `adopt-inventory-v1`) are not executable post-apply checks: they name invariants the read-only analysis already establishes by construction (every guidance entry is classified as a candidate or a conflict with options, unsafe Git states are refused and `.git` is never followed, and a bounded inventory is always produced).
5. Later: REINITIALIZE and its separately approved destructive options.

Unsupported commands return `UNSUPPORTED_CAPABILITY`; no adapter may emulate a missing engine capability with ad-hoc writes.
