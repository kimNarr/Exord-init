package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	approvalcontract "github.com/kimNarr/Exord-init/engine/internal/approval"
	"github.com/kimNarr/Exord-init/engine/internal/canonicaljson"
	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	"github.com/kimNarr/Exord-init/engine/internal/state"
	"github.com/kimNarr/Exord-init/engine/internal/target"
)

const maxRecoveryFileBytes = 4 << 20

type Failure struct {
	Code    string
	Err     error
	Changed bool
}

func (failure *Failure) Error() string { return failure.Code }

type loaded struct {
	targetPath string
	runDir     string
	plan       protocol.SetupPlan
	journal    protocol.RunJournal
}

func Inspect(targetPath, projectRoot, runID string) (protocol.RecoveryAnalysis, *Failure) {
	value, failure := load(targetPath, projectRoot, runID)
	if failure != nil {
		return protocol.RecoveryAnalysis{}, failure
	}
	analysis, err := analyze(value)
	if err != nil {
		return protocol.RecoveryAnalysis{}, &Failure{Code: "PLAN_INVALID", Err: err}
	}
	return analysis, nil
}

func Rollback(targetPath, projectRoot, runID string, approval protocol.Approval) (protocol.RecoveryAnalysis, bool, *Failure) {
	value, failure := load(targetPath, projectRoot, runID)
	if failure != nil {
		return protocol.RecoveryAnalysis{}, false, failure
	}
	root, err := os.OpenRoot(value.targetPath)
	if err != nil {
		return protocol.RecoveryAnalysis{}, false, &Failure{Code: "RECOVERY_REQUIRED", Err: err}
	}
	defer root.Close()
	analysis, err := analyzeRoot(value, root)
	if err != nil {
		return protocol.RecoveryAnalysis{}, false, &Failure{Code: "PLAN_INVALID", Err: err}
	}
	if analysis.Disposition != "ROLLBACK_READY" {
		return analysis, false, &Failure{Code: "RECOVERY_REQUIRED", Err: errors.New("run is not safe for automatic rollback")}
	}
	if err := approvalcontract.Verify(approval, "RECOVER_ROLLBACK", runID, value.plan.PlanID, value.plan.SpecSHA256, value.plan.Spec.TargetIdentitySHA256, value.journal.StartedAt); err != nil {
		return analysis, false, &Failure{Code: "APPROVAL_MISMATCH", Err: err}
	}

	operations := operationMap(value.journal)
	changed := false
	for index := len(value.plan.Spec.Operations) - 1; index >= 0; index-- {
		operation := value.plan.Spec.Operations[index]
		journalIndex := operations[operation.Path]
		journalOperation := &value.journal.Operations[journalIndex]
		if journalOperation.Status != "APPLYING" && journalOperation.Status != "APPLIED" {
			continue
		}
		current, err := currentState(root, operation.Path, operation.ExpectedSHA256)
		if err != nil {
			return analysis, changed, recoveryFailure(value.runDir, &value.journal, err, changed)
		}
		switch current {
		case "MISSING":
			journalOperation.Status = "ROLLED_BACK"
		case "MATCHED":
			clean, err := cleanRelative(operation.Path)
			if err != nil {
				return analysis, changed, recoveryFailure(value.runDir, &value.journal, err, changed)
			}
			if err := root.Remove(clean); err != nil {
				return analysis, changed, recoveryFailure(value.runDir, &value.journal, err, changed)
			}
			journalOperation.Status = "ROLLED_BACK"
			changed = true
		default:
			return analysis, changed, recoveryFailure(value.runDir, &value.journal, errors.New("target changed during recovery"), changed)
		}
		if err := state.WriteJournal(value.runDir, value.journal); err != nil {
			return analysis, changed, &Failure{Code: "RECOVERY_REQUIRED", Err: err, Changed: changed}
		}
		changed = true
	}
	value.journal.Status = "FAILED"
	value.journal.Stage = "ROLLED_BACK"
	if err := state.WriteJournal(value.runDir, value.journal); err != nil {
		return analysis, changed, &Failure{Code: "RECOVERY_REQUIRED", Err: err, Changed: changed}
	}
	changed = true
	final, err := analyzeRoot(value, root)
	if err != nil {
		return analysis, changed, &Failure{Code: "RECOVERY_REQUIRED", Err: err, Changed: changed}
	}
	return final, changed, nil
}

