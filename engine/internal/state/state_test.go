package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kimNarr/Exord-init/engine/internal/planner"
	"github.com/kimNarr/Exord-init/engine/internal/protocol"
)

const testRunID = "123e4567-e89b-42d3-a456-426614174000"

func TestLockIsExclusiveAndReleasable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	first, err := Acquire(root, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(root, testRunID); !errors.Is(err, ErrLocked) {
		t.Fatalf("expected ErrLocked, got %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(root, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestLockTakesOverOrphanLockFileFromCrashedRun(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	// A crashed run leaves lock.json behind but holds no OS advisory lock,
	// because the kernel drops it when the process exits.
	orphan := []byte(`{"schema_version":1,"run_id":"123e4567-e89b-42d3-a456-426614174011","pid":999999,"owner_token":"123e4567-e89b-42d3-a456-426614174012","created_at":"2026-09-10T00:00:00Z"}`)
	if err := os.WriteFile(filepath.Join(root, "lock.json"), orphan, 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := Acquire(root, testRunID)
	if err != nil {
		t.Fatalf("Acquire must reclaim an orphaned lock file, got %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release after takeover failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "lock.json")); !os.IsNotExist(err) {
		t.Fatalf("lock file should be removed after release: %v", err)
	}
}

func TestLockReleasedWhenHandleClosesWithoutRelease(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	lock, err := Acquire(root, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a crash: the process drops the handle without calling Release.
	if err := lock.file.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := Acquire(root, testRunID)
	if err != nil {
		t.Fatalf("a dropped handle must free the lock, got %v", err)
	}
	if err := next.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestLockReleasePreservesReplacementOwnedByAnotherProcess(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	lock, err := Acquire(root, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	replacement := []byte(`{"schema_version":1,"run_id":"123e4567-e89b-42d3-a456-426614174099","pid":1,"owner_token":"123e4567-e89b-42d3-a456-426614174098","created_at":"2026-09-10T00:00:00Z"}`)
	if err := lock.file.Truncate(0); err != nil {
		t.Fatal(err)
	}
	if _, err := lock.file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := lock.file.Write(replacement); err != nil {
		t.Fatal(err)
	}
	if err := lock.Release(); err == nil {
		t.Fatal("release accepted a replaced lock")
	}
	if _, err := os.Stat(filepath.Join(root, "lock.json")); err != nil {
		t.Fatalf("replacement lock was removed: %v", err)
	}
}

func TestPersistLoadAndReplaceJournal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	prepared := planner.PreparedPlan{
		Plan:  protocol.SetupPlan{SchemaVersion: 1, PlanID: "123e4567-e89b-42d3-a456-426614174001", SpecSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Spec: protocol.PlanSpec{Operations: []protocol.Operation{{Kind: "CREATE", Path: "docs/PROJECT.md"}}}},
		Files: []planner.PreparedFile{{Path: "docs/PROJECT.md", Content: []byte("hello\n")}},
	}
	journal, err := PersistPrepared(root, testRunID, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if journal.Stage != "PLANNED" {
		t.Fatalf("unexpected initial stage %q", journal.Stage)
	}
	runDir, loadedPlan, loadedJournal, err := Load(root, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	if loadedPlan.PlanID != prepared.Plan.PlanID || loadedJournal.RunID != testRunID {
		t.Fatal("stored run binding changed")
	}
	staged, err := StagedPath(runDir, "docs/PROJECT.md")
	if err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(staged); err != nil || string(content) != "hello\n" {
		t.Fatalf("unexpected staged content %q: %v", content, err)
	}
	loadedJournal.Stage = "APPROVED"
	if err := WriteJournal(runDir, loadedJournal); err != nil {
		t.Fatal(err)
	}
	_, _, reloaded, err := Load(root, testRunID)
	if err != nil || reloaded.Stage != "APPROVED" {
		t.Fatalf("atomic journal replacement failed: %v", err)
	}
}

func TestStagedPathRejectsEscape(t *testing.T) {
	if _, err := StagedPath(t.TempDir(), "../outside"); err == nil {
		t.Fatal("expected path escape rejection")
	}
}

func TestProjectIDIsRandomThenStable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	first, err := ProjectID(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ProjectID(root)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second {
		t.Fatalf("project identity was not stable: %q != %q", first, second)
	}
}

func TestExternalProjectRootRejectsInvalidIdentity(t *testing.T) {
	if _, err := ExternalProjectRoot("not-a-hash"); err == nil {
		t.Fatal("expected invalid identity rejection")
	}
}
