package apply

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	"github.com/kimNarr/Exord-init/engine/internal/recovery"
	"github.com/kimNarr/Exord-init/engine/internal/state"
	"github.com/kimNarr/Exord-init/engine/internal/target"
)

// crashFollowUp is what an operator does after the simulated crash.
type crashFollowUp int

const (
	followRollback crashFollowUp = iota // recover rollback with RECOVER_ROLLBACK
	followResume                        // apply the same approved plan again
	followDiscard                       // recover discard with RECOVER_DISCARD
)

// expectedCreateFiles is the operation set a codex-only CREATE + QUICK plan
// produces. The crash matrix below names these paths, so it fails fast if the
// planner output changes instead of silently skipping crash points.
var expectedCreateFiles = []string{".exord/manifest.json", "AGENTS.md", "docs/PROJECT.md"}

// TestCrashMatrixLeavesRecoverableState simulates a process crash at every
// injection point in apply and drives the documented recovery path from the
// exact on-disk state left behind. "Crash" means no journal write, rollback,
// or deferred cleanup runs after the injection point.
func TestCrashMatrixLeavesRecoverableState(t *testing.T) {
	cases := []struct {
		stage        string
		validator    validatorFunc
		status       string
		journalStage string
		ops          map[string]string // journal status per path; omitted paths are PENDING
		disposition  string
		blocks       bool
		follow       crashFollowUp
		// residue lists what remains in the target (outside .git) after the
		// follow-up. Directories end in "/", run-unique temp names are shown as
		// ".exord-create-*". Rollback never removes directories or temp files
		// that the journal does not record, so these are reported, not deleted.
		residue []string
	}{
		{
			stage: "after-approved", status: "ACTIVE", journalStage: "APPROVED",
			disposition: "NOT_RECOVERABLE", blocks: false, follow: followResume,
			residue: []string{".exord/", ".exord/manifest.json", "AGENTS.md", "docs/", "docs/PROJECT.md"},
		},
		{
			stage: "after-files-applying", status: "ACTIVE", journalStage: "FILES_APPLYING",
			disposition: "ROLLBACK_READY", blocks: true, follow: followRollback,
			residue: []string{},
		},
		{
			stage: "after-applying:AGENTS.md", status: "ACTIVE", journalStage: "FILES_APPLYING",
			ops:         map[string]string{"AGENTS.md": "APPLYING"},
			disposition: "ROLLBACK_READY", blocks: true, follow: followRollback,
			residue: []string{},
		},
		{
			stage: "after-temp-write:AGENTS.md", status: "ACTIVE", journalStage: "FILES_APPLYING",
			ops:         map[string]string{"AGENTS.md": "APPLYING"},
			disposition: "ROLLBACK_READY", blocks: true, follow: followRollback,
			residue: []string{".exord-create-*"},
		},
		{
			stage: "after-link:AGENTS.md", status: "ACTIVE", journalStage: "FILES_APPLYING",
			ops:         map[string]string{"AGENTS.md": "APPLYING"},
			disposition: "ROLLBACK_READY", blocks: true, follow: followRollback,
			residue: []string{".exord-create-*"},
		},
		{
			stage: "after-publish:AGENTS.md", status: "ACTIVE", journalStage: "FILES_APPLYING",
			ops:         map[string]string{"AGENTS.md": "APPLYING"},
			disposition: "ROLLBACK_READY", blocks: true, follow: followRollback,
			residue: []string{},
		},
		{
			stage: "after-temp-write:docs/PROJECT.md", status: "ACTIVE", journalStage: "FILES_APPLYING",
			ops:         map[string]string{"AGENTS.md": "APPLIED", "docs/PROJECT.md": "APPLYING"},
			disposition: "ROLLBACK_READY", blocks: true, follow: followRollback,
			residue: []string{"docs/", "docs/.exord-create-*"},
		},
		{
			stage: "after-publish:docs/PROJECT.md", status: "ACTIVE", journalStage: "FILES_APPLYING",
			ops:         map[string]string{"AGENTS.md": "APPLIED", "docs/PROJECT.md": "APPLYING"},
			disposition: "ROLLBACK_READY", blocks: true, follow: followRollback,
			residue: []string{"docs/"},
		},
		{
			stage: "before-validation", status: "ACTIVE", journalStage: "FILES_APPLYING",
			ops:         map[string]string{".exord/manifest.json": "APPLIED", "AGENTS.md": "APPLIED", "docs/PROJECT.md": "APPLIED"},
			disposition: "ROLLBACK_READY", blocks: true, follow: followRollback,
			residue: []string{".exord/", "docs/"},
		},
		{
			stage: "after-validated", status: "ACTIVE", journalStage: "VALIDATED",
			ops:         map[string]string{".exord/manifest.json": "APPLIED", "AGENTS.md": "APPLIED", "docs/PROJECT.md": "APPLIED"},
			disposition: "ROLLBACK_READY", blocks: true, follow: followRollback,
			residue: []string{".exord/", "docs/"},
		},
		{
			stage: "after-finalized", status: "FINALIZED", journalStage: "FINALIZED",
			ops:         map[string]string{".exord/manifest.json": "APPLIED", "AGENTS.md": "APPLIED", "docs/PROJECT.md": "APPLIED"},
			disposition: "CLEANUP_REQUIRED", blocks: false, follow: followDiscard,
			residue: []string{".exord/", ".exord/manifest.json", "AGENTS.md", "docs/", "docs/PROJECT.md"},
		},
		{
			// A declared validation fails, apply starts its own rollback, and
			// the process dies right after removing the newest file but before
			// journaling it or removing its directory.
			stage: "rollback-after-remove:.exord/manifest.json",
			validator: func(*os.Root, protocol.SetupPlan, []createdFile) error {
				return errors.New("validation rejected output")
			},
			status: "ACTIVE", journalStage: "FILES_APPLYING",
			ops:         map[string]string{".exord/manifest.json": "APPLIED", "AGENTS.md": "APPLIED", "docs/PROJECT.md": "APPLIED"},
			disposition: "ROLLBACK_READY", blocks: true, follow: followRollback,
			residue: []string{".exord/", "docs/"},
		},
	}

	for _, tc := range cases {
		t.Run(strings.NewReplacer("/", "_", ":", "_").Replace(tc.stage), func(t *testing.T) {
			targetPath, projectRoot, runID, prepared := prepareRun(t)
			requireExpectedCreateFiles(t, prepared.Plan)

			injected := errors.New("simulated crash")
			fired := false
			_, failure := execute(targetPath, projectRoot, runID, approvalFor(runID, prepared.Plan), func(stage string) error {
				if stage == tc.stage {
					fired = true
					return injected
				}
				return nil
			}, tc.validator)
			if !fired {
				t.Fatalf("injection point %q was never reached", tc.stage)
			}
			if failure == nil || failure.Code != "RECOVERY_REQUIRED" || !errors.Is(failure.Err, injected) {
				t.Fatalf("expected simulated crash failure, got %#v", failure)
			}

			// 1. The journal holds exactly what was durably written before the crash.
			_, _, journal, err := state.Load(projectRoot, runID)
			if err != nil {
				t.Fatalf("journal unreadable after crash: %v", err)
			}
			if journal.Status != tc.status || journal.Stage != tc.journalStage {
				t.Fatalf("journal = %s/%s, want %s/%s", journal.Status, journal.Stage, tc.status, tc.journalStage)
			}
			for _, operation := range journal.Operations {
				want := tc.ops[operation.Path]
				if want == "" {
					want = "PENDING"
				}
				if operation.Status != want {
					t.Fatalf("operation %s = %s, want %s", operation.Path, operation.Status, want)
				}
			}

			// 2. recover inspect classifies the leftover run.
			analysis, inspectFailure := recovery.Inspect(targetPath, projectRoot, runID)
			if inspectFailure != nil {
				t.Fatalf("inspect failed: %s: %v", inspectFailure.Code, inspectFailure.Err)
			}
			if analysis.Disposition != tc.disposition {
				t.Fatalf("disposition = %s, want %s (%#v)", analysis.Disposition, tc.disposition, analysis)
			}

			// 3. Pre-plan discovery blocks a new plan exactly when it must.
			runs, err := state.ListRetainedRuns(projectRoot)
			if err != nil || len(runs) != 1 {
				t.Fatalf("retained runs = %#v, %v", runs, err)
			}
			if runs[0].BlocksNewPlan != tc.blocks {
				t.Fatalf("blocks new plan = %v, want %v (%#v)", runs[0].BlocksNewPlan, tc.blocks, runs[0])
			}

			// 4. The documented follow-up completes from the crashed state.
			switch tc.follow {
			case followRollback:
				final, _, rollbackFailure := recovery.Rollback(targetPath, projectRoot, runID, recoveryApprovalFor(runID, prepared.Plan, "RECOVER_ROLLBACK"))
				if rollbackFailure != nil {
					t.Fatalf("rollback failed: %s: %v", rollbackFailure.Code, rollbackFailure.Err)
				}
				if final.Disposition != "ROLLED_BACK_RETAINED" {
					t.Fatalf("post-rollback disposition = %s", final.Disposition)
				}
				for _, path := range expectedCreateFiles {
					if _, err := os.Lstat(filepath.Join(targetPath, filepath.FromSlash(path))); !os.IsNotExist(err) {
						t.Fatalf("rollback left generated file %s: %v", path, err)
					}
				}
				runs, err := state.ListRetainedRuns(projectRoot)
				if err != nil || len(runs) != 1 || runs[0].BlocksNewPlan {
					t.Fatalf("rolled-back run must be retained but non-blocking: %#v, %v", runs, err)
				}
			case followResume:
				outcome, resumeFailure := Execute(targetPath, projectRoot, runID, approvalFor(runID, prepared.Plan))
				if resumeFailure != nil {
					t.Fatalf("resume failed: %s: %v", resumeFailure.Code, resumeFailure.Err)
				}
				if len(outcome.CreatedPaths) != len(expectedCreateFiles) {
					t.Fatalf("resume created %v", outcome.CreatedPaths)
				}
			case followDiscard:
				removed, discardFailure := recovery.Discard(targetPath, projectRoot, runID, recoveryApprovalFor(runID, prepared.Plan, "RECOVER_DISCARD"))
				if discardFailure != nil || !removed {
					t.Fatalf("discard failed: removed=%v %#v", removed, discardFailure)
				}
			}

			// 5. Exactly the documented residue remains in the target.
			if got := targetResidue(t, targetPath); !reflect.DeepEqual(got, tc.residue) {
				t.Fatalf("target residue = %v, want %v", got, tc.residue)
			}
		})
	}
}

