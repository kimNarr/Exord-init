package main

import (
	"errors"
	"flag"
	"io"
	"os"

	"github.com/kimNarr/Exord-init/engine/internal/planner"
	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	"github.com/kimNarr/Exord-init/engine/internal/target"
	taskcontract "github.com/kimNarr/Exord-init/engine/internal/task"
)

func runTask(args []string) {
	if len(args) == 0 || args[0] != "plan" {
		emit(failure("task", "UNSUPPORTED_CAPABILITY", "error.task_action", nil), 7)
	}
	flags := flag.NewFlagSet("task plan", flag.ContinueOnError)
	intentPath := flags.String("intent", "", "path to TaskIntent JSON")
	targetPath := flags.String("target", "", "existing exord project directory")
	prepare := flags.Bool("prepare", false, "preserve a local draft ID and exact document plan; does not create TASK.md")
	draftID := flags.String("draft-id", "", "reuse an existing draft explicitly, including after a work-branch rename")
	expectedProtocol := flags.Int("protocol-version", protocolVersion, "expected protocol version")
	flags.Bool("json", false, "emit JSON (always enabled)")
	flags.SetOutput(os.Stderr)
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *intentPath == "" || *targetPath == "" {
		emit(failure("task", "INVALID_INTENT", "error.task_flags", nil), 2)
	}
	if *expectedProtocol != protocolVersion {
		emit(failure("task", "VERSION_MISMATCH", "error.protocol_version", nil), 7)
	}
	if *draftID != "" && (!*prepare || !taskcontract.ValidDraftID(*draftID)) {
		emit(failure("task", "INVALID_INTENT", "error.task_draft_flags", nil), 2)
	}
	file, err := os.Open(*intentPath)
	if err != nil {
		emit(failure("task", "INVALID_INTENT", "error.intent_read", err), 2)
	}
	raw, err := io.ReadAll(io.LimitReader(file, taskcontract.MaxDocumentBytes+1))
	_ = file.Close()
	if err != nil {
		emit(failure("task", "INVALID_INTENT", "error.intent_read", err), 2)
	}
	intent, err := taskcontract.DecodeIntent(raw)
	if err != nil {
		emit(failure("task", "INVALID_INTENT", "error.task_intent", err), 2)
	}
	inspection, err := target.InspectTask(*targetPath, intent.BaseBranch)
	if err != nil {
		if errors.Is(err, target.ErrManifestSchemaUnsupported) {
			emit(failure("task", "VERSION_MISMATCH", "error.manifest_schema", err), 7)
		}
		emit(failure("task", "UNSUPPORTED_TARGET", "error.task_target", err), 4)
	}
	plan, err := planner.BuildTaskReview(intent, inspection)
	if err != nil {
		emit(failure("task", "INTERNAL_ERROR", "error.task_plan", err), 10)
	}
	if *prepare && plan.Spec.Disposition != "BLOCKED" {
		result, exit := prepareTaskLocally(*targetPath, intent, *draftID)
		emit(result, exit)
	}
	status, code := "OK", "OK"
	if plan.Spec.Disposition == "BLOCKED" {
		status, code = "BLOCKED", "TASK_NOT_READY"
	}
	warnings := []string{"read-only task review; no task ID, files, local state or approval request were created"}
	if inspection.SecretWarnings > 0 || inspection.SecretScanLimited {
		warnings = append(warnings, "possible secrets or incomplete secret scan; suspend commit proposals until human review")
	}
	result := protocol.Result{SchemaVersion: 1, Command: "task", Status: status, Code: code, MessageKey: "task.reviewed", PlanID: &plan.PlanID, SpecSHA256: &plan.SpecSHA256, Changed: false, Warnings: warnings, NextActions: []string{"review blockers and readiness gaps with the human owner", "task apply is not implemented; do not generate task files from this review"}, Data: map[string]any{"task_plan": plan}}
	// Like other read-only plans, a valid BLOCKED analysis is still a successful
	// inspection; automation must inspect status/disposition before proceeding.
	emit(result, 0)
}
