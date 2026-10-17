# exord-init

[English](README.md) | [한국어](README.ko.md)

`exord-init` is an early-stage, model-portable Agent Skill and deterministic engine for preparing a software-project foundation before feature implementation begins.

It is designed for both individual vibe-coding projects and teams using OpenAI Codex, Anthropic Claude Code, or Google Gemini CLI. Instead of choosing a framework first, it guides the project through product definition, scope, architecture boundaries, working agreements, source-control policy, validation, and task preparation—then derives the technical setup from those decisions.

The project itself is being developed through a vibe-coding workflow: a human directs product and safety decisions while AI coding agents help research, design, implement, review, and test the system. “Vibe-coded” does not mean unverified—the repository uses explicit contracts, plan-bound approval, automated tests, and human review to keep generated changes accountable.

> Development status: **v0.6.0-dev development prototype**. There is no stable release or supported installer yet. The engine supports safe `CREATE + QUICK` apply, read-only ADOPT/upgrade/Task review, local Task draft preparation, and CREATE recovery. See [changes](CHANGELOG.md) and [version policy](docs/spec/versioning.md).

## Why exord-init?

AI-assisted projects often accumulate framework choices, duplicated instructions, and generated files before the product boundary is understood. Context also disappears when the model or session changes.

`exord-init` aims to provide:

- a product-first setup sequence rather than stack-first scaffolding;
- one portable Agent Skill centered on `SKILL.md`;
- concise shared rules in `AGENTS.md`, with thin product bridges only when needed;
- progressive disclosure so agents read only the relevant project documents;
- explicit task, verification, ownership, and handoff contracts;
- deterministic plans and machine-readable results shared by CLI and future app adapters;
- fail-closed filesystem behavior with no silent overwrite or destructive default.

## Current capability

| Area | Current status |
|---|---|
| `doctor` and protocol handshake | Implemented |
| `CREATE + QUICK` planning | Implemented |
| Plan-bound human approval | Implemented |
| Non-overwriting atomic apply | Implemented |
| Target reinspection, hashes, lock, journal, rollback | Implemented |
| English and Korean generated documents | Implemented |
| Codex, Claude Code, and Gemini CLI document targets | Implemented |
| Read-only `ADOPT + QUICK` inventory and conflict plan | Implemented |
| Recovery inspection and approval-bound rollback | Implemented |
| `recover list` retained-run discovery and pre-plan blocking on recovery-required runs | Implemented |
| Approval-bound `recover discard` for settled (rolled-back, finalized, never-applied) runs | Implemented |
| Declared apply validations (`agents-size`, `manifest-schema`, `project-required-sections`) executed before finalization | Implemented |
| Test-only crash matrix (every apply step and mid-rollback, verified through recovery) | Implemented |
| Read-only `plan --upgrade` analysis for an existing exord-init project | Implemented |
| Upgrade apply and finalized-state cleanup | Planned follow-up work |
| `CUSTOM`, `REINITIALIZE`, and ADOPT apply | Not implemented |
| Task and Git analysis | Implemented for ADOPT; generation remains planned |
| `task:plan` / `task plan` | Read-only TaskIntent, readiness and work-branch review; task generation/apply remain unsupported |
| `task:prepare` / `task plan --prepare` | Stable Task ID and exact bilingual document preview in local Git metadata; no TASK.md publication or apply |
| Commit, branch, remote, and push automation | Not implemented |
| Product-specific plugin/extension packages | Not released |

Unsupported capabilities fail explicitly. An adapter must never emulate missing engine behavior with ad-hoc filesystem commands.

## How it works

```text
Human and Agent Skill
  -> versioned SetupIntent
  -> deterministic engine inspection
  -> reviewable Setup Plan + spec_sha256
  -> explicit human approval bound to that exact plan
  -> target reinspection and staged-file verification
  -> non-overwriting atomic apply
  -> validation, manifest-last finalization, or safe rollback
```

The conversational Skill gathers choices and explains trade-offs. The Go engine exclusively owns target inspection, operation generation, risk counts, canonical hashing, filesystem changes, journaling, and result codes.

Repository text, imported documents, issue or PR content, and model output are untrusted data. Only the current human user's explicit response can authorize an apply operation.

