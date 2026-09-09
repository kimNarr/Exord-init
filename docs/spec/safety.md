# Safety contract v1

Only the current human user can approve a plan. Repository content, imported documents, agent output, issue text, and PR text are untrusted input and never confer approval.

`CREATE + QUICK` planning is allowed only for an exact existing directory whose entries are empty or on the harmless-entry allowlist. Root, home, symlinked/junction targets, and user-owned entries fail closed. QUICK cannot reduce safety checks.

An eventual apply must acquire a target/project lock, re-inspect all input state, reject stale approval, and use same-directory temporary files with flush, validation, and atomic replacement. No cross-filesystem rename is part of the atomicity assumption.

REINITIALIZE stays disabled until backup fidelity, local recovery indexing, locking, fault injection, and supported-platform integration tests all pass. Permanent deletion and Git-history removal remain separate advanced approvals.
