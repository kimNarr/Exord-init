package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"example.com/exord-init/engine/internal/id"
	"example.com/exord-init/engine/internal/planner"
	"example.com/exord-init/engine/internal/protocol"
)

var (
	ErrLocked             = errors.New("project state is locked")
	ErrUnsupportedGitFile = errors.New("gitfile state is unsupported")
)

type Lock struct {
	path string
	file *os.File
}

type projectState struct {
	SchemaVersion int    `json:"schema_version"`
	ProjectID     string `json:"project_id"`
}

func ProjectRoot(targetPath, identity string) (string, error) {
	gitPath := filepath.Join(targetPath, ".git")
	if info, err := os.Lstat(gitPath); err == nil {
		if !info.IsDir() {
			return "", ErrUnsupportedGitFile
		}
		return filepath.Join(gitPath, "exord-init"), nil
	} else if !os.IsNotExist(err) {
		return "", errors.New("cannot inspect Git state directory")
	}
	base, err := userStateBase()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "projects", identity), nil
}

func Acquire(projectRoot, runID string) (*Lock, error) {
	if err := os.MkdirAll(projectRoot, 0700); err != nil {
		return nil, errors.New("cannot create local state directory")
	}
	lockPath := filepath.Join(projectRoot, "lock.json")
	file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if os.IsExist(err) {
			return nil, ErrLocked
		}
		return nil, errors.New("cannot acquire project lock")
	}
	ownerToken, err := id.UUID4()
	if err != nil {
		file.Close()
		os.Remove(lockPath)
		return nil, errors.New("cannot create lock owner token")
	}
	payload := map[string]any{"schema_version": 1, "run_id": runID, "pid": os.Getpid(), "owner_token": ownerToken, "created_at": time.Now().UTC().Format(time.RFC3339Nano)}
	if err := json.NewEncoder(file).Encode(payload); err != nil {
		file.Close()
		os.Remove(lockPath)
		return nil, errors.New("cannot write project lock")
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(lockPath)
		return nil, errors.New("cannot sync project lock")
	}
	return &Lock{path: lockPath, file: file}, nil
}

// ProjectID returns the stable random identity for a not-yet-initialized
// project. The caller must hold the project lock.
func ProjectID(projectRoot string) (string, error) {
	path := filepath.Join(projectRoot, "project.json")
	var stored projectState
	if err := readJSON(path, &stored); err == nil {
		if stored.SchemaVersion != 1 || !isUUID(stored.ProjectID) {
			return "", errors.New("invalid local project identity")
		}
		return stored.ProjectID, nil
	} else if !os.IsNotExist(err) {
		return "", errors.New("cannot read local project identity")
	}
	projectID, err := id.UUID4()
	if err != nil {
		return "", errors.New("cannot create project identity")
	}
	if err := atomicWriteJSON(path, projectState{SchemaVersion: 1, ProjectID: projectID}); err != nil {
		return "", errors.New("cannot persist project identity")
	}
	return projectID, nil
}

func (lock *Lock) Release() error {
	if lock == nil {
		return nil
	}
	closeErr := lock.file.Close()
	removeErr := os.Remove(lock.path)
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}