## Requirements

- Go 1.25 or newer to build the engine
- Python 3.10+ only for the optional schema test suite
- An existing target directory for the current `CREATE` flow

Go 1.25 is required because safe apply uses rooted, traversal-resistant filesystem operations and create-only hard-link publication on a supporting local filesystem (such as NTFS, APFS, or ext4).

## Build from source

Clone the repository and build the CLI:

```sh
git clone https://github.com/kimNarr/Exord-init.git
cd Exord-init
mkdir -p bin
go build -trimpath -o ./bin/exord-init ./engine/cmd/exord-init
./bin/exord-init doctor --json
```

PowerShell:

```powershell
git clone https://github.com/kimNarr/Exord-init.git
Set-Location Exord-init
New-Item -ItemType Directory -Force bin | Out-Null
go build -trimpath -o .\bin\exord-init.exe .\engine\cmd\exord-init
.\bin\exord-init.exe doctor --json
```

`doctor` should report protocol version `1` and the `plan:create-quick`, `apply:create-quick`, `plan:adopt-quick`, `plan:upgrade`, `recover:inspect`, `recover:rollback`, `recover:list`, and `recover:discard` capabilities.

## Quick start: `CREATE + QUICK`

### 1. Prepare a target

The directory must already exist and may contain only documented harmless entries such as `.git`, `.DS_Store`, `Thumbs.db`, or `desktop.ini`. A filesystem root, home directory, symlink/junction target, or directory containing user-owned files is rejected. For repositories pre-populated with `README.md`, `LICENSE`, or `.gitignore`, initialize in a clean folder or use `ADOPT` mode.

```sh
mkdir ../my-project
```

To use the project as a Git repository, run `git init` in the target **before** `plan`. The plan records the target fingerprint, so creating `.git` after planning changes it and apply fails closed with `PLAN_STALE`. Without `.git`, the manifest is generated with `git_mode: DOCUMENT_ONLY`.

### 2. Create a SetupIntent

Save the intent outside the target directory, for example as `intent.json`:

```json
{
  "schema_version": 1,
  "mode": "CREATE",
  "depth": "QUICK",
  "project_summary": "A small project that helps a team track release readiness",
  "documentation_language": "en",
  "supported_agents": ["codex", "claude", "gemini"]
}
```

`documentation_language` accepts `en` or `ko`. `supported_agents` accepts one or more of `codex`, `claude`, and `gemini`.

### 3. Generate and review the plan

```sh
./bin/exord-init plan \
  --intent ./intent.json \
  --target ../my-project \
  --protocol-version 1 \
  --json
```

The command does not create project files. It returns one JSON Result containing:

- `run_id`, `plan_id`, and canonical `spec_sha256`;
- the exact ordered operations and expected file hashes;
- target identity and fingerprint;
- validation and risk summaries;
- an `approval_request` object.

The exact plan and staged bytes are retained in local run state. For a normal Git directory this state is under `.git/exord-init/`; for a non-Git target it is stored in the operating system's user-state directory.

### 4. Approve the exact plan

Review the entire plan first. Copy only the returned `approval_request` object into `approval.json` and add the current UTC time as `approved_at`:

```json
{
  "schema_version": 1,
  "run_id": "<run_id from plan>",
  "plan_id": "<plan_id from plan>",
  "spec_sha256": "<spec_sha256 from plan>",
  "target_identity_sha256": "<target identity from plan>",
  "approved_action": "APPLY_CREATE",
  "approved_at": "2026-09-09T03:00:00Z"
}
```

There is intentionally no broad `--yes` flag. Approval is valid only for the exact run, plan, spec hash, target identity, action, and a valid approval timestamp. If the target or plan changes, apply fails closed.

### 5. Apply

```sh
./bin/exord-init apply \
  --target ../my-project \
  --run-id <run_id> \
  --approval ./approval.json \
  --protocol-version 1 \
  --json
```

On success, the engine re-hashes every generated file, runs the plan's declared validations (`agents-size-v1`, `manifest-schema-v1`, `project-required-sections-v1`), and only then removes the finalized run bundle. On failure, including a failed validation, it rolls back only files created by that run whose hashes are still unchanged. Incomplete recovery state is retained for inspection.

