package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"

	exordinit "github.com/kimNarr/Exord-init"
	applyengine "github.com/kimNarr/Exord-init/engine/internal/apply"
	approvalcontract "github.com/kimNarr/Exord-init/engine/internal/approval"
	"github.com/kimNarr/Exord-init/engine/internal/id"
	"github.com/kimNarr/Exord-init/engine/internal/planner"
	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	recoveryengine "github.com/kimNarr/Exord-init/engine/internal/recovery"
	"github.com/kimNarr/Exord-init/engine/internal/state"
	"github.com/kimNarr/Exord-init/engine/internal/target"
)

var version = "0.4.0-dev"

const protocolVersion = 1

// engineCapabilities is the single source of truth for the capability list
// reported by `doctor`. README tables and Skill references must mirror this
// exact set; contract tests assert the READMEs stay in sync.
var engineCapabilities = []string{
	"doctor",
	"plan:create-quick",
	"apply:create-quick",
	"plan:adopt-quick",
	"plan:upgrade",
	"recover:inspect",
	"recover:rollback",
	"recover:list",
	"recover:discard",
}

func main() {
	if len(os.Args) < 2 {
		emit(failure("", "INVALID_INTENT", "error.command_required", nil), 2)
	}
	switch os.Args[1] {
	case "doctor":
		runDoctor(os.Args[2:])
	case "plan":
		runPlan(os.Args[2:])
	case "apply":
		runApply(os.Args[2:])
	case "recover":
		runRecover(os.Args[2:])
	default:
		emit(failure(os.Args[1], "INVALID_INTENT", "error.unknown_command", nil), 2)
	}
}

func runDoctor(args []string) {
	set := flag.NewFlagSet("doctor", flag.ContinueOnError)
	jsonOutput := set.Bool("json", false, "emit JSON (output is always JSON)")
	set.SetOutput(os.Stderr)
	if err := set.Parse(args); err != nil {
		emit(failure("doctor", "INVALID_INTENT", "error.invalid_flags", nil), 2)
	}
	_ = jsonOutput
	for _, language := range []string{"en", "ko"} {
		for _, name := range []string{"AGENTS.md", "PROJECT.md", "CLAUDE.md", "GEMINI.md"} {
			if _, err := exordinit.RenderTemplate(language, name, exordinit.TemplateData{ProjectSummary: "doctor"}); err != nil {
				emit(failure("doctor", "INTERNAL_ERROR", "error.embedded_template", err), 10)
			}
		}
	}
	result := protocol.Result{
		SchemaVersion: 1, Command: "doctor", Status: "OK", Code: "OK", MessageKey: "doctor.ok",
		Changed: false, Warnings: []string{},
		NextActions: []string{"prepare a CREATE + QUICK intent", "run exord-init plan"},
		Data:        map[string]any{"version": version, "protocol_version": protocolVersion, "goos": runtime.GOOS, "goarch": runtime.GOARCH, "capabilities": engineCapabilities},
	}
	emit(result, 0)
}