// TestCrashResidueBlocksFreshCreatePlan records the operator-visible effect of
// leftover temp files and directories: a fresh CREATE plan against the same
// target is refused until they are removed by hand.
func TestCrashResidueBlocksFreshCreatePlan(t *testing.T) {
	targetPath, projectRoot, runID, prepared := prepareRun(t)
	_, failure := execute(targetPath, projectRoot, runID, approvalFor(runID, prepared.Plan), func(stage string) error {
		if stage == "after-publish:docs/PROJECT.md" {
			return errors.New("simulated crash")
		}
		return nil
	}, nil)
	if failure == nil {
		t.Fatal("expected simulated crash")
	}
	if _, _, rollbackFailure := recovery.Rollback(targetPath, projectRoot, runID, recoveryApprovalFor(runID, prepared.Plan, "RECOVER_ROLLBACK")); rollbackFailure != nil {
		t.Fatalf("rollback failed: %#v", rollbackFailure)
	}
	if _, err := target.InspectCreate(targetPath); err == nil {
		t.Fatal("empty generated directory unexpectedly passes CREATE inspection; update the crash residue documentation")
	}
	if err := os.Remove(filepath.Join(targetPath, "docs")); err != nil {
		t.Fatal(err)
	}
	if _, err := target.InspectCreate(targetPath); err != nil {
		t.Fatalf("target should be plannable once residue is removed: %v", err)
	}
}