## Read-only existing-project analysis: `ADOPT + QUICK`

Set `mode` to `ADOPT` in the SetupIntent and run the same `plan` command against an existing project. The current prototype will not modify or stage files for apply. It reports:

- bounded inventory counts without exposing the full project file list;
- content fingerprints using NFC-normalized paths, with explicit hashing and scan limits;
- local Git root, branch, HEAD, dirty state, untracked count, worktree marker, and in-progress operation;
- existing `TASK.md` classification and `.exord/TASK.md` as the default conflict alternative;
- ownership status for known guidance files;
- CREATE-only candidates for missing files and resolution options for every conflict;
- secret-candidate counts without printing detected values.

The scanner does not follow `.git`, dependency caches, symlinks, or nested repositories. Git inspection disables optional locks and external fsmonitor and performs no network operation. If a secret candidate is found or scanning reaches a safety limit, commit proposals must stop pending human review.

## Interrupted-run recovery

List every retained run bundle for a target:

```sh
./bin/exord-init recover list --target ../my-project --json
```

Each entry reports `run_id`, `status`, `stage`, `started_at`, and a single safe `next_action`. `plan` runs the same scan before persisting a new CREATE run and returns `BLOCKED` (persisting nothing) if any retained run is recovery-required, interrupted mid-apply, or unreadable. Never-applied plans, rolled-back runs, and finalized bundles are reported as warnings only.

Inspect a retained run before changing anything:

```sh
./bin/exord-init recover inspect --target ../my-project --run-id <run_id> --json
```

The engine verifies the stored plan hash, journal binding, target identity, operation state, and current file hashes. `ROLLBACK_READY` means only files whose bytes still match the approved generated hashes are eligible for removal. A modified, unreadable, linked, or type-conflicting path blocks the entire automatic rollback before it starts.

Rollback requires a separate approval document bound to the same run, plan, spec hash, target identity, and the exact `RECOVER_ROLLBACK` action:

```sh
./bin/exord-init recover rollback \
  --target ../my-project \
  --run-id <run_id> \
  --approval ./recovery-approval.json \
  --json
```

Recovery rechecks each file immediately before removal and journals every step. It deliberately leaves empty directories and retains the failed run bundle for later explicit disposition.

Once a run is settled — rolled back (`FAILED`), applied (`FINALIZED`), or never applied (`PLANNED`/`APPROVED`) — `recover discard` permanently removes its bundle. It requires an approval bound to the same run, plan, spec hash, and target with the exact `RECOVER_DISCARD` action, refuses any `RECOVERY_REQUIRED` or interrupted mid-apply run, and reports whether the bundle was actually removed:

```sh
./bin/exord-init recover discard \
  --target ../my-project \
  --run-id <run_id> \
  --approval ./discard-approval.json \
  --json
```

Finalized-state cleanup of the target itself is not implemented in this slice.

## Read-only Task preparation: `task plan`

After creating a project foundation and preparing a local work branch, copy
[`intent-task-review-ko.json`](tests/fixtures/intent-task-review-ko.json) and
edit it for your task, work/base branches and language (`en` or `ko`).

```sh
./bin/exord-init task plan --intent ./task-intent.json --target ../my-project --json
```

This command requires an exord-init manifest with Git enabled and a supported
task path. It reviews the requested branch, local base branch, existing Task,
human owner confirmation, scope, acceptance criteria and verification methods.
It never runs the proposed verification commands. Optional manifest
`settings.integration_branches` extends the protected main/dev/master names.

The separate `data.task_plan` contract returns `DRAFT`, `READY_FOR_REVIEW` or
`BLOCKED` with machine-readable gaps/blockers. A successful inspection returns
exit code 0 even if blocked: inspect the disposition, not just the exit code.
READY_FOR_REVIEW is input/branch readiness, not approval or product readiness.
An owner confirmation boolean records the user's response; it cannot verify a
person's identity. Validation stays `NOT_RUN`.

No Task ID, files, staged bytes, approval request or local run state is created.
`task apply`, worktree support and Task updates remain unsupported. Repeated
review of unchanged input/state has the same spec hash and distinct plan IDs.
The review hash is not a token accepted by CREATE apply or recovery.

