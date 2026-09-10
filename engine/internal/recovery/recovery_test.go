package recovery

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kimNarr/Exord-init/engine/internal/planner"
	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	"github.com/kimNarr/Exord-init/engine/internal/state"
	"github.com/kimNarr/Exord-init/engine/internal/target"
)

const (
	testRunID     = "123e4567-e89b-42d3-a456-426614174010"
	testProjectID = "123e4567-e89b-42d3-a456-426614174011"
)

func TestRollbackRemovesOnlyHashMatchedGeneratedFile(t *testing.T) {
	targetPath, projectRoot, prepared, operation := recoveryFixture(t, true)
	analysis, failure := Inspect(targetPath, projectRoot, testRunID)
	if failure != nil {
		t.Fatalf("inspect failed: %s: %v", failure.Code, failure.Err)
	}
	if analysis.Disposition != "ROLLBACK_READY" || analysis.Recoverable != 1 {
		t.Fatalf("unexpected recovery analysis: %#v", analysis)
	}
	approval := recoveryApproval(prepared.Plan)
	final, changed, failure := Rollback(targetPath, projectRoot, testRunID, approval)
	if failure != nil {
		t.Fatalf("rollback failed: %s: %v", failure.Code, failure.Err)
	}
	if !changed || final.Disposition != "ROLLED_BACK_RETAINED" {
		t.Fatalf("unexpected rollback result: changed=%v analysis=%#v", changed, final)
	}
	if _, err := os.Lstat(filepath.Join(targetPath, filepath.FromSlash(operation.Path))); !os.IsNotExist(err) {
		t.Fatalf("generated file was not removed: %v", err)
	}
}

func TestRollbackRefusesModifiedGeneratedFileWithoutMutation(t *testing.T) {
	targetPath, projectRoot, prepared, operation := recoveryFixture(t, false)
	matchedPath := "docs/PROJECT.md"
	var matchedContent []byte
	for _, candidate := range prepared.Files {
		if candidate.Path == matchedPath {
			matchedContent = candidate.Content
			break
		}
	}
	matchedDestination := filepath.Join(targetPath, filepath.FromSlash(matchedPath))
	if err := os.MkdirAll(filepath.Dir(matchedDestination), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(matchedDestination, matchedContent, 0600); err != nil {
		t.Fatal(err)
	}
	runDir, _ := state.RunDirectory(projectRoot, testRunID)
	_, _, journal, err := state.Load(projectRoot, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	for index := range journal.Operations {
		if journal.Operations[index].Path == matchedPath {
			journal.Operations[index].Status = "APPLIED"
		}
	}
	if err := state.WriteJournal(runDir, journal); err != nil {
		t.Fatal(err)
	}
	analysis, failure := Inspect(targetPath, projectRoot, testRunID)
	if failure != nil {
		t.Fatalf("inspect failed: %s: %v", failure.Code, failure.Err)
	}
	if analysis.Disposition != "CONFLICT" || analysis.Conflicts != 1 {
		t.Fatalf("modified file was not classified as conflict: %#v", analysis)
	}
	_, changed, failure := Rollback(targetPath, projectRoot, testRunID, recoveryApproval(prepared.Plan))
	if failure == nil || failure.Code != "RECOVERY_REQUIRED" || changed {
		t.Fatalf("unsafe rollback result: changed=%v failure=%#v", changed, failure)
	}
	content, err := os.ReadFile(filepath.Join(targetPath, filepath.FromSlash(operation.Path)))
	if err != nil || string(content) != "user modification\n" {
		t.Fatalf("modified file was touched: %q %v", content, err)
	}
	if content, err := os.ReadFile(matchedDestination); err != nil || string(content) != string(matchedContent) {
		t.Fatalf("preflight removed a safe file before noticing another conflict: %q %v", content, err)
	}
}

func recoveryFixture(t *testing.T, matching bool) (string, string, planner.PreparedPlan, protocol.Operation) {
	t.Helper()
	targetPath := t.TempDir()
	inspection, err := target.InspectCreate(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	intent := protocol.SetupIntent{SchemaVersion: 1, Mode: "CREATE", Depth: "QUICK", ProjectSummary: "recovery fixture", DocumentationLanguage: "en", SupportedAgents: []string{"codex"}}
	prepared, err := planner.PrepareWithProjectID(intent, inspection, testProjectID)
	if err != nil {
		t.Fatal(err)
	}
	projectRoot := filepath.Join(t.TempDir(), "state")
	journal, err := state.PersistPrepared(projectRoot, testRunID, prepared)
	if err != nil {
		t.Fatal(err)
	}
	var operation protocol.Operation
	var content []byte
	for _, candidate := range prepared.Plan.Spec.Operations {
		if candidate.Path == "AGENTS.md" {
			operation = candidate
			break
		}
	}
	for _, candidate := range prepared.Files {
		if candidate.Path == operation.Path {
			content = candidate.Content
			break
		}
	}
	if operation.Path == "" || content == nil {
		t.Fatal("fixture operation missing")
	}
	if !matching {
		content = []byte("user modification\n")
	}
	destination := filepath.Join(targetPath, filepath.FromSlash(operation.Path))
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, content, 0600); err != nil {
		t.Fatal(err)
	}
	journal.Status = "RECOVERY_REQUIRED"
	journal.Stage = "FILES_APPLYING"
	for index := range journal.Operations {
		if journal.Operations[index].Path == operation.Path {
			journal.Operations[index].Status = "APPLYING"
		}
	}
	runDir, _ := state.RunDirectory(projectRoot, testRunID)
	if err := state.WriteJournal(runDir, journal); err != nil {
		t.Fatal(err)
	}
	return targetPath, projectRoot, prepared, operation
}

func recoveryApproval(plan protocol.SetupPlan) protocol.Approval {
	return protocol.Approval{SchemaVersion: 1, RunID: testRunID, PlanID: plan.PlanID, SpecSHA256: plan.SpecSHA256, TargetIdentitySHA256: plan.Spec.TargetIdentitySHA256, ApprovedAction: "RECOVER_ROLLBACK", ApprovedAt: time.Now().UTC().Format(time.RFC3339)}
}