// TestCrashMatrixCoversEveryInjectionPoint fails when apply gains an injection
// point kind that the crash matrix does not exercise.
func TestCrashMatrixCoversEveryInjectionPoint(t *testing.T) {
	targetPath, projectRoot, runID, prepared := prepareRun(t)
	seen := map[string]bool{}
	if _, failure := execute(targetPath, projectRoot, runID, approvalFor(runID, prepared.Plan), func(stage string) error {
		kind, _, _ := strings.Cut(stage, ":")
		seen[kind] = true
		return nil
	}, nil); failure != nil {
		t.Fatalf("recording apply failed: %#v", failure)
	}
	got := make([]string, 0, len(seen))
	for kind := range seen {
		got = append(got, kind)
	}
	sort.Strings(got)
	// rollback-after-remove only fires on a failing apply and is covered by
	// its own matrix case.
	want := []string{"after-applying", "after-approved", "after-files-applying", "after-finalized", "after-link", "after-publish", "after-temp-write", "after-validated", "before-validation"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("injection point kinds = %v, want %v; add matrix cases for new kinds", got, want)
	}
}

func TestSimulatedCrashIsNeverReachableFromExecute(t *testing.T) {
	// Execute must not accept or construct an injector; a nil injector never
	// crashes at any stage.
	var inject faultInjector
	if err := inject.at("after-approved"); err != nil {
		t.Fatalf("nil injector produced %v", err)
	}
	targetPath, projectRoot, runID, prepared := prepareRun(t)
	if _, failure := Execute(targetPath, projectRoot, runID, approvalFor(runID, prepared.Plan)); failure != nil {
		t.Fatalf("production apply failed: %#v", failure)
	}
}

func requireExpectedCreateFiles(t *testing.T, plan protocol.SetupPlan) {
	t.Helper()
	paths := make([]string, 0, len(plan.Spec.Operations))
	for _, operation := range plan.Spec.Operations {
		paths = append(paths, operation.Path)
	}
	sort.Strings(paths)
	if !reflect.DeepEqual(paths, expectedCreateFiles) {
		t.Fatalf("CREATE plan operations changed to %v; update the crash matrix", paths)
	}
}

func targetResidue(t *testing.T, targetPath string) []string {
	t.Helper()
	residue := []string{}
	err := filepath.WalkDir(targetPath, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == targetPath {
			return nil
		}
		relative, err := filepath.Rel(targetPath, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == ".git" {
			return filepath.SkipDir
		}
		if strings.HasPrefix(entry.Name(), ".exord-create-") {
			relative = strings.TrimSuffix(relative, entry.Name()) + ".exord-create-*"
		}
		if entry.IsDir() {
			relative += "/"
		}
		residue = append(residue, relative)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(residue)
	return residue
}

func recoveryApprovalFor(runID string, plan protocol.SetupPlan, action string) protocol.Approval {
	return protocol.Approval{SchemaVersion: 1, RunID: runID, PlanID: plan.PlanID, SpecSHA256: plan.SpecSHA256, TargetIdentitySHA256: plan.Spec.TargetIdentitySHA256, ApprovedAction: action, ApprovedAt: time.Now().UTC().Format(time.RFC3339)}
}