### Preserve a draft and exact document plan

```sh
./bin/exord-init task plan --prepare --intent ./task-intent.json --target ../my-project --json
# Explicit reuse after a work-branch rename:
./bin/exord-init task plan --prepare --draft-id <draft_uuid> --intent ./task-intent.json --target ../my-project --json
```

The explicit `--prepare` flag takes the project lock and writes only local
`.git/exord-init/task-drafts/` metadata. `data.task_preparation` contains a stable
draft UUID, `T-YYYYMMDD-XXXXXXXX` task ID, UTC creation time, exact UTF-8/LF
document, one CREATE operation and hashes. `changed` describes project output
(false); `data.local_state_changed` reports the metadata write. No approval
request, CREATE run, tracked file or Git index change is made.

The default draft is selected by project and original work branch. Repeat on
that branch to reuse its identity, or pass the returned `draft_id` after a
branch rename. The immutable identity records the original branch; each
document/plan revision records the inspected branch. Identical plans reuse the
saved revision; changed input/state preserves previous hash-named snapshots
containing both plan and document. Preview `updated_at` uses draft creation day.
New tasks on a previously used branch, draft disposal and date updates after
actual task progress are not automated in this slice.

Collision checks read other local branch names (up to 257), commit messages and
Task patches (257 entries per pass, 8 MiB/5 seconds per Git query), up to 32
worktree Task files, and 256 local draft identities. `collision_scan.limits`
discloses partial coverage; history counts include entries from both passes,
not unique commits. No fetch runs and remote-wide uniqueness is not guaranteed.
Corrupt/conflicting local records stop preparation instead of being replaced.
A failure may retain identity or temporary metadata; task-state cleanup is not
automated. `recover list/discard` still handle CREATE runs only. Task apply and
recovery remain unsupported; do not copy previews into the target as a substitute.

## Read-only upgrade analysis: `plan --upgrade`

For a directory that already has a valid `.exord/manifest.json`, `plan --upgrade` reports what a future upgrade would do, without touching the project or requesting an approval:

```sh
./bin/exord-init plan --upgrade --target ../my-project --json
```

It reads the manifest, fails closed if the manifest `schema_version` is not understood, compares each managed file's current bytes (CRLF-normalized) to its recorded baseline and its template version to the current generator's, and classifies every file:

- `UP_TO_DATE` — baseline matches and the template is current;
- `TEMPLATE_UPDATE_AVAILABLE` — an unmodified `REPLACE_IF_UNMODIFIED` file with a newer template;
- `PROPOSE_ONLY_UPDATE` — a newer template for a `PROPOSE_ONLY` file, needs per-file approval;
- `REVIEW_LOCAL_EDIT` — the file was changed locally;
- `RECREATE_MISSING` — a managed file was deleted;
- `NEW_MANAGED_FILE` / `ORPHAN_MANAGED_FILE` — the supported-agent set changed;
- `BLOCKED` — a symlinked, type-conflicting, or unreadable managed file, an unknown `hash_mode`, or a generator downgrade.

The overall `disposition` is `UP_TO_DATE`, `UPGRADE_AVAILABLE`, `REVIEW_REQUIRED`, or `BLOCKED`. Upgrade apply is not implemented in the current slice.

## Generated project files

The current QUICK flow creates only the permanent minimum and requested bridges:

```text
.exord/manifest.json   Managed-file baselines and shared generator settings
AGENTS.md              Concise model-portable project rules
docs/PROJECT.md        Product summary and discovery placeholders
CLAUDE.md              Thin Claude Code bridge, when requested
GEMINI.md              Thin Gemini CLI bridge, when requested
```

`CLAUDE.md` and `GEMINI.md` point back to the canonical `AGENTS.md`; they do not duplicate all project rules. Architecture-layer documents and `TASK.md` are conditional future outputs and are not generated by the current QUICK flow.

## Safety guarantees in the current prototype

