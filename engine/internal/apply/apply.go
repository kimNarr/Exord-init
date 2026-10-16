package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	approvalcontract "github.com/kimNarr/Exord-init/engine/internal/approval"
	"github.com/kimNarr/Exord-init/engine/internal/canonicaljson"
	"github.com/kimNarr/Exord-init/engine/internal/id"
	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	"github.com/kimNarr/Exord-init/engine/internal/state"
	"github.com/kimNarr/Exord-init/engine/internal/target"
)

const maxStagedFileBytes = 4 << 20

// ErrHardLinksUnsupported lets callers present safe guidance without exposing
// filesystem paths from the underlying LinkError.
var ErrHardLinksUnsupported = errors.New("filesystem does not support hard-link publication")

type Failure struct {
	Code string
	Err  error
}

func (failure *Failure) Error() string { return failure.Code }

type Outcome struct {
	PlanID         string
	SpecSHA256     string
	CreatedPaths   []string
	CleanupPending bool
}

// faultInjector simulates a process crash at a named point. It exists only
// for tests: Execute always passes nil, and no flag, environment variable, or
// other external input can supply one. When it returns an error, execution
// stops immediately with no further journal writes, rollback, or deferred
// cleanup, leaving exactly the on-disk state a killed process would.
type faultInjector func(stage string) error

// simulatedCrash marks an error produced by a faultInjector so callers stop
// instead of treating it as an ordinary, rollback-worthy failure.
type simulatedCrash struct{ err error }

func (crash *simulatedCrash) Error() string { return crash.err.Error() }
func (crash *simulatedCrash) Unwrap() error { return crash.err }

func (inject faultInjector) at(stage string) error {
	if inject == nil {
		return nil
	}
	if err := inject(stage); err != nil {
		return &simulatedCrash{err: err}
	}
	return nil
}

func isSimulatedCrash(err error) bool {
	var crash *simulatedCrash
	return errors.As(err, &crash)
}

func crashed(err error) (Outcome, *Failure) {
	return Outcome{}, &Failure{Code: "RECOVERY_REQUIRED", Err: err}
}

type validatorFunc func(targetRoot *os.Root, plan protocol.SetupPlan, created []createdFile) error

func DecodeApproval(data []byte) (protocol.Approval, error) {
	value, err := approvalcontract.Decode(data)
	if err == nil && value.ApprovedAction != "APPLY_CREATE" {
		return value, errors.New("unsupported approval for apply")
	}
	return value, err
}

func Execute(targetPath, projectRoot, runID string, approval protocol.Approval) (Outcome, *Failure) {
	return execute(targetPath, projectRoot, runID, approval, nil, nil)
}

