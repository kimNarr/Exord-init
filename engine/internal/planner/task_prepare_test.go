package planner

import (
	"os"
	"strings"
	"testing"

	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	"github.com/kimNarr/Exord-init/engine/internal/target"
	taskcontract "github.com/kimNarr/Exord-init/engine/internal/task"
)

func taskPreparationFixture(t *testing.T) (protocol.TaskIntent, target.TaskInspection, protocol.TaskDraft) {
	t.Helper()
	raw, err := os.ReadFile("../../../tests/fixtures/intent-task-review-ko.json")
	if err != nil {
		t.Fatal(err)
	}
	intent, err := taskcontract.DecodeIntent(raw)
	if err != nil {
		t.Fatal(err)
	}
	inspection := target.TaskInspection{ProjectID: "123e4567-e89b-42d3-a456-426614174000", IdentitySHA256: strings.Repeat("a", 64), Fingerprint: strings.Repeat("b", 64), TaskPath: "TASK.md", BaseBranchExists: true, Git: protocol.GitAnalysis{Detected: true, CLIAvailable: true, RootMatch: true, Status: "OK", Branch: intent.WorkBranch, Head: strings.Repeat("a", 40)}}
	draft := protocol.TaskDraft{SchemaVersion: 1, DraftID: "123e4567-e89b-42d3-a456-426614174001", TaskID: "T-20261016-7K3M2Q9R", CreatedAt: "2026-10-16T00:00:00Z", ProjectID: inspection.ProjectID, TargetIdentitySHA256: inspection.IdentitySHA256, WorkBranch: intent.WorkBranch}
	return intent, inspection, draft
}

func TestTaskPreparationRendersBothLanguagesAndBindsBytes(t *testing.T) {
	intent, inspection, draft := taskPreparationFixture(t)
	scan := protocol.TaskCollisionScan{Limits: []string{}}
	for _, language := range []string{"en", "ko"} {
		intent.Language = language
		intent.Title = `Title: "quoted"`
		intent.Owner = `@example: "quoted"`
		one, err := PrepareTask(intent, inspection, draft, scan)
		if err != nil {
			t.Fatal(err)
		}
		two, err := PrepareTask(intent, inspection, draft, scan)
		if err != nil {
			t.Fatal(err)
		}
		if one.Document != two.Document || one.SpecSHA256 != two.SpecSHA256 {
			t.Fatal("rendered bytes are not deterministic")
		}
		meta, err := taskcontract.ParseDocument([]byte(one.Document))
		if err != nil || meta.Owner != intent.Owner || meta.Title != intent.Title {
			t.Fatalf("YAML escaping failed: %#v %v", meta, err)
		}
		if err := ValidateTaskPreparation(one); err != nil {
			t.Fatal(err)
		}
		one.Document += "tampered"
		if err := ValidateTaskPreparation(one); err == nil {
			t.Fatal("document tampering accepted")
		}
	}
}

func TestTaskPreparationRejectsBlockersAndAllowsExplicitRename(t *testing.T) {
	intent, inspection, draft := taskPreparationFixture(t)
	scan := protocol.TaskCollisionScan{Limits: []string{}}
	inspection.TaskExists = true
	if _, err := PrepareTask(intent, inspection, draft, scan); err == nil {
		t.Fatal("existing task accepted")
	}
	inspection.TaskExists = false
	intent.WorkBranch = "feat/renamed"
	inspection.Git.Branch = intent.WorkBranch
	prepared, err := PrepareTask(intent, inspection, draft, scan)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := taskcontract.ParseDocument([]byte(prepared.Document))
	if err != nil || meta.WorkBranch != intent.WorkBranch || meta.TaskID != draft.TaskID {
		t.Fatal("renamed branch lost task identity")
	}
	draft.TargetIdentitySHA256 = strings.Repeat("c", 64)
	if _, err := PrepareTask(intent, inspection, draft, scan); err == nil {
		t.Fatal("foreign target draft accepted")
	}
}
