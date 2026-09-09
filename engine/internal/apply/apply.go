package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"example.com/exord-init/engine/internal/canonicaljson"
	"example.com/exord-init/engine/internal/id"
	"example.com/exord-init/engine/internal/protocol"
	"example.com/exord-init/engine/internal/state"
	"example.com/exord-init/engine/internal/target"
)

const maxStagedFileBytes = 4 << 20

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

func DecodeApproval(data []byte) (protocol.Approval, error) {
	var approval protocol.Approval
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&approval); err != nil {
		return approval, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return approval, errors.New("approval contains trailing JSON values")
	}
	if approval.SchemaVersion != 1 || approval.ApprovedAction != "APPLY_CREATE" {
		return approval, errors.New("unsupported approval")
	}
	if _, err := time.Parse(time.RFC3339, approval.ApprovedAt); err != nil {
		return approval, errors.New("invalid approval timestamp")
	}
	return approval, nil
}

func Execute(targetPath, projectRoot, runID string, approval protocol.Approval) (Outcome, *Failure) {
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
	journal.Stage = "FILES_APPLYING"
	if err := state.WriteJournal(runDir, journal); err != nil {
		return Outcome{}, &Failure{Code: "APPLY_FAILED", Err: err}
	}
	created := make([]createdFile, 0, len(ordered))
	for _, staged := range ordered {
		operation := staged.Operation
		setOperationStatus(&journal, operation.Path, "APPLYING")
		if err := state.WriteJournal(runDir, journal); err != nil {
			return failWithRollback(targetRoot, runDir, journal, created, err)
		}
		createdPath, directories, err := writeCreate(targetRoot, operation.Path, staged.Content)
		if err != nil {
			return failWithRollback(targetRoot, runDir, journal, created, err)
		}
		created = append(created, createdFile{Path: createdPath, ExpectedSHA256: operation.ExpectedSHA256, Directories: directories})
		setOperationStatus(&journal, operation.Path, "APPLIED")
		if err := state.WriteJournal(runDir, journal); err != nil {
			return failWithRollback(targetRoot, runDir, journal, created, err)
		}
	}
	for _, file := range created {
		actual, err := hashRootFile(targetRoot, file.Path)
		if err != nil || actual != file.ExpectedSHA256 {
			return failWithRollback(targetRoot, runDir, journal, created, errors.New("created file validation failed"))
		}
	}
	journal.Stage = "FINALIZED"
	journal.Status = "FINALIZED"
	if err := state.WriteJournal(runDir, journal); err != nil {
		return Outcome{}, &Failure{Code: "RECOVERY_REQUIRED", Err: err}
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
	if approval.SchemaVersion != 1 || approval.ApprovedAction != "APPLY_CREATE" {
		return errors.New("unsupported approval")
	}
	if approval.RunID != runID || approval.PlanID != plan.PlanID || approval.SpecSHA256 != plan.SpecSHA256 || approval.TargetIdentitySHA256 != plan.Spec.TargetIdentitySHA256 {
		return errors.New("approval binding mismatch")
	}
	approvedAt, err := time.Parse(time.RFC3339, approval.ApprovedAt)
	if err != nil {
		return errors.New("invalid approval timestamp")
	}
	startedAt, err := time.Parse(time.RFC3339Nano, journal.StartedAt)
	if err != nil || approvedAt.Before(startedAt.Add(-time.Second)) || approvedAt.After(time.Now().UTC().Add(5*time.Minute)) {
		return errors.New("approval timestamp is outside the run window")
	}
	return nil
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

func failWithRollback(targetRoot *os.Root, runDir string, journal protocol.RunJournal, created []createdFile, cause error) (Outcome, *Failure) {
	if rollback(targetRoot, created, &journal) {
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

func rollback(targetRoot *os.Root, created []createdFile, journal *protocol.RunJournal) bool {
	ok := true
	for index := len(created) - 1; index >= 0; index-- {
		file := created[index]
		actual, err := hashRootFile(targetRoot, file.Path)
		if err != nil || actual != file.ExpectedSHA256 || targetRoot.Remove(file.Path) != nil {
			ok = false
			continue
		}
		setOperationStatus(journal, filepath.ToSlash(file.Path), "ROLLED_BACK")
		removeEmptyDirectories(targetRoot, file.Directories)
	}
	return ok
}

func writeCreate(root *os.Root, relative string, content []byte) (string, []string, error) {
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
	defer func() {
		if !createdDestination {
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
		if tempExists {
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
	if _, err := root.Lstat(clean); err == nil || !os.IsNotExist(err) {
		return "", directories, errors.New("destination changed during apply")
	}
	// Link atomically publishes the synced same-directory temporary file and
	// fails if the destination appeared concurrently; it never overwrites.
	if err := root.Link(tempPath, clean); err != nil {
		return "", directories, err
	}
	createdDestination = true
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