- QUICK reduces questions, never safety checks.
- Existing user files are never overwritten.
- Target state is inspected again immediately before apply.
- Plan, approval, target identity, and staged content hashes must all match.
- Staging and target access use rooted filesystem handles and reject linked path components.
- Files are published atomically without replace semantics; the manifest is written last.
- Rollback removes only unchanged files created by the current run.
- Failed and recovery-required run state is preserved.
- The project run lock is an OS advisory lock; a crashed run cannot leave the project permanently locked, and a leftover `lock.json` after a finalized run is a warning, not a recovery-required state.
- The engine never commits, pushes, changes branches, deletes Git history, or sends telemetry.
- `REINITIALIZE` and permanent deletion remain disabled.
- ADOPT is analysis-only and cannot be applied in the current prototype.
- Recovery rollback removes only journaled CREATE outputs whose current SHA-256 still matches the stored plan; conflicts block mutation.
- Recovery approval is independent from the original CREATE approval and is bound to `RECOVER_ROLLBACK`; `recover discard` needs its own `RECOVER_DISCARD` approval and only acts on settled runs.

See [the safety contract](docs/spec/safety.md) and [protocol contract](docs/spec/protocol.md) for the public implementation boundaries.

## Agent Skill source

The common Skill source is located at [`skill/exord-init/`](skill/exord-init/):

```text
skill/exord-init/
├── SKILL.md
├── agents/openai.yaml
├── assets/templates/{en,ko}/
└── references/
```

The project intends to generate Codex plugin, Claude Code plugin, and Gemini CLI extension packages from this common source. Product-specific installers and marketplace releases have not been finalized, so the repository does not yet claim a stable installation command.

## Repository layout

```text
engine/cmd/exord-init/    CLI entry point
engine/internal/          Planner, apply, state, hashing, and target safety
schemas/                  Versioned JSON contracts
skill/exord-init/         Installable Agent Skill source
docs/spec/                Public protocol, safety, and implementation scope
tests/                    Cross-contract and binary schema tests
templates.go              Embedded template loader
```

Historical planning checkpoints, feedback inputs, and generated PDFs are intentionally maintained locally rather than committed to the public repository.

## Testing

Go tests and static analysis:

```sh
go test ./...
go vet ./...
```

Optional Python schema tests:

```sh
python -m pip install -r requirements-dev.txt
python -m unittest discover -s tests -p '*_test.py'
```

Set `EXORD_INIT_BIN` to a built executable to include binary CREATE apply, read-only ADOPT, and approved recovery integration tests:

```sh
EXORD_INIT_BIN=./bin/exord-init python -m unittest discover -s tests -p '*_test.py'
```

Local native execution has been exercised on Windows amd64. Cross-compilation succeeds for windows/amd64, windows/arm64, linux/amd64, and darwin/arm64. A GitHub Actions matrix (`.github/workflows/ci.yml`) runs `go build`, `go vet`, `go test`, a trimpath CLI build, and the Python contract/schema/integration suite on linux amd64, linux arm64, windows amd64, and macOS arm64. Release-artifact signing and packaging remain future work.

## Roadmap

The planned risk-ordered implementation sequence is:

1. v0.1: read-only `doctor` and `CREATE + QUICK` planning — implemented.
2. v0.2: safe `CREATE + QUICK` plan-bound apply — implemented.
3. v0.3: read-only `ADOPT` inventory, conflict analysis, Task and Git analysis — implemented.
4. v0.4: recovery commands, upgrade behavior, and fault injection — recovery inspection, approved rollback, `recover list` discovery with pre-plan blocking, approval-bound `recover discard` for settled runs, read-only `plan --upgrade` analysis, declared apply-validation execution, and the test-only apply crash matrix are implemented; upgrade apply and finalized-state cleanup are pending.
5. Later: `REINITIALIZE` with verified external backup and separately approved destructive options.

This roadmap describes implementation order, not a release commitment.

## License

Engine, Skill, schemas, tests, and project documentation are licensed under [Apache License 2.0](LICENSE).

Template sources under `skill/exord-init/assets/templates/` and their substantial generated outputs are dedicated under [CC0 1.0](LICENSE.templates). Existing or unrelated user content is never relicensed.

Third-party dependency notices are listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## Contributing

The project is still defining its public contribution and release process. Until those policies are published, please open an issue before proposing a large change. Safety-contract changes should include tests and must not weaken plan binding, target reinspection, overwrite prevention, or recovery behavior.