func runPlan(args []string) {
	set := flag.NewFlagSet("plan", flag.ContinueOnError)
	intentPath := set.String("intent", "", "path to SetupIntent JSON")
	targetPath := set.String("target", "", "target project directory")
	upgrade := set.Bool("upgrade", false, "produce a read-only upgrade plan for an existing exord-init project")
	expectedProtocol := set.Int("protocol-version", protocolVersion, "protocol version expected by the adapter")
	jsonOutput := set.Bool("json", false, "emit JSON (output is always JSON)")
	set.SetOutput(os.Stderr)
	if err := set.Parse(args); err != nil || *targetPath == "" || (!*upgrade && *intentPath == "") {
		emit(failure("plan", "INVALID_INTENT", "error.plan_flags", nil), 2)
	}
	_ = jsonOutput
	if *expectedProtocol != protocolVersion {
		emit(failure("plan", "VERSION_MISMATCH", "error.protocol_version", nil), 7)
	}
	if *upgrade {
		runUpgradePlan(*targetPath)
		return
	}
	raw, err := os.ReadFile(*intentPath)
	if err != nil {
		emit(failure("plan", "INVALID_INTENT", "error.intent_read", err), 2)
	}
	intent, err := planner.DecodeIntent(raw)
	if err != nil {
		emit(failure("plan", "INVALID_INTENT", "error.intent_invalid", err), 2)
	}
	if intent.Depth != "QUICK" {
		emit(failure("plan", "UNSUPPORTED_CAPABILITY", "error.v03_scope", nil), 7)
	}
	if intent.Mode == "ADOPT" {
		runAdoptPlan(intent, *targetPath)
		return
	}
	if intent.Mode != "CREATE" {
		emit(failure("plan", "UNSUPPORTED_CAPABILITY", "error.v03_scope", nil), 7)
	}
	inspection, err := target.InspectCreate(*targetPath)
	if err != nil {
		emit(failure("plan", "UNSUPPORTED_TARGET", "error.target_unsafe", err), 4)
	}
	runID, err := id.UUID4()
	if err != nil {
		emit(failure("plan", "INTERNAL_ERROR", "error.run_id", err), 10)
	}
	projectRoot, err := state.ProjectRoot(inspection.Path, inspection.IdentitySHA256)
	if err != nil {
		emitStateError("plan", err)
	}
	lock, err := state.Acquire(projectRoot, runID)
	if err != nil {
		emitStateError("plan", err)
	}
	retained, err := state.ListRetainedRuns(projectRoot)
	if err != nil {
		_ = lock.Release()
		emit(failure("plan", "INTERNAL_ERROR", "error.local_state", err), 10)
	}
	if blocking := state.BlockingRetainedRuns(retained); len(blocking) > 0 {
		// Nothing has been persisted for this run yet; just release and report.
		_ = lock.Release()
		emitBlockedByRetainedRuns("plan", blocking)
	}
	projectID, err := state.ProjectID(projectRoot)
	if err != nil {
		_ = lock.Release()
		emit(failure("plan", "INTERNAL_ERROR", "error.project_identity", err), 10)
	}
	prepared, err := planner.PrepareWithProjectID(intent, inspection, projectID)
	if err != nil {
		_ = lock.Release()
		emit(failure("plan", "INTERNAL_ERROR", "error.plan_build", err), 10)
	}
	if _, err := state.PersistPrepared(projectRoot, runID, prepared); err != nil {
		_ = state.RemoveRun(projectRoot, runID)
		_ = lock.Release()
		emit(failure("plan", "INTERNAL_ERROR", "error.plan_persist", err), 10)
	}
	warnings := []string{"the target project is unchanged; an approval-bound plan and staged files are stored in local run state"}
	warnings = append(warnings, retainedRunWarnings(retained)...)
	if err := lock.Release(); err != nil {
		// The plan and staged bytes are persisted; only the diagnostic lock
		// file could not be removed. The OS advisory lock is already gone, so
		// the leftover file is harmless and the next run reuses it.
		warnings = append(warnings, "the local lock file could not be removed and is safe to delete manually")
	}
	plan := prepared.Plan
	result := protocol.Result{
		SchemaVersion: 1, Command: "plan", Status: "OK", Code: "OK", MessageKey: "plan.ready",
		RunID: &runID, PlanID: &plan.PlanID, SpecSHA256: &plan.SpecSHA256, Changed: false,
		Warnings:    warnings,
		NextActions: []string{"review the plan", "create an approval document bound to run_id, plan_id, spec_sha256, and target_identity_sha256", "run exord-init apply"},
		Data: map[string]any{
			"inspection":       inspection,
			"plan":             plan,
			"approval_request": map[string]any{"schema_version": 1, "run_id": runID, "plan_id": plan.PlanID, "spec_sha256": plan.SpecSHA256, "target_identity_sha256": plan.Spec.TargetIdentitySHA256, "approved_action": "APPLY_CREATE"},
		},
	}
	emit(result, 0)
}

