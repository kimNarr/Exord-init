package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kimNarr/Exord-init/engine/internal/protocol"
)

func writeRunJournal(t *testing.T, projectRoot, runID string, journal protocol.RunJournal) {
	t.Helper()
	dir := filepath.Join(projectRoot, "runs", runID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

const (
	runA = "123e4567-e89b-42d3-a456-4266141740aa"
	runB = "123e4567-e89b-42d3-a456-4266141740bb"
	runC = "123e4567-e89b-42d3-a456-4266141740cc"
)

func journalFor(runID, status, stage, started string) protocol.RunJournal {
	return protocol.RunJournal{SchemaVersion: 1, RunID: runID, PlanID: runID, SpecSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Status: status, Stage: stage, Operations: []protocol.RunOperation{}, StartedAt: started, UpdatedAt: started}
}

func TestListRetainedRunsEmptyWhenNoRunsDirectory(t *testing.T) {
	runs, err := ListRetainedRuns(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected no runs, got %d", len(runs))
	}
}

func TestListRetainedRunsClassifiesAndOrders(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	writeRunJournal(t, root, runA, journalFor(runA, "FINALIZED", "FINALIZED", "2026-09-10T03:00:00Z"))
	writeRunJournal(t, root, runB, journalFor(runB, "ACTIVE", "PLANNED", "2026-09-10T01:00:00Z"))
	writeRunJournal(t, root, runC, journalFor(runC, "RECOVERY_REQUIRED", "FILES_APPLYING", "2026-09-10T02:00:00Z"))

	runs, err := ListRetainedRuns(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3 {
		t.Fatalf("expected 3 runs, got %d", len(runs))
	}
	if runs[0].RunID != runB || runs[1].RunID != runC || runs[2].RunID != runA {
		t.Fatalf("runs not ordered by start time: %+v", runs)
	}
	if runs[0].BlocksNewPlan || !runs[1].BlocksNewPlan || runs[2].BlocksNewPlan {
		t.Fatalf("unexpected blocking classification: %+v", runs)
	}
	if got := BlockingRetainedRuns(runs); len(got) != 1 || got[0].RunID != runC {
		t.Fatalf("expected only %s blocking, got %+v", runC, got)
	}
}

func TestListRetainedRunsBlocksInterruptedActiveRun(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	writeRunJournal(t, root, runA, journalFor(runA, "ACTIVE", "FILES_APPLIED", "2026-09-10T00:00:00Z"))
	runs, err := ListRetainedRuns(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || !runs[0].BlocksNewPlan {
		t.Fatalf("interrupted ACTIVE run must block a new plan: %+v", runs)
	}
}

func TestListRetainedRunsFailsClosedOnUnparseableJournal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	dir := filepath.Join(root, "runs", runA)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	runs, err := ListRetainedRuns(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || !runs[0].BlocksNewPlan || runs[0].Status != "UNKNOWN" {
		t.Fatalf("unparseable journal must be reported as blocking UNKNOWN: %+v", runs)
	}
}

func TestListRetainedRunsIgnoresNonRunEntries(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	writeRunJournal(t, root, runA, journalFor(runA, "FAILED", "ROLLED_BACK", "2026-09-10T00:00:00Z"))
	// A stray non-UUID directory and file must be ignored.
	if err := os.MkdirAll(filepath.Join(root, "runs", "not-a-uuid"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "runs", "README"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	runs, err := ListRetainedRuns(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].RunID != runA || runs[0].BlocksNewPlan {
		t.Fatalf("expected only the FAILED run, non-blocking: %+v", runs)
	}
}
