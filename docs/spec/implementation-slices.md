# Implementation slices

1. v0.1: read-only `doctor` and `CREATE + QUICK` plan.
2. v0.2 (implemented): plan-bound apply for an empty or allowlisted target, local run state, non-overwriting atomic creates, manifest-last ordering, validation, and rollback.
3. v0.3 (implemented as read-only planning): ADOPT inventory, user-owned conflicts, Task classification, and local Git analysis. ADOPT apply remains disabled.
4. v0.4: recovery journal, upgrade, and fault injection.
5. Later: REINITIALIZE and its separately approved destructive options.

Unsupported commands return `UNSUPPORTED_CAPABILITY`; no adapter may emulate a missing engine capability with ad-hoc writes.
