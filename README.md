# exord-init

`exord-init` is an early-stage Agent Skill and deterministic engine for planning a safe software-project foundation before feature implementation.

The current implementation is a v0.2 vertical slice: Skill discovery, `doctor`, `CREATE + QUICK` target inspection, SetupIntent validation, engine-generated Setup Plan, plan-bound human approval, local run state, and non-overwriting atomic apply with rollback. It does not commit, push, delete user data, adopt existing projects, or reinitialize projects.

The source Skill is in `skill/exord-init/`; protocol schemas are in `schemas/`; the Go engine entry point is `engine/cmd/exord-init/`. The temporary Go module path `example.com/exord-init` must be replaced only after the public repository owner is decided.

Building the engine requires Go 1.25 or newer because the safe apply path uses rooted, traversal-resistant filesystem operations including create-only hard-link publication.

Engine, Skill, schema, and project documentation source is licensed under Apache-2.0. Template sources under `skill/exord-init/assets/templates/` and their substantial generated outputs use CC0-1.0; unrelated user content is not relicensed.

See `docs/spec/` for the public implementation contracts and safety boundaries. Local planning history and review inputs are intentionally excluded from the repository.