func execute(targetPath, projectRoot, runID string, approval protocol.Approval, inject faultInjector, customValidator validatorFunc) (Outcome, *Failure) {
	runDir, plan, journal, err := state.Load(projectRoot, runID)
	if err != nil {
		return Outcome{}, &Failure{Code: "PLAN_INVALID", Err: err}
	}
	if err := verifyStoredRun(runID, plan, journal); err != nil {
		return Outcome{}, &Failure{Code: "PLAN_INVALID", Err: err}
	}
	if err := verifyApproval(runID, plan, journal, approval); err != nil {
		return Outcome{}, &Failure{Code: "APPROVAL_MISMATCH", Err: err}
	}
	inspection, err := target.InspectCreate(targetPath)
	if err != nil {
		return Outcome{}, &Failure{Code: "PLAN_STALE", Err: err}
	}
	if inspection.IdentitySHA256 != plan.Spec.TargetIdentitySHA256 || inspection.Fingerprint != plan.Spec.TargetFingerprint {
		return Outcome{}, &Failure{Code: "PLAN_STALE", Err: errors.New("target identity or fingerprint changed")}
	}
	ordered, err := verifyStaging(runDir, plan)
	if err != nil {
		return Outcome{}, &Failure{Code: "PLAN_INVALID", Err: err}
	}
	targetRoot, err := os.OpenRoot(targetPath)
	if err != nil {
		return Outcome{}, &Failure{Code: "APPLY_FAILED", Err: err}
	}
	defer targetRoot.Close()

	if journal.Stage == "PLANNED" {
		journal.Stage = "APPROVED"
		if err := state.WriteJournal(runDir, journal); err != nil {
			return Outcome{}, &Failure{Code: "APPLY_FAILED", Err: err}
		}
	}
	if err := inject.at("after-approved"); err != nil {
		return crashed(err)
	}
	journal.Stage = "FILES_APPLYING"
	if err := state.WriteJournal(runDir, journal); err != nil {
		return Outcome{}, &Failure{Code: "APPLY_FAILED", Err: err}
	}
	if err := inject.at("after-files-applying"); err != nil {
		return crashed(err)
	}
	created := make([]createdFile, 0, len(ordered))
	for _, staged := range ordered {
		operation := staged.Operation
		slashPath := filepath.ToSlash(operation.Path)
		setOperationStatus(&journal, operation.Path, "APPLYING")
		if err := state.WriteJournal(runDir, journal); err != nil {
			return failWithRollback(targetRoot, runDir, journal, created, err, inject)
		}
		if err := inject.at("after-applying:" + slashPath); err != nil {
			return crashed(err)
		}
		createdPath, directories, err := writeCreate(targetRoot, operation.Path, staged.Content, inject)
		if err != nil {
			if isSimulatedCrash(err) {
				return crashed(err)
			}
			return failWithRollback(targetRoot, runDir, journal, created, err, inject)
		}
		created = append(created, createdFile{Path: createdPath, ExpectedSHA256: operation.ExpectedSHA256, Directories: directories})
		if err := inject.at("after-publish:" + slashPath); err != nil {
			return crashed(err)
		}
		setOperationStatus(&journal, operation.Path, "APPLIED")
		if err := state.WriteJournal(runDir, journal); err != nil {
			return failWithRollback(targetRoot, runDir, journal, created, err, inject)
		}
	}
	if err := inject.at("before-validation"); err != nil {
		return crashed(err)
	}
	for _, file := range created {
		actual, err := hashRootFile(targetRoot, file.Path)
		if err != nil || actual != file.ExpectedSHA256 {
			return failWithRollback(targetRoot, runDir, journal, created, errors.New("created file validation failed"), inject)
		}
	}
	validate := runValidations
	if customValidator != nil {
		validate = customValidator
	}
	if err := validate(targetRoot, plan, created); err != nil {
		return failWithRollback(targetRoot, runDir, journal, created, err, inject)
	}
	journal.Stage = "VALIDATED"
	if err := state.WriteJournal(runDir, journal); err != nil {
		return Outcome{}, &Failure{Code: "RECOVERY_REQUIRED", Err: err}
	}
	if err := inject.at("after-validated"); err != nil {
		return crashed(err)
	}
	journal.Stage = "FINALIZED"
	journal.Status = "FINALIZED"
	if err := state.WriteJournal(runDir, journal); err != nil {
		return Outcome{}, &Failure{Code: "RECOVERY_REQUIRED", Err: err}
	}
	if err := inject.at("after-finalized"); err != nil {
		return crashed(err)
	}
	paths := make([]string, 0, len(created))
	for _, file := range created {
		paths = append(paths, filepath.ToSlash(file.Path))
	}
	cleanupPending := state.RemoveFinalizedRun(projectRoot, runID) != nil
	return Outcome{PlanID: plan.PlanID, SpecSHA256: plan.SpecSHA256, CreatedPaths: paths, CleanupPending: cleanupPending}, nil
}

