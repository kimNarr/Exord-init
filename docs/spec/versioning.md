# Version policy

The current development milestone is `0.6.0-dev` (local Task preparation).
The canonical source value
is `engine/internal/buildinfo/version.go`; the CLI default, generated manifest
and upgrade comparison use it. Release builds must not override the CLI value
with a version different from the embedded generator/templates.

Before 1.0, increment the minor version for a new supported feature or a
breaking behavior change, and increment the patch version for a published
bug-fix milestone without new capabilities. Documentation/test-only commits do
not require a version increment. Keep `-dev` until a milestone satisfies its
release checks; do not imply stability or installation support from a tag alone.
Add a CHANGELOG entry when advancing a milestone, separating implemented work
from remaining scope. Unfinished work from earlier slices need not block a new
feature milestone; list it explicitly rather than declaring that slice complete.

Create an annotated `v<version>` Git tag for a reviewed milestone and push only
that tag. Tags are immutable: corrections receive a new version, not a moved
tag. The `v0.5.0-dev` tag records the first read-only Task review milestone;
it is a source checkpoint, not a binary or marketplace release.

Protocol/schema and file-template versions have independent compatibility
meaning. Increment them only when their own contracts change, and document the
migration. This milestone retains protocol v1 and existing template version 1;
task review/preparation schemas have schema_version 1 and capabilities
`task:plan` / `task:prepare`. Consumers
discover optional commands via doctor capabilities. Task review hashes cannot
authorize CREATE apply. The planning document v1.3 is a historical design
checkpoint, not the engine's release version.

For every milestone run build, vet, Go tests, Python schema/CLI integration and
the skill validator; verify doctor and generated manifest agree on the version.
Record native test coverage separately from cross-compilation. GitHub Actions
provides the additional platform checks after push; do not claim their success
before inspecting the results.