// Discard permanently removes one retained run bundle. It is allowed only for a
// settled run — a rolled-back FAILED run, a FINALIZED run whose cleanup did not
// complete, or an ACTIVE plan that was never applied — so it can never erase
// RECOVERY_REQUIRED evidence or a run that is still mid-apply. It requires a
// RECOVER_DISCARD approval bound to the same run, plan, spec hash, and target,
// verifies the stored plan the same way rollback does, and reports whether the
// bundle was actually removed.
func Discard(targetPath, projectRoot, runID string, approval protocol.Approval) (bool, *Failure) {
	value, failure := load(targetPath, projectRoot, runID)
	if failure != nil {
		return false, failure
	}
	if !discardableState(value.journal) {
		return false, &Failure{Code: "DISCARD_NOT_ALLOWED", Err: errors.New("run is not in a settled state; roll it back or finish recovery first")}
	}
	if err := approvalcontract.Verify(approval, "RECOVER_DISCARD", runID, value.plan.PlanID, value.plan.SpecSHA256, value.plan.Spec.TargetIdentitySHA256, value.journal.StartedAt); err != nil {
		return false, &Failure{Code: "APPROVAL_MISMATCH", Err: err}
	}
	removed, err := state.DiscardRun(projectRoot, runID)
	if err != nil {
		// removed reports whether anything was deleted before the failure.
		return removed, &Failure{Code: "DISCARD_INCOMPLETE", Err: err, Changed: removed}
	}
	return removed, nil
}

// discardableState is the allowlist of journal states a discard may remove.
// RECOVERY_REQUIRED, BLOCKED, and any interrupted mid-apply ACTIVE run are
// intentionally excluded.
func discardableState(journal protocol.RunJournal) bool {
	switch journal.Status {
	case "FAILED", "FINALIZED":
		return true
	case "ACTIVE":
		return journal.Stage == "PLANNED" || journal.Stage == "APPROVED"
	default:
		return false
	}
}

func load(targetPath, projectRoot, runID string) (loaded, *Failure) {
	identity, err := target.InspectIdentity(targetPath)
	if err != nil {
		return loaded{}, &Failure{Code: "UNSUPPORTED_TARGET", Err: err}
	}
	runDir, plan, journal, err := state.Load(projectRoot, runID)
	if err != nil {
		return loaded{}, &Failure{Code: "PLAN_INVALID", Err: err}
	}
	if err := validateStored(runID, identity.IdentitySHA256, plan, journal); err != nil {
		return loaded{}, &Failure{Code: "PLAN_INVALID", Err: err}
	}
	return loaded{targetPath: identity.Path, runDir: runDir, plan: plan, journal: journal}, nil
}

func validateStored(runID, identity string, plan protocol.SetupPlan, journal protocol.RunJournal) error {
	canonical, err := canonicaljson.Marshal(plan.Spec)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(canonical)
	if hex.EncodeToString(sum[:]) != plan.SpecSHA256 || plan.TargetFingerprint != plan.Spec.TargetFingerprint {
		return errors.New("stored plan hash or fingerprint mismatch")
	}
	if plan.SchemaVersion != 1 || plan.Spec.Mode != "CREATE" || plan.Spec.Depth != "QUICK" || plan.Spec.TargetIdentitySHA256 != identity {
		return errors.New("stored plan is not recoverable for this target")
	}
	if journal.SchemaVersion != 1 || journal.RunID != runID || journal.PlanID != plan.PlanID || journal.SpecSHA256 != plan.SpecSHA256 {
		return errors.New("run journal binding mismatch")
	}
	if len(journal.Operations) != len(plan.Spec.Operations) {
		return errors.New("run operation count mismatch")
	}
	planPaths := map[string]bool{}
	for _, operation := range plan.Spec.Operations {
		if operation.Kind != "CREATE" || planPaths[operation.Path] {
			return errors.New("invalid stored operation")
		}
		if _, err := cleanRelative(operation.Path); err != nil {
			return err
		}
		planPaths[operation.Path] = true
	}
	journalPaths := map[string]bool{}
	for _, operation := range journal.Operations {
		if !planPaths[operation.Path] || journalPaths[operation.Path] || !validJournalStatus(operation.Status) {
			return errors.New("invalid run journal operation")
		}
		journalPaths[operation.Path] = true
	}
	return nil
}

func analyze(value loaded) (protocol.RecoveryAnalysis, error) {
	root, err := os.OpenRoot(value.targetPath)
	if err != nil {
		return protocol.RecoveryAnalysis{}, err
	}
	defer root.Close()
	return analyzeRoot(value, root)
}

