package target

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kimNarr/Exord-init/engine/internal/protocol"
)

func TestInspectAdoptClassifiesInventoryGitTaskAndSecrets(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "AGENTS.md", "user rules\n")
	writeTestFile(t, dir, "TASK.md", "personal task notes\n")
	writeTestFile(t, dir, ".env", "EXAMPLE_ONLY=value\n")
	writeTestFile(t, dir, "src/main.go", "package main\n")
	if gitPath, err := exec.LookPath("git"); err == nil {
		command := exec.Command(gitPath, "-C", dir, "init", "-b", "main")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v: %s", err, output)
		}
	}
	inspection, err := InspectAdopt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Inventory.Files != 4 || inspection.Inventory.SecretCandidates != 1 {
		t.Fatalf("unexpected inventory: %#v", inspection.Inventory)
	}
	if inspection.Task.Status != "USER_OWNED" {
		t.Fatalf("unexpected task status %q", inspection.Task.Status)
	}
	if statusFor(inspection.Guidance, "AGENTS.md") != "USER_OWNED" {
		t.Fatal("existing AGENTS.md was not classified as user-owned")
	}
	if inspection.Fingerprint == "" {
		t.Fatal("missing adopt fingerprint")
	}
	if _, err := exec.LookPath("git"); err == nil {
		if _, kind, _, exists := inspection.Entry(".git"); !exists || kind != "GIT_DIRECTORY" {
			t.Fatalf("root Git boundary was not inventoried: exists=%v kind=%q", exists, kind)
		}
		if !inspection.Git.Detected || !inspection.Git.RootMatch {
			t.Fatalf("Git root was not detected: %#v", inspection.Git)
		}
	}
}

func TestInspectAdoptFingerprintChangesWithKnownFileContent(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "AGENTS.md", "first\n")
	first, err := InspectAdopt(dir)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, dir, "AGENTS.md", "other\n")
	second, err := InspectAdopt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint == second.Fingerprint {
		t.Fatal("content change did not change fingerprint")
	}
}

func TestInspectAdoptPreservesManifestIdentityAndManagedBaseline(t *testing.T) {
	dir := t.TempDir()
	content := "managed rules\n"
	writeTestFile(t, dir, "AGENTS.md", content)
	sum := sha256.Sum256([]byte(content))
	manifest := fmt.Sprintf(`{"schema_version":1,"project_id":"123e4567-e89b-42d3-a456-426614174000","managed_files":[{"path":"AGENTS.md","baseline_sha256":"%s"}]}`, hex.EncodeToString(sum[:]))
	writeTestFile(t, dir, ".exord/manifest.json", manifest)
	inspection, err := InspectAdopt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.ManifestProjectID != "123e4567-e89b-42d3-a456-426614174000" {
		t.Fatalf("manifest project identity was not preserved: %q", inspection.ManifestProjectID)
	}
	if statusFor(inspection.Guidance, "AGENTS.md") != "MANAGED_UNMODIFIED" {
		t.Fatal("matching managed baseline was not recognized")
	}
}

func TestInspectAdoptStopsAtNestedGitBoundary(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "nested/.git/HEAD", "ref: refs/heads/main\n")
	writeTestFile(t, dir, "nested/.env", "SECRET=value\n")
	inspection, err := InspectAdopt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Inventory.NestedGitRepositories != 1 {
		t.Fatalf("nested Git boundary was not counted: %#v", inspection.Inventory)
	}
	if inspection.Inventory.SecretCandidates != 0 {
		t.Fatal("nested repository contents must not be scanned")
	}
	if _, kind, _, exists := inspection.Entry("nested"); !exists || kind != "NESTED_GIT_DIRECTORY" {
		t.Fatalf("nested repository boundary missing: exists=%v kind=%q", exists, kind)
	}
}

func TestInspectAdoptDetectsRebaseDirectory(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git CLI is unavailable")
	}
	dir := t.TempDir()
	command := exec.Command(gitPath, "-C", dir, "init", "-b", "main")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git", "rebase-merge"), 0700); err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectAdopt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Git.InProgress != "REBASE" {
		t.Fatalf("rebase state was not detected: %#v", inspection.Git)
	}
}

func writeTestFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func statusFor(items []protocol.GuidanceAnalysis, path string) string {
	for _, item := range items {
		if item.Path == path {
			return item.Status
		}
	}
	return ""
}
