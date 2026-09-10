package apply

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	"github.com/kimNarr/Exord-init/engine/internal/state"
)

func writeTargetFile(t *testing.T, root string, relative, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const validManifest = `{
  "schema_version": 1,
  "generator": {"name": "exord-init"},
  "project_id": "11111111-1111-4111-8111-111111111111",
  "settings": {"git_mode": "ENABLED"},
  "managed_files": []
}`

const validProjectEN = "# Project\n\n## Users and problem\n\nx\n\n## MVP and non-goals\n\ny\n\n## Success criteria\n\nz\n\n## Boundaries and constraints\n\nw\n"

func basePlan() protocol.SetupPlan {
	return protocol.SetupPlan{
		Spec: protocol.PlanSpec{
			ProjectID: "11111111-1111-4111-8111-111111111111",
			Validation: []protocol.Validation{
				{ID: "agents-size-v1", Path: "AGENTS.md"},
				{ID: "manifest-schema-v1", Path: ".exord/manifest.json"},
				{ID: "project-required-sections-v1", Path: "docs/PROJECT.md"},
			},
		},
	}
}

func setupValidationRoot(t *testing.T, agents, manifest, project string) (*os.Root, []createdFile) {
	t.Helper()
	dir := t.TempDir()
	writeTargetFile(t, dir, "AGENTS.md", agents)
	writeTargetFile(t, dir, ".exord/manifest.json", manifest)
	writeTargetFile(t, dir, "docs/PROJECT.md", project)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	created := []createdFile{
		{Path: "AGENTS.md"},
		{Path: filepath.FromSlash(".exord/manifest.json")},
		{Path: filepath.FromSlash("docs/PROJECT.md")},
	}
	return root, created
}

func TestRunValidationsAcceptsWellFormedOutput(t *testing.T) {
	root, created := setupValidationRoot(t, "# rules\n", validManifest, validProjectEN)
	if err := runValidations(root, basePlan(), created); err != nil {
		t.Fatalf("expected valid output to pass, got %v", err)
	}
}

func TestRunValidationsAcceptsKoreanProjectSections(t *testing.T) {
	korean := "# 프로젝트\n\n## 사용자와 문제\n\nx\n\n## MVP와 비목표\n\ny\n\n## 성공 기준\n\nz\n\n## 경계와 제약\n\nw\n"
	root, created := setupValidationRoot(t, "# rules\n", validManifest, korean)
	if err := runValidations(root, basePlan(), created); err != nil {
		t.Fatalf("expected Korean sections to pass, got %v", err)
	}
}

func TestRunValidationsToleratesMarkdownBreakingSummary(t *testing.T) {
	// A hostile project summary injects fake headings and unbalanced fences,
	// but every real required heading is still present on its own line.
	hostile := "# Project\n\n## Users and problem\n\n```\n## Success criteria\nnot really\n### ```\n\n## MVP and non-goals\n\ny\n\n## Success criteria\n\nz\n\n## Boundaries and constraints\n\nw\n"
	root, created := setupValidationRoot(t, "# rules\n", validManifest, hostile)
	if err := runValidations(root, basePlan(), created); err != nil {
		t.Fatalf("expected injected markdown to still validate, got %v", err)
	}
}

func TestRunValidationsRejectsUnknownValidationID(t *testing.T) {
	root, created := setupValidationRoot(t, "# rules\n", validManifest, validProjectEN)
	plan := basePlan()
	plan.Spec.Validation = append(plan.Spec.Validation, protocol.Validation{ID: "future-check-v9", Path: "AGENTS.md"})
	err := runValidations(root, plan, created)
	if err == nil || !strings.Contains(err.Error(), "unknown validation ID") {
		t.Fatalf("expected unknown-ID fail-closed, got %v", err)
	}
}

func TestRunValidationsRejectsOversizedAgentsFile(t *testing.T) {
	root, created := setupValidationRoot(t, strings.Repeat("x\n", maxAgentsFileLines+5), validManifest, validProjectEN)
	if err := runValidations(root, basePlan(), created); err == nil || !strings.Contains(err.Error(), "agents-size-v1") {
		t.Fatalf("expected agents-size-v1 failure, got %v", err)
	}
}

func TestRunValidationsRejectsManifestProjectIDMismatch(t *testing.T) {
	bad := strings.Replace(validManifest, "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", 1)
	root, created := setupValidationRoot(t, "# rules\n", bad, validProjectEN)
	if err := runValidations(root, basePlan(), created); err == nil || !strings.Contains(err.Error(), "manifest-schema-v1") {
		t.Fatalf("expected manifest-schema-v1 failure, got %v", err)
	}
}

func TestRunValidationsRejectsMissingProjectSection(t *testing.T) {
	missing := strings.Replace(validProjectEN, "## Success criteria\n\nz\n\n", "", 1)
	root, created := setupValidationRoot(t, "# rules\n", validManifest, missing)
	if err := runValidations(root, basePlan(), created); err == nil || !strings.Contains(err.Error(), "project-required-sections-v1") {
		t.Fatalf("expected project-required-sections-v1 failure, got %v", err)
	}
}

func TestRunValidationsRejectsTargetNotCreatedByRun(t *testing.T) {
	root, _ := setupValidationRoot(t, "# rules\n", validManifest, validProjectEN)
	err := runValidations(root, basePlan(), []createdFile{{Path: "AGENTS.md"}})
	if err == nil || !strings.Contains(err.Error(), "not created by this run") {
		t.Fatalf("expected not-created-by-run failure, got %v", err)
	}
}

func TestExecuteRollsBackWhenValidationFails(t *testing.T) {
	targetPath, projectRoot, runID, prepared := prepareRun(t)
	original := validateCreatedFiles
	validateCreatedFiles = func(*os.Root, protocol.SetupPlan, []createdFile) error {
		return errors.New("declared validation rejected the output")
	}
	defer func() { validateCreatedFiles = original }()

	_, failure := Execute(targetPath, projectRoot, runID, approvalFor(runID, prepared.Plan))
	if failure == nil || failure.Code != "APPLY_FAILED" {
		t.Fatalf("expected APPLY_FAILED after validation failure, got %#v", failure)
	}
	for _, file := range prepared.Files {
		if _, err := os.Stat(filepath.Join(targetPath, filepath.FromSlash(file.Path))); !os.IsNotExist(err) {
			t.Fatalf("validation failure must roll back %s: %v", file.Path, err)
		}
	}
	_, _, journal, err := state.Load(projectRoot, runID)
	if err != nil {
		t.Fatal(err)
	}
	if journal.Status != "FAILED" || journal.Stage != "ROLLED_BACK" {
		t.Fatalf("unexpected journal after validation rollback: %#v", journal)
	}
}
