package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"example.com/exord-init/engine/internal/planner"
	"example.com/exord-init/engine/internal/protocol"
	"example.com/exord-init/engine/internal/state"
	"example.com/exord-init/engine/internal/target"
)

func TestExecuteAppliesBoundCreatePlanAndCleansRun(t *testing.T) {
	targetPath, projectRoot, runID, prepared := prepareRun(t)
	approval := approvalFor(runID, prepared.Plan)
	outcome, failure := Execute(targetPath, projectRoot, runID, approval)
	if failure != nil {
		t.Fatalf("apply failed: %s: %v", failure.Code, failure.Err)
	}
	if len(outcome.CreatedPaths) != len(prepared.Files) {
		t.Fatalf("created %d files, want %d", len(outcome.CreatedPaths), len(prepared.Files))
	}
	for _, file := range prepared.Files {
		if _, err := os.Stat(filepath.Join(targetPath, filepath.FromSlash(file.Path))); err != nil {
			t.Fatalf("missing generated file %s: %v", file.Path, err)
		}
	}
	runDir, _ := state.RunDirectory(projectRoot, runID)
	if _, err := os.Stat(runDir); !os.IsNotExist(err) {
		t.Fatalf("finalized run was not cleaned: %v", err)
	}
}

func TestExecuteRejectsStaleTargetWithoutCreatingFiles(t *testing.T) {
	targetPath, projectRoot, runID, prepared := prepareRun(t)
	if err := os.WriteFile(filepath.Join(targetPath, "user-file.txt"), []byte("mine"), 0600); err != nil {
		t.Fatal(err)
	}
	_, failure := Execute(targetPath, projectRoot, runID, approvalFor(runID, prepared.Plan))
	if failure == nil || failure.Code != "PLAN_STALE" {
		t.Fatalf("expected PLAN_STALE, got %#v", failure)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("apply touched target after stale detection: %v", err)
	}
}

func TestExecuteRejectsMismatchedApproval(t *testing.T) {
	targetPath, projectRoot, runID, prepared := prepareRun(t)
	approval := approvalFor(runID, prepared.Plan)
	approval.SpecSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	_, failure := Execute(targetPath, projectRoot, runID, approval)
	if failure == nil || failure.Code != "APPROVAL_MISMATCH" {
		t.Fatalf("expected APPROVAL_MISMATCH, got %#v", failure)
	}
}

func TestExecuteClassifiesCorruptStoredPlanSeparately(t *testing.T) {
	targetPath, projectRoot, runID, prepared := prepareRun(t)
	runDir, _ := state.RunDirectory(projectRoot, runID)
	planPath := filepath.Join(runDir, "plan.json")
	content, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	content[len(content)/2] ^= 1
	if err := os.WriteFile(planPath, content, 0600); err != nil {
		t.Fatal(err)
	}
	_, failure := Execute(targetPath, projectRoot, runID, approvalFor(runID, prepared.Plan))
	if failure == nil || failure.Code != "PLAN_INVALID" {
		t.Fatalf("expected PLAN_INVALID, got %#v", failure)
	}
}

func TestWriteCreateNeverOverwritesExistingPath(t *testing.T) {
	rootPath := t.TempDir()
	destination := filepath.Join(rootPath, "AGENTS.md")
	if err := os.WriteFile(destination, []byte("user owned\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, _, err := writeCreate(root, "AGENTS.md", []byte("generated\n")); err == nil {
		t.Fatal("expected existing destination rejection")
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "user owned\n" {
		t.Fatalf("existing content changed: %q, %v", content, err)
	}
}

func TestRollbackRemovesOnlyUnmodifiedGeneratedFiles(t *testing.T) {
	rootPath := t.TempDir()
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	firstContent := []byte("first\n")
	secondContent := []byte("second\n")
	firstPath, firstDirs, err := writeCreate(root, "docs/first.md", firstContent)
	if err != nil {
		t.Fatal(err)
	}
	secondPath, secondDirs, err := writeCreate(root, "second.md", secondContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootPath, "docs", "first.md"), []byte("user changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	journal := protocol.RunJournal{Operations: []protocol.RunOperation{{Path: "docs/first.md", Status: "APPLIED"}, {Path: "second.md", Status: "APPLIED"}}}
	created := []createdFile{
		{Path: firstPath, ExpectedSHA256: sha256Hex(firstContent), Directories: firstDirs},
		{Path: secondPath, ExpectedSHA256: sha256Hex(secondContent), Directories: secondDirs},
	}
	if rollback(root, created, &journal) {
		t.Fatal("rollback must report incomplete when a generated file was modified")
	}
	if _, err := os.Stat(filepath.Join(rootPath, "docs", "first.md")); err != nil {
		t.Fatalf("modified file must be preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootPath, "second.md")); !os.IsNotExist(err) {
		t.Fatalf("unmodified file was not rolled back: %v", err)
	}
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func prepareRun(t *testing.T) (string, string, string, planner.PreparedPlan) {
	t.Helper()
	targetPath := t.TempDir()
	if err := os.Mkdir(filepath.Join(targetPath, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	inspection, err := target.InspectCreate(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := planner.Prepare(protocol.SetupIntent{SchemaVersion: 1, Mode: "CREATE", Depth: "QUICK", ProjectSummary: "test", DocumentationLanguage: "en", SupportedAgents: []string{"codex"}}, inspection)
	if err != nil {
		t.Fatal(err)
	}
	runID := "123e4567-e89b-42d3-a456-426614174000"
	projectRoot, err := state.ProjectRoot(targetPath, inspection.IdentitySHA256)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.PersistPrepared(projectRoot, runID, prepared); err != nil {
		t.Fatal(err)
	}
	return targetPath, projectRoot, runID, prepared
}

func approvalFor(runID string, plan protocol.SetupPlan) protocol.Approval {
	return protocol.Approval{SchemaVersion: 1, RunID: runID, PlanID: plan.PlanID, SpecSHA256: plan.SpecSHA256, TargetIdentitySHA256: plan.Spec.TargetIdentitySHA256, ApprovedAction: "APPLY_CREATE", ApprovedAt: time.Now().UTC().Format(time.RFC3339)}
}
