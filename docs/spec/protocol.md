# Protocol contract v1

This document is the public implementation-facing protocol contract. Historical planning checkpoints and review inputs are maintained outside the repository.

## Boundary

Adapters create a schema-valid SetupIntent. They cannot supply operations, risk counts, or a trusted hash. The engine inspects the target and exclusively creates the Setup Plan, canonical `spec_sha256`, and Result. An ADOPT plan may include `adopt_analysis` with bounded inventory, Git, Task, known-guidance, and conflict facts; these remain inside the hashed plan spec.

## Determinism

- Canonicalize plan `spec` with RFC 8785 and hash the canonical UTF-8 bytes with SHA-256.
- Sort operations by NFC relative-path UTF-8 bytes, operation kind, then expected hash.
- Sort validations by validation ID, then target path. For CREATE, each declared validation is executed against the published bytes before finalization; an unknown ID fails closed. ADOPT validation entries are assertions the read-only analysis already satisfies, not executable post-apply checks.
- Generate a random project ID once in local project state and reuse it across pre-apply plans; never derive it from the absolute path. Plan and run envelope IDs remain unique per attempt.
- ADOPT is read-only for the target, but when the target has no manifest `project_id` it still initializes local project state: a `project.json` is written under the operating-system user-state directory (never inside the target) so the analysis project ID stays stable across re-planning. A future ADOPT apply will decide whether this ID is promoted into the target manifest or the Git-managed state directory; until then the storage location is intentionally unchanged.
- Adapters reuse the engine hash; they do not recalculate it.

## Output

Stdout contains exactly one Result JSON object. Redacted diagnostics use stderr. Exit codes are stable at the category level; Result `code` carries the specific reason. Every `plan` reports `changed: false`; a successful CREATE apply reports `changed: true`. ADOPT does not produce an applicable run or approval request in the current prototype. Recovery inspection reports `changed: false`; recovery rollback requires an Approval bound to `RECOVER_ROLLBACK` and reports whether target or journal state changed. A recovery failure Result carries no recovery analysis; its next action is to re-run `recover inspect` for the current disposition.

The normative machine-readable files are `schemas/intent.schema.json`, `schemas/plan.schema.json`, `schemas/approval.schema.json`, `schemas/result.schema.json`, `schemas/run.schema.json`, `schemas/recovery.schema.json`, `schemas/retained-runs.schema.json`, and `schemas/manifest.schema.json`.

`recover list` is read-only discovery: it reports every retained run bundle for a target as `{run_id, status, stage, started_at, next_action, blocks_new_plan}` and does not take the project lock. Before persisting a new CREATE run, `plan` performs the same scan; if any retained run has `blocks_new_plan` (a `RECOVERY_REQUIRED` journal, an interrupted mid-apply run, or an unparseable journal) it returns `status: "BLOCKED"` with `code: "RECOVERY_REQUIRED"` and persists nothing. Non-blocking leftovers (`PLANNED`/`APPROVED`, rolled-back `FAILED`, `FINALIZED`) are surfaced as warnings only. Read-only ADOPT planning never blocks but reports the same leftovers as warnings.