func verifyStoredRun(runID string, plan protocol.SetupPlan, journal protocol.RunJournal) error {
	canonical, err := canonicaljson.Marshal(plan.Spec)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(canonical)
	if hex.EncodeToString(sum[:]) != plan.SpecSHA256 {
		return errors.New("stored plan hash mismatch")
	}
	if plan.SchemaVersion != 1 || plan.Spec.Mode != "CREATE" || plan.Spec.Depth != "QUICK" {
		return errors.New("unsupported stored plan")
	}
	if plan.TargetFingerprint != plan.Spec.TargetFingerprint {
		return errors.New("plan fingerprint mismatch")
	}
	if journal.SchemaVersion != 1 || journal.Status != "ACTIVE" || (journal.Stage != "PLANNED" && journal.Stage != "APPROVED") {
		return errors.New("run is not ready to apply")
	}
	if journal.RunID != runID || journal.PlanID != plan.PlanID || journal.SpecSHA256 != plan.SpecSHA256 {
		return errors.New("run journal binding mismatch")
	}
	return nil
}

func verifyApproval(runID string, plan protocol.SetupPlan, journal protocol.RunJournal, approval protocol.Approval) error {
	return approvalcontract.Verify(approval, "APPLY_CREATE", runID, plan.PlanID, plan.SpecSHA256, plan.Spec.TargetIdentitySHA256, journal.StartedAt)
}

type stagedOperation struct {
	Operation protocol.Operation
	Content   []byte
}

func verifyStaging(runDir string, plan protocol.SetupPlan) ([]stagedOperation, error) {
	stagingRoot, err := os.OpenRoot(filepath.Join(runDir, "staging"))
	if err != nil {
		return nil, err
	}
	defer stagingRoot.Close()
	seen := map[string]bool{}
	ordered := make([]stagedOperation, 0, len(plan.Spec.Operations))
	for _, operation := range plan.Spec.Operations {
		if operation.Kind != "CREATE" || seen[operation.Path] {
			return nil, errors.New("invalid or duplicate operation")
		}
		seen[operation.Path] = true
		content, err := readRootRegular(stagingRoot, operation.Path, maxStagedFileBytes)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(content)
		if hex.EncodeToString(sum[:]) != operation.ExpectedSHA256 {
			return nil, errors.New("staged file hash mismatch")
		}
		ordered = append(ordered, stagedOperation{Operation: operation, Content: content})
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Operation.Path == ".exord/manifest.json" {
			return false
		}
		if ordered[j].Operation.Path == ".exord/manifest.json" {
			return true
		}
		return ordered[i].Operation.Path < ordered[j].Operation.Path
	})
	return ordered, nil
}

type createdFile struct {
	Path           string
	ExpectedSHA256 string
	Directories    []string
}

func failWithRollback(targetRoot *os.Root, runDir string, journal protocol.RunJournal, created []createdFile, cause error, inject faultInjector) (Outcome, *Failure) {
	complete, err := rollback(targetRoot, created, &journal, inject)
	if err != nil {
		return crashed(err)
	}
	if complete {
		journal.Status = "FAILED"
		journal.Stage = "ROLLED_BACK"
		_ = state.WriteJournal(runDir, journal)
		return Outcome{}, &Failure{Code: "APPLY_FAILED", Err: cause}
	}
	journal.Status = "RECOVERY_REQUIRED"
	journal.Stage = "ROLLBACK_INCOMPLETE"
	_ = state.WriteJournal(runDir, journal)
	return Outcome{}, &Failure{Code: "RECOVERY_REQUIRED", Err: cause}
}

// rollback removes hash-matched files this run created, newest first. It
// reports whether every file was removed; the error is non-nil only for a
// simulated crash, in which case the caller must stop without journaling.
func rollback(targetRoot *os.Root, created []createdFile, journal *protocol.RunJournal, inject faultInjector) (bool, error) {
	ok := true
	for index := len(created) - 1; index >= 0; index-- {
		file := created[index]
		actual, err := hashRootFile(targetRoot, file.Path)
		if err != nil || actual != file.ExpectedSHA256 || targetRoot.Remove(file.Path) != nil {
			ok = false
			continue
		}
		if err := inject.at("rollback-after-remove:" + filepath.ToSlash(file.Path)); err != nil {
			return false, err
		}
		setOperationStatus(journal, filepath.ToSlash(file.Path), "ROLLED_BACK")
		removeEmptyDirectories(targetRoot, file.Directories)
	}
	return ok, nil
}

