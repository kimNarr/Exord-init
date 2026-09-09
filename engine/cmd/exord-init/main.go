package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"

	exordinit "example.com/exord-init"
	applyengine "example.com/exord-init/engine/internal/apply"
	"example.com/exord-init/engine/internal/id"
	"example.com/exord-init/engine/internal/planner"
	"example.com/exord-init/engine/internal/protocol"
	"example.com/exord-init/engine/internal/state"
	"example.com/exord-init/engine/internal/target"
)

var version = "0.2.0-dev"

const protocolVersion = 1

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
		Data:        map[string]any{"version": version, "protocol_version": protocolVersion, "goos": runtime.GOOS, "goarch": runtime.GOARCH, "capabilities": []string{"doctor", "plan:create-quick", "apply:create-quick"}},
	}
	emit(result, 0)
}

func runPlan(args []string) {
	set := flag.NewFlagSet("plan", flag.ContinueOnError)
	intentPath := set.String("intent", "", "path to SetupIntent JSON")
	targetPath := set.String("target", "", "target project directory")
	expectedProtocol := set.Int("protocol-version", protocolVersion, "protocol version expected by the adapter")
	jsonOutput := set.Bool("json", false, "emit JSON (output is always JSON)")
	set.SetOutput(os.Stderr)
	if err := set.Parse(args); err != nil || *intentPath == "" || *targetPath == "" {
		emit(failure("plan", "INVALID_INTENT", "error.plan_flags", nil), 2)
	}
	_ = jsonOutput
	if *expectedProtocol != protocolVersion {
		emit(failure("plan", "VERSION_MISMATCH", "error.protocol_version", nil), 7)
	}
	raw, err := os.ReadFile(*intentPath)
	if err != nil {
		emit(failure("plan", "INVALID_INTENT", "error.intent_read", err), 2)
	}
	intent, err := planner.DecodeIntent(raw)
	if err != nil {
		emit(failure("plan", "INVALID_INTENT", "error.intent_invalid", err), 2)
	}
	if intent.Mode != "CREATE" || intent.Depth != "QUICK" {
		emit(failure("plan", "UNSUPPORTED_CAPABILITY", "error.v02_scope", nil), 7)
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
	if err := lock.Release(); err != nil {
		_ = state.RemoveRun(projectRoot, runID)
		emit(failure("plan", "INTERNAL_ERROR", "error.lock_release", err), 10)
	}
	plan := prepared.Plan
	result := protocol.Result{
		SchemaVersion: 1, Command: "plan", Status: "OK", Code: "OK", MessageKey: "plan.ready",
		RunID: &runID, PlanID: &plan.PlanID, SpecSHA256: &plan.SpecSHA256, Changed: false,
		Warnings:    []string{"the target project is unchanged; an approval-bound plan and staged files are stored in local run state"},
		NextActions: []string{"review the plan", "create an approval document bound to run_id, plan_id, spec_sha256, and target_identity_sha256", "run exord-init apply"},
		Data: map[string]any{
			"inspection":       inspection,
			"plan":             plan,
			"approval_request": map[string]any{"schema_version": 1, "run_id": runID, "plan_id": plan.PlanID, "spec_sha256": plan.SpecSHA256, "target_identity_sha256": plan.Spec.TargetIdentitySHA256, "approved_action": "APPLY_CREATE"},
		},
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
	if err := lock.Release(); err != nil {
		emit(failure("apply", "RECOVERY_REQUIRED", "error.lock_release", err), 6)
	}
	warnings := []string{}
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
