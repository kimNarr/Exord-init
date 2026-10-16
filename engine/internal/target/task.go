package target

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	taskcontract "github.com/kimNarr/Exord-init/engine/internal/task"
)

type TaskInspection struct {
	ProjectID           string
	IdentitySHA256      string
	Fingerprint         string
	TaskPath            string
	TaskExists          bool
	Git                 protocol.GitAnalysis
	IntegrationBranches []string
	SecretWarnings      int
	SecretScanLimited   bool
	BaseBranchExists    bool
}

// InspectTask does not create local project state, journal, lock or task ID.
// Inventory deliberately includes existing user changes and known path aliases.
func InspectTask(path, baseBranch string) (TaskInspection, error) {
	var result TaskInspection
	if !taskcontract.ValidBranch(baseBranch) {
		return result, errors.New("invalid base branch")
	}
	adopt, err := InspectAdopt(path)
	if err != nil {
		return result, err
	}
	for _, p := range []string{".exord", ".exord/manifest.json"} {
		entry, ok := adopt.entries[pathKey(p)]
		want := "DIRECTORY"
		if p != ".exord" {
			want = "REGULAR"
		}
		if !ok || entry.Path != p || entry.Kind != want {
			return result, ErrNotExordProject
		}
	}
	root, err := os.OpenRoot(adopt.Path)
	if err != nil {
		return result, err
	}
	defer root.Close()
	raw, err := readRootFile(root, filepath.FromSlash(".exord/manifest.json"), maxKnownFileBytes)
	if err != nil {
		return result, ErrNotExordProject
	}
	var manifest struct {
		SchemaVersion int    `json:"schema_version"`
		ProjectID     string `json:"project_id"`
		Generator     struct {
			Name string `json:"name"`
		} `json:"generator"`
		Settings struct {
			GitMode             string   `json:"git_mode"`
			TaskPath            string   `json:"task_path"`
			IntegrationBranches []string `json:"integration_branches"`
		} `json:"settings"`
	}
	if json.Unmarshal(raw, &manifest) != nil || !validUUID(manifest.ProjectID) || manifest.Generator.Name != "exord-init" {
		return result, ErrNotExordProject
	}
	if manifest.SchemaVersion != 1 {
		return result, ErrManifestSchemaUnsupported
	}
	if manifest.Settings.GitMode != "ENABLED" {
		return result, errors.New("task review requires Git-enabled settings")
	}
	if manifest.Settings.TaskPath != "TASK.md" && manifest.Settings.TaskPath != ".exord/TASK.md" {
		return result, errors.New("unsupported task path")
	}
	if len(manifest.Settings.IntegrationBranches) > 32 {
		return result, errors.New("too many integration branches")
	}
	for _, branch := range manifest.Settings.IntegrationBranches {
		if !taskcontract.ValidBranch(branch) {
			return result, errors.New("invalid integration branch")
		}
	}
	_, configuredExists := adopt.entries[pathKey(manifest.Settings.TaskPath)]
	// A second task at either supported path also requires human resolution.
	_, rootExists := adopt.entries[pathKey("TASK.md")]
	_, alternateExists := adopt.entries[pathKey(".exord/TASK.md")]
	result = TaskInspection{ProjectID: manifest.ProjectID, IdentitySHA256: adopt.IdentitySHA256, Fingerprint: sha256Hex([]byte(adopt.Fingerprint + "\x00" + sha256Hex(raw))), TaskPath: manifest.Settings.TaskPath, TaskExists: configuredExists || rootExists || alternateExists, Git: adopt.Git, IntegrationBranches: manifest.Settings.IntegrationBranches, SecretWarnings: adopt.Inventory.SecretCandidates, SecretScanLimited: adopt.Inventory.SecretScanLimited}
	marker, ok := adopt.entries[pathKey(".git")]
	if !ok || marker.Kind != "GIT_DIRECTORY" {
		result.Git.Status = "INVALID_REPOSITORY"
	}
	if gitPath, err := exec.LookPath("git"); err == nil && result.Git.Status == "OK" {
		_, err = runGit(gitPath, adopt.Path, "show-ref", "--verify", "--quiet", "refs/heads/"+baseBranch)
		result.BaseBranchExists = err == nil
	}
	return result, nil
}