func runAdoptPlan(intent protocol.SetupIntent, targetPath string) {
	inspection, err := target.InspectAdopt(targetPath)
	if err != nil {
		emit(failure("plan", "UNSUPPORTED_TARGET", "error.target_unsafe", err), 4)
	}
	if inspection.Git.Detected && (!inspection.Git.CLIAvailable || !inspection.Git.RootMatch || inspection.Git.Status == "INVALID_REPOSITORY" || inspection.Git.Status == "STATUS_UNAVAILABLE") {
		emit(failure("plan", "UNSUPPORTED_TARGET", "error.git_state_unsafe", nil), 4)
	}
	lockFileRetained := false
	projectID := inspection.ManifestProjectID
	if projectID == "" {
		projectRoot, err := state.ExternalProjectRoot(inspection.IdentitySHA256)
		if err != nil {
			emitStateError("plan", err)
		}
		lockID, err := id.UUID4()
		if err != nil {
			emit(failure("plan", "INTERNAL_ERROR", "error.run_id", err), 10)
		}
		lock, err := state.Acquire(projectRoot, lockID)
		if err != nil {
			emitStateError("plan", err)
		}
		projectID, err = state.ProjectID(projectRoot)
		if err != nil {
			_ = lock.Release()
			emit(failure("plan", "INTERNAL_ERROR", "error.project_identity", err), 10)
		}
		if err := lock.Release(); err != nil {
			// The project ID is persisted; only the diagnostic lock file
			// lingers, and it holds no OS lock.
			lockFileRetained = true
		}
	}
	plan, err := planner.BuildAdopt(intent, inspection, projectID)
	if err != nil {
		emit(failure("plan", "INTERNAL_ERROR", "error.plan_build", err), 10)
	}
	warnings := []string{"read-only ADOPT analysis; existing project files and Git state were not changed"}
	if lockFileRetained {
		warnings = append(warnings, "the local lock file could not be removed and is safe to delete manually")
	}
	// ADOPT does not persist a run, so retained runs never block it, but a
	// leftover interrupted CREATE run on the same directory is worth surfacing.
	if createRoot, rootErr := state.ProjectRoot(inspection.Path, inspection.IdentitySHA256); rootErr == nil {
		if retained, listErr := state.ListRetainedRuns(createRoot); listErr == nil {
			warnings = append(warnings, retainedRunWarnings(retained)...)
		}
	}
	if inspection.Inventory.SecretCandidates > 0 {
		warnings = append(warnings, "possible secret-bearing files were detected; commit proposals must remain suspended until reviewed")
	}
	if inspection.Inventory.SecretScanLimited {
		warnings = append(warnings, "secret content scanning reached a safety bound; commit proposals must remain suspended")
	}
	if inspection.Inventory.ExcludedDirectories > 0 || inspection.Inventory.UnhashedFiles > 0 {
		warnings = append(warnings, "excluded dependency directories or files beyond hashing bounds were summarized without full content hashing")
	}
	if inspection.Inventory.NestedGitRepositories > 0 || inspection.Git.GitFile {
		warnings = append(warnings, "nested Git or worktree boundaries were detected and left untouched")
	}
	result := protocol.Result{
		SchemaVersion: 1, Command: "plan", Status: "OK", Code: "OK", MessageKey: "plan.adopt_ready",
		PlanID: &plan.PlanID, SpecSHA256: &plan.SpecSHA256, Changed: false,
		Warnings:    warnings,
		NextActions: []string{"review inventory and Git/Task analysis", "classify every conflict as diff, skip, alternate path, keep, or review update", "prepare a new plan after resolving required choices"},
		Data:        map[string]any{"inspection": inspection, "plan": plan},
	}
	emit(result, 0)
}

