package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kimNarr/Exord-init/engine/internal/planner"
	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	"github.com/kimNarr/Exord-init/engine/internal/target"
	taskcontract "github.com/kimNarr/Exord-init/engine/internal/task"
)

func draftTestRoot(t *testing.T) (string, *os.Root) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	root, lock, err := OpenTaskDraftState(dir, "123e4567-e89b-42d3-a456-426614174001")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lock.Release(); err != nil {
			t.Error(err)
		}
		root.Close()
	})
	return dir, root
}

func TestTaskDraftReusesIdentityAndPreservesPlanRevisions(t *testing.T) {
	dir, root := draftTestRoot(t)
	if _, err := Acquire(filepath.Join(dir, ".git", "exord-init"), "123e4567-e89b-42d3-a456-426614174002"); !errors.Is(err, ErrLocked) {
		t.Fatalf("CREATE and task locks differ: %v", err)
	}
	raw, err := os.ReadFile("../../../tests/fixtures/intent-task-review-ko.json")
	if err != nil {
		t.Fatal(err)
	}
	intent, err := taskcontract.DecodeIntent(raw)
	if err != nil {
		t.Fatal(err)
	}
	projectID := "123e4567-e89b-42d3-a456-426614174000"
	identity := strings.Repeat("a", 64)
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	draft, err := ResolveTaskDraft(root, projectID, identity, intent.WorkBranch, "", map[string]bool{}, now)
	if err != nil {
		t.Fatal(err)
	}
	reused, err := ResolveTaskDraft(root, projectID, identity, intent.WorkBranch, "", map[string]bool{}, now.Add(24*time.Hour))
	if err != nil || reused != draft {
		t.Fatalf("draft ID changed: %#v %v", reused, err)
	}
	selected, err := ResolveTaskDraft(root, projectID, identity, "feat/renamed", draft.DraftID, map[string]bool{}, now)
	if err != nil || selected != draft {
		t.Fatal("explicit draft reuse failed", err)
	}
	inspection := target.TaskInspection{ProjectID: projectID, IdentitySHA256: identity, Fingerprint: strings.Repeat("b", 64), TaskPath: "TASK.md", BaseBranchExists: true, Git: protocol.GitAnalysis{Detected: true, CLIAvailable: true, RootMatch: true, Status: "OK", Branch: intent.WorkBranch, Head: strings.Repeat("c", 40)}}
	prepare := func() protocol.TaskPreparation {
		t.Helper()
		p, err := planner.PrepareTask(intent, inspection, draft, protocol.TaskCollisionScan{Limits: []string{}})
		if err != nil {
			t.Fatal(err)
		}
		p, err = PersistTaskPreparation(root, p)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	one := prepare()
	same := prepare()
	if same.PlanID != one.PlanID || same.Document != one.Document {
		t.Fatal("identical plan revision was overwritten")
	}
	intent.Title = "revised title"
	two := prepare()
	if two.Spec.Draft.TaskID != one.Spec.Draft.TaskID || two.SpecSHA256 == one.SpecSHA256 {
		t.Fatal("replan lost identity or revision")
	}
	planPath := draftDirectory(projectID, intent.WorkBranch) + "/plans/" + one.SpecSHA256 + ".json"
	var preserved protocol.TaskPreparation
	if err := draftRead(root, planPath, &preserved); err != nil || preserved.Document != one.Document {
		t.Fatal("old revision lost", err)
	}
	file, err := root.OpenFile(planPath, os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	file.WriteString("corrupt record")
	file.Close()
	if _, err := PersistTaskPreparation(root, one); err == nil {
		t.Fatal("corrupt revision silently replaced")
	}
	if _, err := ResolveTaskDraft(root, projectID, identity, intent.WorkBranch, "", map[string]bool{draft.TaskID: true}, now); err == nil {
		t.Fatal("new history collision ignored")
	}
	if _, err := ResolveTaskDraft(root, projectID, strings.Repeat("d", 64), intent.WorkBranch, "", map[string]bool{}, now); err == nil {
		t.Fatal("foreign target binding ignored")
	}
}

func TestTaskDraftStateRejectsLinkedDirectory(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, ".git", "exord-init")); err != nil {
		t.Skip("symlink creation unavailable")
	}
	root, lock, err := OpenTaskDraftState(dir, "123e4567-e89b-42d3-a456-426614174000")
	if err == nil {
		lock.Release()
		root.Close()
		t.Fatal("linked state followed")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("outside directory changed", err)
	}
}
