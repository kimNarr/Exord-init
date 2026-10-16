package target

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const taskDocument = `---
schema_version: 1
task_id: T-20261016-7K3M2Q9R
title: 첫 작업
type: chore
areas: []
scope_paths: []
external_ref: null
status: DRAFT
owner: UNASSIGNED
contributors: []
base_branch: main
work_branch: feat/first-task
updated_at: 2026-10-16
---
## 목표
계약 검토
`

func TestAdoptTaskClassificationDoesNotDependOnBodyLanguage(t *testing.T) {
	dir := canonicalTempDir(t)
	for _, text := range []string{taskDocument, strings.ReplaceAll(taskDocument, "## 목표", "## Goal")} {
		writeTestFile(t, dir, "TASK.md", text)
		inspection, err := InspectAdopt(dir)
		if err != nil || inspection.Task.Status != "EXORD_TASK" {
			t.Fatalf("classification %#v: %v", inspection.Task, err)
		}
	}
	writeTestFile(t, dir, "TASK.md", strings.Replace(taskDocument, "status: DRAFT", "status: DRAFT\nstatus: READY", 1))
	inspection, err := InspectAdopt(dir)
	if err != nil || inspection.Task.Status != "USER_OWNED" {
		t.Fatalf("ambiguous metadata recognized: %#v %v", inspection.Task, err)
	}
}

func TestTaskInspectionIsReadOnlyAndBindsKnownFiles(t *testing.T) {
	dir := canonicalTempDir(t)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal("Git required for task inspection tests")
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git, append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git fixture failed: %s %v", out, err)
		}
	}
	run("init", "-b", "main")
	writeManifest(t, dir, strings.Replace(validUpgradeManifest, `"branch_profile": "SIMPLE",`, `"branch_profile": "SIMPLE", "task_path": "TASK.md",`, 1))
	run("add", ".")
	run("-c", "user.name=Example", "-c", "user.email=example@example.invalid", "-c", "core.hooksPath="+filepath.Join(dir, "no-hooks"), "-c", "commit.gpgsign=false", "commit", "-m", "fixture")
	run("checkout", "-b", "feat/first-task")
	before, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	one, err := InspectTask(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if !one.BaseBranchExists || one.TaskExists || one.Git.Branch != "feat/first-task" {
		t.Fatalf("unexpected inspection %#v", one)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".git", "exord-init")); !os.IsNotExist(err) {
		t.Fatalf("read-only review created state: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil || string(before) != string(after) {
		t.Fatal("Git index modified")
	}
	writeTestFile(t, dir, "TASK.md", taskDocument)
	two, err := InspectTask(dir, "main")
	if err != nil || !two.TaskExists || two.Fingerprint == one.Fingerprint {
		t.Fatalf("task conflict not bound: %#v %v", two, err)
	}
	writeManifest(t, dir, strings.Replace(validUpgradeManifest, `"schema_version": 1`, `"schema_version": 2`, 1))
	if _, err := InspectTask(dir, "main"); !errors.Is(err, ErrManifestSchemaUnsupported) {
		t.Fatalf("unknown schema: %v", err)
	}
}