func analyzeRoot(value loaded, root *os.Root) (protocol.RecoveryAnalysis, error) {
	result := protocol.RecoveryAnalysis{
		SchemaVersion: 1, RunID: value.journal.RunID, PlanID: value.plan.PlanID,
		SpecSHA256: value.plan.SpecSHA256, Status: value.journal.Status, Stage: value.journal.Stage,
		Disposition: "NOT_RECOVERABLE", Operations: []protocol.RecoveryOperationAnalysis{},
	}
	journalOperations := operationMap(value.journal)
	eligible := value.journal.Status == "RECOVERY_REQUIRED" || (value.journal.Status == "ACTIVE" && (value.journal.Stage == "FILES_APPLYING" || value.journal.Stage == "FILES_APPLIED" || value.journal.Stage == "VALIDATED"))
	for _, operation := range value.plan.Spec.Operations {
		journalOperation := value.journal.Operations[journalOperations[operation.Path]]
		current, err := currentState(root, operation.Path, operation.ExpectedSHA256)
		if err != nil {
			return result, err
		}
		action := "NONE"
		if eligible {
			switch journalOperation.Status {
			case "APPLYING", "APPLIED":
				switch current {
				case "MATCHED":
					action = "REMOVE"
					result.Recoverable++
				case "MISSING":
					action = "MARK_ROLLED_BACK"
					result.AlreadyAbsent++
				default:
					action = "BLOCK"
					result.Conflicts++
				}
			case "PENDING", "ROLLED_BACK":
				if current != "MISSING" {
					action = "BLOCK"
					result.Conflicts++
				}
			}
		}
		result.Operations = append(result.Operations, protocol.RecoveryOperationAnalysis{Path: operation.Path, JournalStatus: journalOperation.Status, CurrentState: current, RecoveryAction: action})
	}
	if eligible {
		if result.Conflicts > 0 {
			result.Disposition = "CONFLICT"
		} else {
			result.Disposition = "ROLLBACK_READY"
		}
	} else if value.journal.Status == "FINALIZED" {
		result.Disposition = "CLEANUP_REQUIRED"
	} else if value.journal.Status == "FAILED" && value.journal.Stage == "ROLLED_BACK" {
		result.Disposition = "ROLLED_BACK_RETAINED"
	}
	return result, nil
}

func recoveryFailure(runDir string, journal *protocol.RunJournal, cause error, changed bool) *Failure {
	journal.Status = "RECOVERY_REQUIRED"
	journal.Stage = "ROLLBACK_INCOMPLETE"
	journalChanged := state.WriteJournal(runDir, *journal) == nil
	return &Failure{Code: "RECOVERY_REQUIRED", Err: cause, Changed: changed || journalChanged}
}

func operationMap(journal protocol.RunJournal) map[string]int {
	result := make(map[string]int, len(journal.Operations))
	for index, operation := range journal.Operations {
		result[operation.Path] = index
	}
	return result
}

func currentState(root *os.Root, relative, expectedSHA256 string) (string, error) {
	clean, err := cleanRelative(relative)
	if err != nil {
		return "", err
	}
	info, err := root.Lstat(clean)
	if os.IsNotExist(err) {
		return "MISSING", nil
	}
	if err != nil {
		return "UNREADABLE", nil
	}
	// IsRegular already excludes symlinks, devices, and every other mode bit,
	// so a non-regular node here is a type conflict.
	if !info.Mode().IsRegular() {
		return "TYPE_CONFLICT", nil
	}
	file, err := root.Open(clean)
	if err != nil {
		return "UNREADABLE", nil
	}
	defer file.Close()
	hasher := sha256.New()
	written, err := io.Copy(hasher, io.LimitReader(file, maxRecoveryFileBytes+1))
	if err != nil {
		return "UNREADABLE", nil
	}
	if written > maxRecoveryFileBytes {
		return "MODIFIED", nil
	}
	if hex.EncodeToString(hasher.Sum(nil)) == expectedSHA256 {
		return "MATCHED", nil
	}
	return "MODIFIED", nil
}

func cleanRelative(relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", errors.New("invalid relative path")
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("relative path escapes root")
	}
	return clean, nil
}

func validJournalStatus(status string) bool {
	return status == "PENDING" || status == "APPLYING" || status == "APPLIED" || status == "ROLLED_BACK"
}