func writeCreate(root *os.Root, relative string, content []byte, inject faultInjector) (string, []string, error) {
	clean, err := cleanRelative(relative)
	if err != nil {
		return "", nil, err
	}
	if _, err := root.Lstat(clean); err == nil || !os.IsNotExist(err) {
		return "", nil, errors.New("destination already exists or cannot be inspected")
	}
	directories, err := ensureParent(root, filepath.Dir(clean))
	if err != nil {
		removeEmptyDirectories(root, directories)
		return "", directories, err
	}
	createdDestination := false
	// A simulated crash skips every deferred cleanup, as a killed process would.
	crash := false
	defer func() {
		if !createdDestination && !crash {
			removeEmptyDirectories(root, directories)
		}
	}()
	tempID, err := id.UUID4()
	if err != nil {
		return "", directories, err
	}
	tempPath := filepath.Join(filepath.Dir(clean), ".exord-create-"+tempID)
	temp, err := root.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", directories, err
	}
	tempExists := true
	defer func() {
		_ = temp.Close()
		if tempExists && !crash {
			_ = root.Remove(tempPath)
		}
	}()
	if _, err := temp.Write(content); err != nil {
		return "", directories, err
	}
	if err := temp.Sync(); err != nil {
		return "", directories, err
	}
	if err := temp.Close(); err != nil {
		return "", directories, err
	}
	if err := inject.at("after-temp-write:" + filepath.ToSlash(clean)); err != nil {
		crash = true
		return "", directories, err
	}
	if _, err := root.Lstat(clean); err == nil || !os.IsNotExist(err) {
		return "", directories, errors.New("destination changed during apply")
	}
	// Link atomically publishes the synced same-directory temporary file and
	// fails if the destination appeared concurrently; it never overwrites.
	if err := root.Link(tempPath, clean); err != nil {
		if isUnsupportedLinkError(err) {
			return "", directories, fmt.Errorf("%w: %w", ErrHardLinksUnsupported, err)
		}
		return "", directories, err
	}
	createdDestination = true
	if err := inject.at("after-link:" + filepath.ToSlash(clean)); err != nil {
		crash = true
		return "", directories, err
	}
	if err := root.Remove(tempPath); err == nil {
		tempExists = false
	}
	return clean, directories, nil
}

func ensureParent(root *os.Root, parent string) ([]string, error) {
	if parent == "." {
		return []string{}, nil
	}
	clean, err := cleanRelative(parent)
	if err != nil {
		return nil, err
	}
	created := []string{}
	current := ""
	for _, component := range strings.Split(clean, string(os.PathSeparator)) {
		current = filepath.Join(current, component)
		info, err := root.Lstat(current)
		if os.IsNotExist(err) {
			if err := root.Mkdir(current, 0755); err != nil {
				return created, err
			}
			created = append(created, current)
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return created, errors.New("target parent is not a safe directory")
		}
	}
	return created, nil
}

func removeEmptyDirectories(root *os.Root, directories []string) {
	for index := len(directories) - 1; index >= 0; index-- {
		_ = root.Remove(directories[index])
	}
}

func readRootRegular(root *os.Root, relative string, limit int64) ([]byte, error) {
	clean, err := cleanRelative(relative)
	if err != nil {
		return nil, err
	}
	current := ""
	components := strings.Split(clean, string(os.PathSeparator))
	for index, component := range components {
		current = filepath.Join(current, component)
		info, err := root.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("file path contains a missing or linked component")
		}
		if index < len(components)-1 && !info.IsDir() {
			return nil, errors.New("file parent is not a directory")
		}
		if index == len(components)-1 && !info.Mode().IsRegular() {
			return nil, errors.New("file is not regular")
		}
	}
	file, err := root.Open(clean)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, errors.New("file exceeds size limit")
	}
	return content, nil
}

func hashRootFile(root *os.Root, relative string) (string, error) {
	content, err := readRootRegular(root, relative, maxStagedFileBytes)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
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

func setOperationStatus(journal *protocol.RunJournal, path, status string) {
	for index := range journal.Operations {
		if journal.Operations[index].Path == path {
			journal.Operations[index].Status = status
			return
		}
	}
}