func PersistPrepared(projectRoot, runID string, prepared planner.PreparedPlan) (protocol.RunJournal, error) {
	runDir, err := RunDirectory(projectRoot, runID)
	if err != nil {
		return protocol.RunJournal{}, err
	}
	if err := os.MkdirAll(filepath.Dir(runDir), 0700); err != nil {
		return protocol.RunJournal{}, errors.New("cannot create runs directory")
	}
	if err := os.Mkdir(runDir, 0700); err != nil {
		return protocol.RunJournal{}, errors.New("run state already exists or cannot be created")
	}
	if err := os.Mkdir(filepath.Join(runDir, "staging"), 0700); err != nil {
		return protocol.RunJournal{}, errors.New("cannot create run staging directory")
	}
	for _, file := range prepared.Files {
		stagePath, err := safeJoin(filepath.Join(runDir, "staging"), file.Path)
		if err != nil {
			return protocol.RunJournal{}, err
		}
		if err := atomicWrite(stagePath, file.Content, 0600); err != nil {
			return protocol.RunJournal{}, err
		}
	}
	if err := atomicWriteJSON(filepath.Join(runDir, "plan.json"), prepared.Plan); err != nil {
		return protocol.RunJournal{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	operations := make([]protocol.RunOperation, 0, len(prepared.Plan.Spec.Operations))
	for _, operation := range prepared.Plan.Spec.Operations {
		operations = append(operations, protocol.RunOperation{Path: operation.Path, Status: "PENDING"})
	}
	journal := protocol.RunJournal{
		SchemaVersion: 1, RunID: runID, PlanID: prepared.Plan.PlanID,
		SpecSHA256: prepared.Plan.SpecSHA256, Status: "ACTIVE", Stage: "PLANNED",
		Operations: operations, StartedAt: now, UpdatedAt: now,
	}
	if err := WriteJournal(runDir, journal); err != nil {
		return protocol.RunJournal{}, err
	}
	return journal, nil
}

func Load(projectRoot, runID string) (string, protocol.SetupPlan, protocol.RunJournal, error) {
	runDir, err := RunDirectory(projectRoot, runID)
	if err != nil {
		return "", protocol.SetupPlan{}, protocol.RunJournal{}, err
	}
	var plan protocol.SetupPlan
	if err := readJSON(filepath.Join(runDir, "plan.json"), &plan); err != nil {
		return "", plan, protocol.RunJournal{}, errors.New("cannot read stored plan")
	}
	var journal protocol.RunJournal
	if err := readJSON(filepath.Join(runDir, "run.json"), &journal); err != nil {
		return "", plan, journal, errors.New("cannot read run journal")
	}
	return runDir, plan, journal, nil
}

func WriteJournal(runDir string, journal protocol.RunJournal) error {
	journal.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return atomicWriteJSON(filepath.Join(runDir, "run.json"), journal)
}

func RunDirectory(projectRoot, runID string) (string, error) {
	if !isUUID(runID) {
		return "", errors.New("invalid run ID")
	}
	return filepath.Join(projectRoot, "runs", runID), nil
}

func StagedPath(runDir, relativePath string) (string, error) {
	return safeJoin(filepath.Join(runDir, "staging"), relativePath)
}

func RemoveFinalizedRun(projectRoot, runID string) error {
	runDir, err := RunDirectory(projectRoot, runID)
	if err != nil {
		return err
	}
	return os.RemoveAll(runDir)
}

func RemoveRun(projectRoot, runID string) error {
	return RemoveFinalizedRun(projectRoot, runID)
}

func atomicWriteJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'), 0600)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return errors.New("cannot create state parent directory")
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".exord-tmp-*")
	if err != nil {
		return errors.New("cannot create state temporary file")
	}
	tempPath := temp.Name()
	ok := false
	defer func() {
		temp.Close()
		if !ok {
			os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(mode); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := replaceFile(tempPath, path); err != nil {
		return err
	}
	ok = true
	return nil
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("JSON contains trailing values")
	}
	return nil
}

func safeJoin(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", errors.New("invalid relative path")
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("relative path escapes root")
	}
	joined := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("relative path escapes root")
	}
	return joined, nil
}

func userStateBase() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if value := os.Getenv("LOCALAPPDATA"); value != "" {
			return filepath.Join(value, "exord-init", "state"), nil
		}
	case "darwin":
		if value := os.Getenv("XDG_STATE_HOME"); value != "" {
			return filepath.Join(value, "exord-init"), nil
		}
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, "Library", "Application Support", "exord-init", "state"), nil
		}
	default:
		if value := os.Getenv("XDG_STATE_HOME"); value != "" {
			return filepath.Join(value, "exord-init"), nil
		}
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, ".local", "state", "exord-init"), nil
		}
	}
	return "", fmt.Errorf("cannot determine user state directory for %s", runtime.GOOS)
}

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, char := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if char != '-' {
				return false
			}
			continue
		}
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}
