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

// discardFixture persists a run and forces its journal into the given settled
// (or unsettled) state so discard classification can be exercised directly.
func discardFixture(t *testing.T, status, stage string) (string, string, planner.PreparedPlan) {
	t.Helper()
	targetPath, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := target.InspectCreate(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	intent := protocol.SetupIntent{SchemaVersion: 1, Mode: "CREATE", Depth: "QUICK", ProjectSummary: "discard fixture", DocumentationLanguage: "en", SupportedAgents: []string{"codex"}}
	prepared, err := planner.PrepareWithProjectID(intent, inspection, testProjectID)
	if err != nil {
		t.Fatal(err)
	}
	projectRoot := filepath.Join(t.TempDir(), "state")
	journal, err := state.PersistPrepared(projectRoot, testRunID, prepared)
	if err != nil {
		t.Fatal(err)
	}
	journal.Status = status
	journal.Stage = stage
	runDir, _ := state.RunDirectory(projectRoot, testRunID)
	if err := state.WriteJournal(runDir, journal); err != nil {
		t.Fatal(err)
	}
	return targetPath, projectRoot, prepared
}

func discardApproval(plan protocol.SetupPlan) protocol.Approval {
	return protocol.Approval{SchemaVersion: 1, RunID: testRunID, PlanID: plan.PlanID, SpecSHA256: plan.SpecSHA256, TargetIdentitySHA256: plan.Spec.TargetIdentitySHA256, ApprovedAction: "RECOVER_DISCARD", ApprovedAt: time.Now().UTC().Format(time.RFC3339)}
}

func TestDiscardRemovesSettledRuns(t *testing.T) {
	for _, settled := range []struct{ status, stage string }{
		{"FAILED", "ROLLED_BACK"},
		{"FINALIZED", "FINALIZED"},
		{"ACTIVE", "PLANNED"},
		{"ACTIVE", "APPROVED"},
	} {
		t.Run(settled.status+"/"+settled.stage, func(t *testing.T) {
			targetPath, projectRoot, prepared := discardFixture(t, settled.status, settled.stage)
			removed, failure := Discard(targetPath, projectRoot, testRunID, discardApproval(prepared.Plan))
			if failure != nil {
				t.Fatalf("discard failed: %s: %v", failure.Code, failure.Err)
			}
			if !removed {
				t.Fatal("discard reported nothing removed")
			}
			runDir, _ := state.RunDirectory(projectRoot, testRunID)
			if _, err := os.Stat(runDir); !os.IsNotExist(err) {
				t.Fatalf("run bundle still present: %v", err)
			}
		})
	}
}

func TestDiscardRefusesNonSettledRuns(t *testing.T) {
	for _, blocked := range []struct{ status, stage string }{
		{"RECOVERY_REQUIRED", "FILES_APPLYING"},
		{"ACTIVE", "FILES_APPLYING"},
		{"ACTIVE", "VALIDATED"},
	} {
		t.Run(blocked.status+"/"+blocked.stage, func(t *testing.T) {
			targetPath, projectRoot, prepared := discardFixture(t, blocked.status, blocked.stage)
			removed, failure := Discard(targetPath, projectRoot, testRunID, discardApproval(prepared.Plan))
			if failure == nil || failure.Code != "DISCARD_NOT_ALLOWED" {
				t.Fatalf("expected DISCARD_NOT_ALLOWED, got %#v", failure)
			}
			if removed {
				t.Fatal("discard must not remove a non-settled run")
			}
			runDir, _ := state.RunDirectory(projectRoot, testRunID)
			if _, err := os.Stat(runDir); err != nil {
				t.Fatalf("run bundle must be preserved: %v", err)
			}
		})
	}
}

func TestDiscardRejectsWrongApprovalAction(t *testing.T) {
	targetPath, projectRoot, prepared := discardFixture(t, "FAILED", "ROLLED_BACK")
	approval := discardApproval(prepared.Plan)
	approval.ApprovedAction = "RECOVER_ROLLBACK"
	_, failure := Discard(targetPath, projectRoot, testRunID, approval)
	if failure == nil || failure.Code != "APPROVAL_MISMATCH" {
		t.Fatalf("expected APPROVAL_MISMATCH, got %#v", failure)
	}
}

func TestDiscardRejectsMismatchedApprovalBinding(t *testing.T) {
	targetPath, projectRoot, prepared := discardFixture(t, "FAILED", "ROLLED_BACK")
	approval := discardApproval(prepared.Plan)
	approval.SpecSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	_, failure := Discard(targetPath, projectRoot, testRunID, approval)
	if failure == nil || failure.Code != "APPROVAL_MISMATCH" {
		t.Fatalf("expected APPROVAL_MISMATCH, got %#v", failure)
	}
	runDir, _ := state.RunDirectory(projectRoot, testRunID)
	if _, err := os.Stat(runDir); err != nil {
		t.Fatalf("run bundle must be preserved on binding mismatch: %v", err)
	}
}