func runUpgradePlan(targetPath string) {
	inspection, err := target.InspectUpgrade(targetPath)
	if err != nil {
		if errors.Is(err, target.ErrManifestSchemaUnsupported) {
			emit(failure("plan", "VERSION_MISMATCH", "error.manifest_schema", err), 7)
		}
		if errors.Is(err, target.ErrNotExordProject) {
			emit(failure("plan", "UNSUPPORTED_TARGET", "error.not_exord_project", err), 4)
		}
		emit(failure("plan", "UNSUPPORTED_TARGET", "error.target_unsafe", err), 4)
	}
	plan, err := planner.BuildUpgrade(inspection)
	if err != nil {
		emit(failure("plan", "INTERNAL_ERROR", "error.plan_build", err), 10)
	}
	analysis := plan.Spec.UpgradeAnalysis
	warnings := []string{"read-only upgrade analysis; the project was not modified"}
	switch analysis.Disposition {
	case "BLOCKED":
		warnings = append(warnings, "a managed file or the generator version blocks an automatic upgrade; resolve it before any upgrade apply")
	case "REVIEW_REQUIRED":
		warnings = append(warnings, "one or more managed files need manual review before an upgrade")
	}
	if analysis.Generator.Direction == "UNKNOWN" {
		warnings = append(warnings, "the manifest generator version could not be parsed; the upgrade direction is unknown")
	}
	if !analysis.GitConsistent {
		warnings = append(warnings, "the manifest git_mode no longer matches the target's Git state")
	}
	if createRoot, rootErr := state.ProjectRoot(inspection.Path, inspection.IdentitySHA256); rootErr == nil {
		if retained, listErr := state.ListRetainedRuns(createRoot); listErr == nil {
			warnings = append(warnings, retainedRunWarnings(retained)...)
		}
	}
	result := protocol.Result{
		SchemaVersion: 1, Command: "plan", Status: "OK", Code: "OK", MessageKey: "plan.upgrade_ready",
		PlanID: &plan.PlanID, SpecSHA256: &plan.SpecSHA256, Changed: false,
		Warnings:    warnings,
		NextActions: []string{"review every managed-file upgrade action and the generator direction", "upgrade apply is not implemented in the current slice"},
		Data:        map[string]any{"plan": plan},
	}
	emit(result, 0)
}

func runApply(args []string) {
	set := flag.NewFlagSet("apply", flag.ContinueOnError)
	targetPath := set.String("target", "", "target project directory")
	runID := set.String("run-id", "", "run identifier returned by plan")
	approvalPath := set.String("approval", "", "path to an approval JSON document")
	expectedProtocol := set.Int("protocol-version", protocolVersion, "protocol version expected by the adapter")
	jsonOutput := set.Bool("json", false, "emit JSON (output is always JSON)")
	set.SetOutput(os.Stderr)
	if err := set.Parse(args); err != nil || *targetPath == "" || *runID == "" || *approvalPath == "" {
		emit(failure("apply", "INVALID_INTENT", "error.apply_flags", nil), 2)
	}
	_ = jsonOutput
	if *expectedProtocol != protocolVersion {
		emit(failure("apply", "VERSION_MISMATCH", "error.protocol_version", nil), 7)
	}
	raw, err := os.ReadFile(*approvalPath)
	if err != nil {
		emit(failure("apply", "INVALID_INTENT", "error.approval_read", err), 2)
	}
	approval, err := applyengine.DecodeApproval(raw)
	if err != nil {
		emit(failure("apply", "INVALID_INTENT", "error.approval_invalid", err), 2)
	}
	inspection, err := target.InspectCreate(*targetPath)
	if err != nil {
		emit(failure("apply", "PLAN_STALE", "error.target_changed", err), 5)
	}
	projectRoot, err := state.ProjectRoot(inspection.Path, inspection.IdentitySHA256)
	if err != nil {
		emitStateError("apply", err)
	}
	lock, err := state.Acquire(projectRoot, *runID)
	if err != nil {
		emitStateError("apply", err)
	}
	outcome, applyFailure := applyengine.Execute(*targetPath, projectRoot, *runID, approval)
	if applyFailure != nil {
		_ = lock.Release()
		emit(failure("apply", applyFailure.Code, "error.apply_failed", applyFailure.Err), mapApplyExit(applyFailure.Code))
	}
	warnings := []string{}
	if err := lock.Release(); err != nil {
		// Execute already finalized the run and removed the run bundle, so
		// there is nothing to recover. The OS advisory lock is released; only
		// the diagnostic lock file could not be deleted.
		warnings = append(warnings, "apply completed; the local lock file could not be removed and is safe to delete manually")
	}
	if outcome.CleanupPending {
		warnings = append(warnings, "generated files are valid, but finalized local run state could not be removed")
	}
	result := protocol.Result{
		SchemaVersion: 1, Command: "apply", Status: "OK", Code: "OK", MessageKey: "apply.complete",
		RunID: runID, PlanID: &outcome.PlanID, SpecSHA256: &outcome.SpecSHA256, Changed: true,
		Warnings: warnings, NextActions: []string{"review generated project guidance", "start the first implementation task"},
		Data: map[string]any{"created_paths": outcome.CreatedPaths, "cleanup_pending": outcome.CleanupPending},
	}
	emit(result, 0)
}

