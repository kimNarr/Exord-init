# Changelog

## 0.5.0-dev — 2026-10-16

- Added read-only `task plan`, TaskIntent and TaskReviewPlan schemas, branch
  blockers and owner/scope/acceptance/verification readiness checks.
- Recognize English and Korean Task documents through bounded structural YAML
  metadata; reject duplicate keys, invalid types, aliases and unknown versions.
- Added file/Git-state invariance and deterministic review-hash integration tests.
- Added `go.yaml.in/yaml/v3 v3.0.4` with third-party license notices.
- Unified CLI, generated manifest and upgrade comparison version sources.
- Expanded apply crash/recovery coverage and redacted filesystem diagnostics.

Task ID allocation, document generation, task apply/recovery, upgrade apply,
ADOPT apply, finalized-state cleanup, CUSTOM and REINITIALIZE remain unsupported.
This development source checkpoint does not publish binaries or installers.

## Earlier development slices

- v0.4: recovery inspect/rollback/list/discard, declared validations, OS advisory
  locks, CI and read-only upgrade analysis. Upgrade apply and cleanup remain open.
- v0.3: read-only ADOPT inventory, Task/Git analysis and conflict classification.
- v0.2: plan-bound CREATE + QUICK apply, manifests, hashes and bounded rollback.
- v0.1: read-only doctor and CREATE + QUICK planning foundation.

These describe implementation history; earlier slices did not have release tags.
