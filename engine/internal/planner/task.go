package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/kimNarr/Exord-init/engine/internal/canonicaljson"
	"github.com/kimNarr/Exord-init/engine/internal/id"
	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	"github.com/kimNarr/Exord-init/engine/internal/target"
	taskcontract "github.com/kimNarr/Exord-init/engine/internal/task"
)

func BuildTaskReview(intent protocol.TaskIntent, inspection target.TaskInspection) (protocol.TaskReviewPlan, error) {
	if err := taskcontract.ValidateIntent(intent); err != nil {
		return protocol.TaskReviewPlan{}, err
	}
	blockers := []string{}
	git := inspection.Git
	if !git.Detected || !git.CLIAvailable || !git.RootMatch || git.Status != "OK" || git.GitFile {
		blockers = append(blockers, "ORDINARY_GIT_REPOSITORY_REQUIRED")
	}
	if git.Unborn || git.Head == "" {
		blockers = append(blockers, "BORN_BRANCH_REQUIRED")
	}
	if git.Branch == "" {
		blockers = append(blockers, "ATTACHED_BRANCH_REQUIRED")
	}
	if git.InProgress != "" {
		blockers = append(blockers, "GIT_OPERATION_IN_PROGRESS")
	}
	if git.Branch != intent.WorkBranch {
		blockers = append(blockers, "WORK_BRANCH_MISMATCH")
	}
	// Reserve common integration names conservatively, also on case-insensitive
	// filesystems. Explicit project settings and the selected base add protection.
	protected := append([]string{"main", "dev", "master", intent.BaseBranch}, inspection.IntegrationBranches...)
	for _, branch := range protected {
		if strings.EqualFold(git.Branch, branch) {
			blockers = append(blockers, "INTEGRATION_BRANCH")
			break
		}
	}
	if inspection.TaskExists {
		blockers = append(blockers, "EXISTING_TASK_CONFLICT")
	}
	if !inspection.BaseBranchExists {
		blockers = append(blockers, "LOCAL_BASE_BRANCH_REQUIRED")
	}
	gaps := taskcontract.ReadinessGaps(intent)
	disposition := "DRAFT"
	if len(gaps) == 0 {
		disposition = "READY_FOR_REVIEW"
	}
	if len(blockers) > 0 || (intent.Status == "READY" && len(gaps) > 0) {
		disposition = "BLOCKED"
	}
	spec := protocol.TaskReviewSpec{Mode: "TASK_REVIEW", ProjectID: inspection.ProjectID, TargetIdentitySHA256: inspection.IdentitySHA256, TargetFingerprint: inspection.Fingerprint, TaskPath: inspection.TaskPath, Intent: intent, Git: git, Disposition: disposition, Blockers: blockers, ReadinessGaps: gaps, ValidationResult: "NOT_RUN"}
	canonical, err := canonicaljson.Marshal(spec)
	if err != nil {
		return protocol.TaskReviewPlan{}, err
	}
	sum := sha256.Sum256(canonical)
	planID, err := id.UUID4()
	if err != nil {
		return protocol.TaskReviewPlan{}, err
	}
	return protocol.TaskReviewPlan{SchemaVersion: 1, PlanID: planID, Spec: spec, SpecSHA256: hex.EncodeToString(sum[:])}, nil
}
