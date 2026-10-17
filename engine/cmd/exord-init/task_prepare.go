package main

import (
	"time"

	"github.com/kimNarr/Exord-init/engine/internal/id"
	"github.com/kimNarr/Exord-init/engine/internal/planner"
	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	"github.com/kimNarr/Exord-init/engine/internal/state"
	"github.com/kimNarr/Exord-init/engine/internal/target"
)

// Return before emitting: os.Exit would otherwise bypass lock/handle cleanup.
func prepareTaskLocally(targetPath string, intent protocol.TaskIntent, selectedDraft string) (result protocol.Result, exit int) {
	lockID, err := id.UUID4()
	if err != nil {
		return failure("task", "INTERNAL_ERROR", "error.task_draft_id", err), 10
	}
	root, lock, err := state.OpenTaskDraftState(targetPath, lockID)
	if err != nil {
		return failure("task", "TASK_STATE_UNAVAILABLE", "error.task_draft_state", err), 5
	}
	defer root.Close()
	defer func() {
		if result.Code != "OK" {
			result.Warnings = append(result.Warnings, "local draft metadata may be retained; preserve it for inspection instead of deleting or replacing records")
		}
		if err := lock.Release(); err != nil {
			result.Warnings = append(result.Warnings, "the local lock file could not be removed; the OS lock was released")
		}
	}()
	inspection, err := target.InspectTask(targetPath, intent.BaseBranch)
	if err != nil {
		return failure("task", "PLAN_STALE", "error.task_target_changed", err), 5
	}
	review, err := planner.BuildTaskReview(intent, inspection)
	if err != nil {
		return failure("task", "INVALID_INTENT", "error.task_intent", err), 2
	}
	if review.Spec.Disposition == "BLOCKED" {
		return protocol.Result{SchemaVersion: 1, Command: "task", Status: "BLOCKED", Code: "TASK_NOT_READY", MessageKey: "task.reviewed", Changed: false, Warnings: []string{}, NextActions: []string{"resolve task blockers before preparing a local draft"}, Data: map[string]any{"task_plan": review}}, 0
	}
	projectRoot, err := state.ProjectRoot(targetPath, inspection.IdentitySHA256)
	if err != nil {
		return failure("task", "TASK_STATE_UNAVAILABLE", "error.task_draft_state", err), 5
	}
	runs, err := state.ListRetainedRuns(projectRoot)
	if err != nil {
		return failure("task", "TASK_STATE_UNAVAILABLE", "error.task_draft_state", err), 5
	}
	if len(state.BlockingRetainedRuns(runs)) > 0 {
		result = failure("task", "RECOVERY_REQUIRED", "error.task_recovery_required", nil)
		result.Status = "BLOCKED"
		result.NextActions = []string{"run recover list and resolve interrupted CREATE runs before preparation"}
		return result, 6
	}
	used, scan, err := target.InspectTaskIDs(targetPath, intent.WorkBranch)
	if err != nil {
		return failure("task", "TASK_STATE_UNAVAILABLE", "error.task_collision_scan", err), 5
	}
	draft, err := state.ResolveTaskDraft(root, inspection.ProjectID, inspection.IdentitySHA256, intent.WorkBranch, selectedDraft, used, time.Now().UTC())
	if err != nil {
		return failure("task", "TASK_STATE_UNAVAILABLE", "error.task_draft_state", err), 5
	}
	prepared, err := planner.PrepareTask(intent, inspection, draft, scan)
	if err != nil {
		return failure("task", "INVALID_INTENT", "error.task_document", err), 2
	}
	prepared, err = state.PersistTaskPreparation(root, prepared)
	if err != nil {
		return failure("task", "TASK_STATE_UNAVAILABLE", "error.task_draft_persist", err), 5
	}
	warnings := []string{"TASK.md and tracked project files were not changed; local Git metadata now preserves a draft and exact plan revision", "task apply and task recovery are not implemented; this plan is not an approval request"}
	if len(scan.Limits) > 0 {
		warnings = append(warnings, "Task ID collision inspection was limited; review collision_scan.limits; remote-wide uniqueness is not guaranteed")
	}
	if inspection.SecretWarnings > 0 || inspection.SecretScanLimited {
		warnings = append(warnings, "possible secrets or incomplete secret scan; suspend commit proposals until human review")
	}
	return protocol.Result{SchemaVersion: 1, Command: "task", Status: "OK", Code: "OK", MessageKey: "task.prepared", PlanID: &prepared.PlanID, SpecSHA256: &prepared.SpecSHA256, Changed: false, Warnings: warnings, NextActions: []string{"review the exact document, readiness gaps and collision scan limits", "reuse with task plan --prepare --draft-id " + draft.DraftID + "; task apply remains unsupported"}, Data: map[string]any{"task_preparation": prepared, "local_state_changed": true}}, 0
}