func runRecover(args []string) {
	if len(args) == 0 || (args[0] != "inspect" && args[0] != "rollback" && args[0] != "list" && args[0] != "discard") {
		emit(failure("recover", "INVALID_INTENT", "error.recover_action", nil), 2)
	}
	action := args[0]
	if action == "list" {
		runRecoverList(args[1:])
		return
	}
	needsApproval := action == "rollback" || action == "discard"
	set := flag.NewFlagSet("recover "+action, flag.ContinueOnError)
	targetPath := set.String("target", "", "target project directory")
	runID := set.String("run-id", "", "run identifier to inspect, roll back, or discard")
	approvalPath := set.String("approval", "", "path to a recovery approval JSON document")
	expectedProtocol := set.Int("protocol-version", protocolVersion, "protocol version expected by the adapter")
	jsonOutput := set.Bool("json", false, "emit JSON (output is always JSON)")
	set.SetOutput(os.Stderr)
	if err := set.Parse(args[1:]); err != nil || *targetPath == "" || *runID == "" || (needsApproval && *approvalPath == "") {
		emit(failure("recover", "INVALID_INTENT", "error.recover_flags", nil), 2)
	}
	_ = jsonOutput
	if *expectedProtocol != protocolVersion {
		emit(failure("recover", "VERSION_MISMATCH", "error.protocol_version", nil), 7)
	}
	var recoveryApproval protocol.Approval
	if needsApproval {
		wantAction := "RECOVER_ROLLBACK"
		if action == "discard" {
			wantAction = "RECOVER_DISCARD"
		}
		raw, err := os.ReadFile(*approvalPath)
		if err != nil {
			emit(failure("recover", "INVALID_INTENT", "error.approval_read", err), 2)
		}
		recoveryApproval, err = approvalcontract.Decode(raw)
		if err != nil || recoveryApproval.ApprovedAction != wantAction {
			emit(failure("recover", "INVALID_INTENT", "error.approval_invalid", err), 2)
		}
	}
	identity, err := target.InspectIdentity(*targetPath)
	if err != nil {
		emit(failure("recover", "UNSUPPORTED_TARGET", "error.target_unsafe", err), 4)
	}
	projectRoot, err := state.ProjectRoot(identity.Path, identity.IdentitySHA256)
	if err != nil {
		emitStateError("recover", err)
	}
	if info, err := os.Stat(projectRoot); err != nil || !info.IsDir() {
		emit(failure("recover", "PLAN_INVALID", "error.recovery_state_missing", err), 5)
	}
	lock, err := state.Acquire(projectRoot, *runID)
	if err != nil {
		emitStateError("recover", err)
	}
	// A Release failure only means the diagnostic lock file could not be
	// removed; the OS advisory lock is already gone. It never turns a
	// read-only inspect or a completed rollback into a recovery-required
	// state, so it is surfaced as a warning.
	const lockFileWarning = "the local lock file could not be removed and is safe to delete manually"
	if action == "inspect" {
		analysis, recoveryFailure := recoveryengine.Inspect(identity.Path, projectRoot, *runID)
		if recoveryFailure != nil {
			_ = lock.Release()
			emitRecoveryFailure(recoveryFailure)
		}
		warnings := recoveryWarnings(analysis)
		if err := lock.Release(); err != nil {
			warnings = append(warnings, lockFileWarning)
		}
		result := protocol.Result{SchemaVersion: 1, Command: "recover", Status: "OK", Code: "OK", MessageKey: "recover.inspected", RunID: &analysis.RunID, PlanID: &analysis.PlanID, SpecSHA256: &analysis.SpecSHA256, Changed: false, Warnings: warnings, NextActions: recoveryNextActions(analysis), Data: map[string]any{"recovery": analysis}}
		emit(result, 0)
	}
	if action == "discard" {
		removed, discardFailure := recoveryengine.Discard(identity.Path, projectRoot, *runID, recoveryApproval)
		if discardFailure != nil {
			_ = lock.Release()
			exitCode := 5
			if discardFailure.Code == "DISCARD_INCOMPLETE" {
				exitCode = 6
			} else if discardFailure.Code == "DISCARD_NOT_ALLOWED" {
				exitCode = 4
			}
			result := failure("recover", discardFailure.Code, "error.recovery_failed", discardFailure.Err)
			result.Changed = discardFailure.Changed
			result.NextActions = []string{"run exord-init recover list for the current disposition", "roll back or finish recovery before discarding a non-settled run"}
			emit(result, exitCode)
		}
		warnings := []string{}
		if err := lock.Release(); err != nil {
			warnings = append(warnings, lockFileWarning)
		}
		result := protocol.Result{
			SchemaVersion: 1, Command: "recover", Status: "OK", Code: "OK", MessageKey: "recover.discarded",
			RunID: runID, Changed: removed, Warnings: warnings,
			NextActions: []string{"run exord-init recover list to confirm the remaining runs"},
			Data:        map[string]any{"discarded_run_id": *runID, "removed": removed},
		}
		emit(result, 0)
	}
	analysis, changed, recoveryFailure := recoveryengine.Rollback(identity.Path, projectRoot, *runID, recoveryApproval)
	if recoveryFailure != nil {
		_ = lock.Release()
		emitRecoveryFailure(recoveryFailure)
	}
	warnings := []string{"the failed run bundle was retained for explicit later disposition"}
	if err := lock.Release(); err != nil {
		warnings = append(warnings, lockFileWarning)
	}
	result := protocol.Result{SchemaVersion: 1, Command: "recover", Status: "OK", Code: "OK", MessageKey: "recover.rolled_back", RunID: runID, PlanID: &analysis.PlanID, SpecSHA256: &analysis.SpecSHA256, Changed: changed, Warnings: warnings, NextActions: []string{"review the retained FAILED run before any later discard action", "create a fresh plan before retrying initialization"}, Data: map[string]any{"recovery": analysis}}
	emit(result, 0)
}

