package state

import (
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/kimNarr/Exord-init/engine/internal/protocol"
)

// maxListedRuns bounds discovery so a pathological runs directory cannot make
// the engine walk an unbounded number of entries.
const maxListedRuns = 512

// RetainedRun is the bounded, safe summary of one leftover run bundle. It
// deliberately exposes only identity, coarse state, start time, and the single
// safe next action; plan hashes, staged paths, and per-operation detail stay
// behind `recover inspect`.
type RetainedRun struct {
	RunID         string `json:"run_id"`
	Status        string `json:"status"`
	Stage         string `json:"stage"`
	StartedAt     string `json:"started_at"`
	NextAction    string `json:"next_action"`
	BlocksNewPlan bool   `json:"blocks_new_plan"`
}

// ListRetainedRuns enumerates the run bundles under a project's local state.
// It never follows symlinks, ignores entries whose name is not a run UUID, and
// classifies each run from its journal alone. A journal it cannot parse or
// whose binding is wrong is reported as blocking (fail-closed), not skipped.
func ListRetainedRuns(projectRoot string) ([]RetainedRun, error) {
	runsDir := filepath.Join(projectRoot, "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []RetainedRun{}, nil
		}
		return nil, errors.New("cannot read local runs directory")
	}
	runs := make([]RetainedRun, 0, len(entries))
	for _, entry := range entries {
		if len(runs) >= maxListedRuns {
			break
		}
		name := entry.Name()
		if !isUUID(name) {
			continue
		}
		info, err := os.Lstat(filepath.Join(runsDir, name))
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		runs = append(runs, classifyRetainedRun(runsDir, name))
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].StartedAt != runs[j].StartedAt {
			return runs[i].StartedAt < runs[j].StartedAt
		}
		return runs[i].RunID < runs[j].RunID
	})
	return runs, nil
}

func classifyRetainedRun(runsDir, runID string) RetainedRun {
	run := RetainedRun{RunID: runID, Status: "UNKNOWN", Stage: "UNKNOWN", BlocksNewPlan: true}
	var journal protocol.RunJournal
	if err := readJSON(filepath.Join(runsDir, runID, "run.json"), &journal); err != nil {
		run.NextAction = "unrecognized run state; run exord-init recover inspect before planning a new change"
		return run
	}
	if journal.SchemaVersion != 1 || journal.RunID != runID {
		run.NextAction = "run journal binding mismatch; run exord-init recover inspect before planning a new change"
		return run
	}
	run.Status = journal.Status
	run.Stage = journal.Stage
	run.StartedAt = journal.StartedAt
	switch {
	case journal.Status == "RECOVERY_REQUIRED":
		run.NextAction = "run exord-init recover inspect and resolve this run before planning a new change"
	case journal.Status == "ACTIVE" && (journal.Stage == "FILES_APPLYING" || journal.Stage == "FILES_APPLIED" || journal.Stage == "VALIDATED"):
		run.NextAction = "this run was interrupted during apply; run exord-init recover inspect before planning a new change"
	case journal.Status == "ACTIVE" && (journal.Stage == "PLANNED" || journal.Stage == "APPROVED"):
		run.NextAction = "this plan was never applied; apply it, or leave it for a later discard command"
		run.BlocksNewPlan = false
	case journal.Status == "FAILED":
		run.NextAction = "rolled-back run retained as evidence for a later discard command"
		run.BlocksNewPlan = false
	case journal.Status == "FINALIZED":
		run.NextAction = "apply already completed; a later cleanup command will remove this bundle"
		run.BlocksNewPlan = false
	default:
		run.NextAction = "unrecognized run status; run exord-init recover inspect before planning a new change"
	}
	return run
}

// DiscardRun removes one run bundle directory. It refuses to act on a path that
// is not a plain directory (for example a symlink swapped in for the bundle),
// and reports whether anything was actually deleted so a partial removal is
// never reported as a clean discard. os.RemoveAll does not descend through
// symlinks, so a link planted inside the bundle cannot redirect the deletion.
func DiscardRun(projectRoot, runID string) (bool, error) {
	runDir, err := RunDirectory(projectRoot, runID)
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(runDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, errors.New("run bundle does not exist")
		}
		return false, errors.New("cannot inspect run bundle")
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("run bundle path is not a plain directory")
	}
	if err := os.RemoveAll(runDir); err != nil {
		// RemoveAll deletes bottom-up, so a failure may have already removed
		// staged files. Report the bundle as changed and let the caller
		// surface it as an incomplete discard rather than a clean one.
		return true, errors.New("cannot remove run bundle")
	}
	if _, statErr := os.Stat(runDir); !os.IsNotExist(statErr) {
		return true, errors.New("run bundle was only partially removed")
	}
	return true, nil
}

// BlockingRetainedRuns returns the subset of runs that must be resolved before
// a new mutating plan may proceed.
func BlockingRetainedRuns(runs []RetainedRun) []RetainedRun {
	blocking := make([]RetainedRun, 0, len(runs))
	for _, run := range runs {
		if run.BlocksNewPlan {
			blocking = append(blocking, run)
		}
	}
	return blocking
}
