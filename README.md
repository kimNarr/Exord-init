# exord-init

[English](README.md) | [한국어](README.ko.md)

`exord-init` is an early-stage, model-portable Agent Skill and deterministic engine for preparing a software-project foundation before feature implementation begins.

It is designed for both individual vibe-coding projects and teams using OpenAI Codex, Anthropic Claude Code, or Google Gemini CLI. Instead of choosing a framework first, it guides the project through product definition, scope, architecture boundaries, working agreements, source-control policy, validation, and task preparation—then derives the technical setup from those decisions.

The project itself is being developed through a vibe-coding workflow: a human directs product and safety decisions while AI coding agents help research, design, implement, review, and test the system. “Vibe-coded” does not mean unverified—the repository uses explicit contracts, plan-bound approval, automated tests, and human review to keep generated changes accountable.

> Development status: **v0.3 prototype**. There is no stable release or supported installer yet. The engine supports safe `CREATE + QUICK` apply and read-only `ADOPT + QUICK` analysis.

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

| Area | v0.3 status |
|---|---|
| `doctor` and protocol handshake | Implemented |
| `CREATE + QUICK` planning | Implemented |
| Plan-bound human approval | Implemented |
| Non-overwriting atomic apply | Implemented |
| Target reinspection, hashes, lock, journal, rollback | Implemented |
| English and Korean generated documents | Implemented |
| Codex, Claude Code, and Gemini CLI document targets | Implemented |
| Read-only `ADOPT + QUICK` inventory and conflict plan | Implemented |
| `CUSTOM`, `REINITIALIZE`, and ADOPT apply | Not implemented |
| Task and Git analysis | Implemented for ADOPT; generation remains planned |
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

Go 1.25 is required because safe apply uses rooted, traversal-resistant filesystem operations and create-only hard-link publication.

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

`doctor` should report protocol version `1` and the `plan:create-quick`, `apply:create-quick`, and `plan:adopt-quick` capabilities.

## Quick start: `CREATE + QUICK`

### 1. Prepare a target

The directory must already exist and may contain only documented harmless entries such as `.git`, `.DS_Store`, `Thumbs.db`, or `desktop.ini`. A filesystem root, home directory, symlink/junction target, or directory containing user-owned files is rejected.

```sh
mkdir ../my-project
```

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

On success, the engine validates every generated file and removes the finalized run bundle. On failure, it rolls back only files created by that run whose hashes are still unchanged. Incomplete recovery state is retained for inspection.

## Read-only existing-project analysis: `ADOPT + QUICK`

Set `mode` to `ADOPT` in the SetupIntent and run the same `plan` command against an existing project. v0.3 will not modify or stage files for apply. It reports:

- bounded inventory counts without exposing the full project file list;
- content fingerprints using NFC-normalized paths, with explicit hashing and scan limits;
- local Git root, branch, HEAD, dirty state, untracked count, worktree marker, and in-progress operation;
- existing `TASK.md` classification and `.exord/TASK.md` as the default conflict alternative;
- ownership status for known guidance files;
- CREATE-only candidates for missing files and resolution options for every conflict;
- secret-candidate counts without printing detected values.

The scanner does not follow `.git`, dependency caches, symlinks, or nested repositories. Git inspection disables optional locks and external fsmonitor and performs no network operation. If a secret candidate is found or scanning reaches a safety limit, commit proposals must stop pending human review.

## Generated project files

The current QUICK flow creates only the permanent minimum and requested bridges:

```text
.exord/manifest.json   Managed-file baselines and shared generator settings
AGENTS.md              Concise model-portable project rules
docs/PROJECT.md        Product summary and discovery placeholders
CLAUDE.md              Thin Claude Code bridge, when requested
GEMINI.md              Thin Gemini CLI bridge, when requested
```

`CLAUDE.md` and `GEMINI.md` point back to the canonical `AGENTS.md`; they do not duplicate all project rules. Architecture-layer documents and `TASK.md` are conditional future outputs and are not generated by the current v0.3 QUICK flow.

## Safety guarantees in v0.3

- QUICK reduces questions, never safety checks.
- Existing user files are never overwritten.
- Target state is inspected again immediately before apply.
- Plan, approval, target identity, and staged content hashes must all match.
- Staging and target access use rooted filesystem handles and reject linked path components.
- Files are published atomically without replace semantics; the manifest is written last.
- Rollback removes only unchanged files created by the current run.
- Failed and recovery-required run state is preserved.
- The engine never commits, pushes, changes branches, deletes Git history, or sends telemetry.
- `REINITIALIZE` and permanent deletion remain disabled.
- ADOPT is analysis-only and cannot be applied in v0.3.

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

Set `EXORD_INIT_BIN` to a built executable to include the binary `plan -> approval -> apply` schema integration test:

```sh
EXORD_INIT_BIN=./bin/exord-init python -m unittest discover -s tests -p '*_test.py'
```

Native execution has currently been verified on Windows amd64. Linux amd64, macOS arm64, and Windows arm64 cross-compilation succeeds, but native platform-matrix and release-artifact verification remain future work.

## Roadmap

The planned risk-ordered implementation sequence is:

1. v0.2: safe `CREATE + QUICK` plan-bound apply — implemented.
2. v0.3: read-only `ADOPT` inventory, conflict analysis, Task and Git analysis — implemented.
3. v0.4: recovery commands, upgrade behavior, and fault injection.
4. Later: `REINITIALIZE` with verified external backup and separately approved destructive options.

This roadmap describes implementation order, not a release commitment.

## License

Engine, Skill, schemas, tests, and project documentation are licensed under [Apache License 2.0](LICENSE).

Template sources under `skill/exord-init/assets/templates/` and their substantial generated outputs are dedicated under [CC0 1.0](LICENSE.templates). Existing or unrelated user content is never relicensed.

Third-party dependency notices are listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## Contributing

The project is still defining its public contribution and release process. Until those policies are published, please open an issue before proposing a large change. Safety-contract changes should include tests and must not weaken plan binding, target reinspection, overwrite prevention, or recovery behavior.
