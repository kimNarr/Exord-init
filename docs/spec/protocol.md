# Protocol contract v1

This document is the public implementation-facing protocol contract. Historical planning checkpoints and review inputs are maintained outside the repository.

## Boundary

Adapters create a schema-valid SetupIntent. They cannot supply operations, risk counts, or a trusted hash. The engine inspects the target and exclusively creates the Setup Plan, canonical `spec_sha256`, and Result. An ADOPT plan may include `adopt_analysis` with bounded inventory, Git, Task, known-guidance, and conflict facts; these remain inside the hashed plan spec.

## Determinism

- Canonicalize plan `spec` with RFC 8785 and hash the canonical UTF-8 bytes with SHA-256.
- Sort operations by NFC relative-path UTF-8 bytes, operation kind, then expected hash.
- Sort validations by validation ID, then target path.
- Generate a random project ID once in local project state and reuse it across pre-apply plans; never derive it from the absolute path. Plan and run envelope IDs remain unique per attempt.
- Adapters reuse the engine hash; they do not recalculate it.

## Output

Stdout contains exactly one Result JSON object. Redacted diagnostics use stderr. Exit codes are stable at the category level; Result `code` carries the specific reason. Every `plan` reports `changed: false`; a successful CREATE apply reports `changed: true`. v0.3 ADOPT does not produce an applicable run or approval request.

The normative machine-readable files are `schemas/intent.schema.json`, `schemas/plan.schema.json`, `schemas/approval.schema.json`, `schemas/result.schema.json`, `schemas/run.schema.json`, and `schemas/manifest.schema.json`.