func runRecoverList(args []string) {
	set := flag.NewFlagSet("recover list", flag.ContinueOnError)
	targetPath := set.String("target", "", "target project directory")
	expectedProtocol := set.Int("protocol-version", protocolVersion, "protocol version expected by the adapter")
	jsonOutput := set.Bool("json", false, "emit JSON (output is always JSON)")
	set.SetOutput(os.Stderr)
	if err := set.Parse(args); err != nil || *targetPath == "" {
		emit(failure("recover", "INVALID_INTENT", "error.recover_flags", nil), 2)
	}
	_ = jsonOutput
	if *expectedProtocol != protocolVersion {
		emit(failure("recover", "VERSION_MISMATCH", "error.protocol_version", nil), 7)
	}
	identity, err := target.InspectIdentity(*targetPath)
	if err != nil {
		emit(failure("recover", "UNSUPPORTED_TARGET", "error.target_unsafe", err), 4)
	}
	projectRoot, err := state.ProjectRoot(identity.Path, identity.IdentitySHA256)
	if err != nil {
		emitStateError("recover", err)
	}
	// Discovery is read-only and does not take the project lock, so it still
	// works while another run holds it. Journals are written atomically, so a
	// concurrent read is consistent.
	runs, err := state.ListRetainedRuns(projectRoot)
	if err != nil {
		emit(failure("recover", "INTERNAL_ERROR", "error.local_state", err), 10)
	}
	blocking := state.BlockingRetainedRuns(runs)
	warnings := []string{}
	nextActions := []string{"no retained runs; a new plan can proceed"}
	if len(runs) > 0 {
		nextActions = []string{"run exord-init recover inspect on each run that needs attention"}
	}
	if len(blocking) > 0 {
		warnings = append(warnings, "one or more retained runs must be inspected and resolved before a new mutating plan")
	}
	result := protocol.Result{
		SchemaVersion: 1, Command: "recover", Status: "OK", Code: "OK", MessageKey: "recover.listed",
		Changed: false, Warnings: warnings, NextActions: nextActions,
		Data: map[string]any{"retained_runs": runs, "blocking_runs": len(blocking)},
	}
	emit(result, 0)
}

