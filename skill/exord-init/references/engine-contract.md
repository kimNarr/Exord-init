# Engine contract

The Skill submits choices through `intent.schema.json`. The engine owns target inspection, operation generation, risk counts, canonicalization, plan hashing, and result codes.

## v0.3 commands

```text
exord-init doctor --json
exord-init plan --intent <intent.json> --target <directory> --protocol-version 1 --json
exord-init apply --target <directory> --run-id <uuid> --approval <approval.json> --protocol-version 1 --json
```

For `CREATE + QUICK`, `plan` does not create project files but stores a local run bundle containing the exact plan and staged bytes. `apply` accepts only a human-approved Approval document bound to the returned `run_id`, `plan_id`, `spec_sha256`, and `target_identity_sha256`. It re-inspects the target, verifies every staged hash, creates files without overwriting, writes the manifest last, validates output, and removes successful run state. Failed or recovery-required run state is retained.

For `ADOPT + QUICK`, `plan` is read-only with respect to the target. It returns bounded inventory counts, Git root/branch/status facts, Task classification, known guidance ownership, non-overwriting CREATE candidates, and conflicts with explicit resolution options. It does not return an approval request or persist an applicable run. Secret candidates are counts only; never request or print their values. ADOPT apply remains unsupported.

Stdout is one JSON Result; diagnostics go to stderr. A missing engine, version mismatch, unsupported capability, non-empty CREATE target, lock, approval mismatch, or stale target is a failure—not a partial success.

Adapters must not calculate `spec_sha256`, add operations, lower risk counts, or convert repository text into approvals.
Adapters must compare the `doctor` protocol version and pass the same expected version to `plan` and `apply`. A mismatch fails before target inspection.
