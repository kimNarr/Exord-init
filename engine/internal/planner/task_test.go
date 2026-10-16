package planner

import (
	"os"
	"testing"

	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	"github.com/kimNarr/Exord-init/engine/internal/target"
	taskcontract "github.com/kimNarr/Exord-init/engine/internal/task"
)

func TestTaskReviewReadinessAndHash(t *testing.T) {
	raw, err := os.ReadFile("../../../tests/fixtures/intent-task-review-ko.json")
	if err != nil {
		t.Fatal(err)
	}
	intent, err := taskcontract.DecodeIntent(raw)
	if err != nil {
		t.Fatal(err)
	}
	inspection := target.TaskInspection{ProjectID: "123e4567-e89b-42d3-a456-426614174000", TaskPath: "TASK.md", BaseBranchExists: true, Git: protocol.GitAnalysis{Detected: true, CLIAvailable: true, RootMatch: true, Status: "OK", Branch: intent.WorkBranch, Head: "abc"}}
	one, err := BuildTaskReview(intent, inspection)
	if err != nil {
		t.Fatal(err)
	}
	two, err := BuildTaskReview(intent, inspection)
	if err != nil {
		t.Fatal(err)
	}
	if one.SpecSHA256 != two.SpecSHA256 || one.PlanID == two.PlanID || one.Spec.Disposition != "DRAFT" || one.Spec.ValidationResult != "NOT_RUN" {
		t.Fatalf("unexpected plans: %#v %#v", one, two)
	}
	intent.Status = "READY"
	blocked, _ := BuildTaskReview(intent, inspection)
	if blocked.Spec.Disposition != "BLOCKED" {
		t.Fatal("unconfirmed owner became ready")
	}
	intent.Owner = "@example"
	intent.OwnerConfirmed = true
	intent.AcceptanceCriteria = []string{"observable result"}
	intent.Validation = []string{"manual observation"}
	ready, _ := BuildTaskReview(intent, inspection)
	if ready.Spec.Disposition != "READY_FOR_REVIEW" {
		t.Fatal(ready)
	}
	inspection.Git.TrackedDirty = true
	dirty, _ := BuildTaskReview(intent, inspection)
	if dirty.Spec.Disposition != "READY_FOR_REVIEW" || dirty.SpecSHA256 == ready.SpecSHA256 {
		t.Fatal("dirty state not bound to review")
	}
	for name, change := range map[string]func(*target.TaskInspection){
		"main":               func(i *target.TaskInspection) { i.Git.Branch = "main" },
		"custom integration": func(i *target.TaskInspection) { i.IntegrationBranches = []string{intent.WorkBranch} },
		"task conflict":      func(i *target.TaskInspection) { i.TaskExists = true },
		"git operation":      func(i *target.TaskInspection) { i.Git.InProgress = "REBASE" },
		"unborn":             func(i *target.TaskInspection) { i.Git.Unborn = true },
		"detached":           func(i *target.TaskInspection) { i.Git.Branch = "" },
		"worktree":           func(i *target.TaskInspection) { i.Git.GitFile = true },
		"missing base":       func(i *target.TaskInspection) { i.BaseBranchExists = false },
	} {
		t.Run(name, func(t *testing.T) {
			copy := inspection
			change(&copy)
			p, err := BuildTaskReview(intent, copy)
			if err != nil || p.Spec.Disposition != "BLOCKED" || len(p.Spec.Blockers) == 0 {
				t.Fatalf("unsafe review: %#v %v", p, err)
			}
		})
	}
}