func retainedRunWarnings(runs []state.RetainedRun) []string {
	warnings := make([]string, 0, len(runs))
	for _, run := range runs {
		warnings = append(warnings, "retained run "+run.RunID+" ("+run.Status+"/"+run.Stage+"): "+run.NextAction)
	}
	return warnings
}

func emitBlockedByRetainedRuns(command string, blocking []state.RetainedRun) {
	result := protocol.Result{
		SchemaVersion: 1, Command: command, Status: "BLOCKED", Code: "RECOVERY_REQUIRED",
		MessageKey: "plan.blocked_retained_runs", Changed: false,
		Warnings:    retainedRunWarnings(blocking),
		NextActions: []string{"run exord-init recover list", "run exord-init recover inspect on each blocking run", "resolve or roll back every blocking run before planning again"},
		Data:        map[string]any{"retained_runs": blocking, "blocking_runs": len(blocking)},
	}
	emit(result, 6)
}

func emitRecoveryFailure(recoveryFailure *recoveryengine.Failure) {
	exitCode := 5
	if recoveryFailure.Code == "RECOVERY_REQUIRED" {
		exitCode = 6
	} else if recoveryFailure.Code == "UNSUPPORTED_TARGET" {
		exitCode = 4
	}
	result := failure("recover", recoveryFailure.Code, "error.recovery_failed", recoveryFailure.Err)
	result.Changed = recoveryFailure.Changed
	// A stored analysis captured before this failure may already be out of date,
	// so it is deliberately not attached. Re-running inspect recomputes the
	// current disposition from live target and journal state.
	result.NextActions = []string{"run exord-init recover inspect for the current disposition before any further action"}
	emit(result, exitCode)
}

func recoveryWarnings(analysis protocol.RecoveryAnalysis) []string {
	switch analysis.Disposition {
	case "CONFLICT":
		return []string{"one or more generated paths changed or have an unsafe type; automatic rollback is blocked and no target files were changed"}
	case "ROLLBACK_READY":
		return []string{"rollback can remove only hash-matched generated files; empty directories are intentionally preserved"}
	case "CLEANUP_REQUIRED":
		return []string{"the run is finalized; rollback is not allowed and finalized-state cleanup is not implemented in this slice"}
	default:
		return []string{"the retained run is not eligible for automatic rollback"}
	}
}

func recoveryNextActions(analysis protocol.RecoveryAnalysis) []string {
	if analysis.Disposition == "ROLLBACK_READY" {
		return []string{"review every recovery operation", "create a RECOVER_ROLLBACK approval bound to this run and plan", "run exord-init recover rollback"}
	}
	if analysis.Disposition == "CONFLICT" {
		return []string{"review BLOCK operations and preserve user-modified content", "resolve conflicts manually before requesting a new recovery inspection"}
	}
	return []string{"retain the run bundle until a supported cleanup or discard action is available"}
}

func emitStateError(command string, err error) {
	if errors.Is(err, state.ErrLocked) {
		emit(failure(command, "LOCKED", "error.project_locked", err), 5)
	}
	if errors.Is(err, state.ErrUnsupportedGitFile) {
		emit(failure(command, "UNSUPPORTED_TARGET", "error.gitfile_unsupported", err), 4)
	}
	emit(failure(command, "INTERNAL_ERROR", "error.local_state", err), 10)
}

func mapApplyExit(code string) int {
	switch code {
	case "PLAN_INVALID", "APPROVAL_MISMATCH", "PLAN_STALE":
		return 5
	case "RECOVERY_REQUIRED":
		return 6
	default:
		return 3
	}
}

func failure(command, code, message string, cause error) protocol.Result {
	// Filesystem errors may contain absolute paths or sensitive names. Keep the
	// machine-readable result stable and redacted by default.
	_ = cause
	return protocol.Result{SchemaVersion: 1, Command: command, Status: "FAILED", Code: code, MessageKey: message, Changed: false, Warnings: []string{}, NextActions: []string{}}
}

func emit(result protocol.Result, exitCode int) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, "failed to encode result")
	}
	os.Exit(exitCode)
}
