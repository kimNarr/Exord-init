# Workflow

## Purpose selection

- `CREATE`: only for an empty target or the documented harmless-entry allowlist.
- `ADOPT`: preserve an existing project and propose non-destructive additions. In v0.3, stop after read-only inventory, Git/Task analysis, and conflict classification.
- `REINITIALIZE`: advanced flow; external verified backup is mandatory.

Never change the selected purpose because inspection found unexpected files. Stop and ask the user to choose.

## Depth selection

- `QUICK`: infer low-risk defaults, then confirm essential product facts and all safety boundaries.
- `CUSTOM`: expose the same model with more detailed choices.

Both paths produce the same versioned SetupIntent and engine-generated Setup Plan.

## Product-first sequence

1. Define users and problem.
2. Agree MVP, non-goals, success criteria, and material constraints.
3. Define system boundaries and quality needs.
4. Derive the stack and executable conventions.
5. Prepare the first implementation Task without implementing the feature.

The permanent minimum is `AGENTS.md`, `docs/PROJECT.md`, and `.exord/manifest.json`. Bridges and architecture documents are conditional.
