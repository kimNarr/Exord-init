package target

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTaskIDScanFindsMessagesHistoryBranchesAndOtherWorktrees(t *testing.T) {
	parent := canonicalTempDir(t)
	dir := filepath.Join(parent, "project")
	linked := filepath.Join(parent, "linked")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		base := []string{"-C", dir, "-c", "core.hooksPath=" + filepath.Join(parent, "no-hooks"), "-c", "user.name=Example", "-c", "user.email=example@example.invalid", "-c", "commit.gpgsign=false"}
		out, err := exec.Command("git", append(base, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git fixture: %s %v", out, err)
		}
	}
	run("init", "-b", "main")
	writeTestFile(t, dir, "README.md", "fixture")
	run("add", ".")
	run("commit", "-m", "Task: T-20261016-00000000")
	writeTestFile(t, dir, "TASK.md", "task_id: T-20261016-11111111\n")
	run("add", ".")
	run("commit", "-m", "temporary task")
	run("rm", "TASK.md")
	run("commit", "-m", "task removed")
	run("branch", "feat/t-20261016-22222222")
	run("checkout", "-b", "feat/current")
	run("worktree", "add", "-b", "feat/linked", linked)
	writeTestFile(t, linked, "TASK.md", "task_id: T-20261016-33333333\n")
	used, scan, err := InspectTaskIDs(dir, "feat/current")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"T-20261016-00000000", "T-20261016-11111111", "T-20261016-22222222", "T-20261016-33333333"} {
		if !used[value] {
			t.Fatalf("missed local ID %s: %v", value, used)
		}
	}
	if len(scan.Limits) != 0 || scan.Worktrees != 2 {
		t.Fatalf("unexpected scan evidence %#v", scan)
	}
}

func TestCollisionGitOutputIsBounded(t *testing.T) {
	var out boundedGitOutput
	if _, err := out.Write(bytes.Repeat([]byte{'x'}, (8<<20)+1)); err == nil || !out.limited || out.Len() != 8<<20 {
		t.Fatal("unbounded collision scan output")
	}
}

func TestCollisionGitOutputBoundsStreamCopies(t *testing.T) {
	var out boundedGitOutput
	// Hide bytes.Reader.WriteTo to exercise io.Copy's destination dispatch, as
	// os/exec may do when it drains a child process pipe. Embedding bytes.Buffer
	// would expose ReadFrom and bypass the bounded Write implementation.
	source := struct{ io.Reader }{bytes.NewReader(bytes.Repeat([]byte{'x'}, (8<<20)+1))}
	if _, err := io.Copy(&out, source); err == nil || !out.limited || out.Len() != 8<<20 {
		t.Fatal("stream copy bypassed the Git output limit")
	}
}
