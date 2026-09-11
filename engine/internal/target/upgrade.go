package target

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrNotExordProject means the target has no readable exord-init manifest, so
// an upgrade plan cannot start. The caller directs the user to CREATE or ADOPT.
var ErrNotExordProject = errors.New("target is not an exord-init project")

// ErrManifestSchemaUnsupported means the manifest declares a schema version the
// engine does not understand. Every write must stop; the caller fails closed.
var ErrManifestSchemaUnsupported = errors.New("manifest schema version is not supported")

const maxUpgradeFileBytes = 1 << 20

// ManifestManagedFile is one managed-file record from `.exord/manifest.json`.
type ManifestManagedFile struct {
	Path            string `json:"path"`
	TemplateID      string `json:"template_id"`
	TemplateVersion int    `json:"template_version"`
	BaselineSHA256  string `json:"baseline_sha256"`
	HashMode        string `json:"hash_mode"`
	UpdatePolicy    string `json:"update_policy"`
}

// ManifestDocument is the subset of `.exord/manifest.json` an upgrade plan
// needs. Unknown fields inside a schema_version 1 manifest are tolerated;
// a different schema_version is rejected before this is used.
type ManifestDocument struct {
	SchemaVersion int `json:"schema_version"`
	Generator     struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"generator"`
	ProjectID string `json:"project_id"`
	Settings  struct {
		GitMode       string `json:"git_mode"`
		BranchProfile string `json:"branch_profile"`
		Languages     struct {
			Documentation string `json:"documentation"`
		} `json:"languages"`
		SupportedAgents []string `json:"supported_agents"`
	} `json:"settings"`
	ManagedFiles []ManifestManagedFile `json:"managed_files"`
}

// UpgradeFileState is the current on-disk state of one managed file.
type UpgradeFileState struct {
	Kind             string // REGULAR, SYMLINK, MISSING, TYPE_CONFLICT, UNREADABLE
	NormalizedSHA256 string // SHA-256 of the CRLF->LF normalized bytes, when REGULAR
}

type UpgradeInspection struct {
	Path           string
	IdentitySHA256 string
	Fingerprint    string
	GitDetected    bool
	Manifest       ManifestDocument
	Current        map[string]UpgradeFileState
}

// InspectUpgrade reads an existing exord-init project's manifest and the
// current state of every managed file without modifying the target.
func InspectUpgrade(path string) (UpgradeInspection, error) {
	abs, resolved, err := resolveSafeRoot(path)
	if err != nil {
		return UpgradeInspection{}, err
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return UpgradeInspection{}, err
	}
	defer root.Close()

	manifestInfo, err := root.Lstat(filepath.FromSlash(".exord/manifest.json"))
	if err != nil || !manifestInfo.Mode().IsRegular() || manifestInfo.Size() > maxUpgradeFileBytes {
		return UpgradeInspection{}, ErrNotExordProject
	}
	raw, err := readRootFile(root, filepath.FromSlash(".exord/manifest.json"), maxUpgradeFileBytes)
	if err != nil {
		return UpgradeInspection{}, ErrNotExordProject
	}
	var manifest ManifestDocument
	if json.Unmarshal(raw, &manifest) != nil {
		return UpgradeInspection{}, ErrNotExordProject
	}
	if !validUUID(manifest.ProjectID) || manifest.Generator.Name != "exord-init" {
		return UpgradeInspection{}, ErrNotExordProject
	}
	if manifest.SchemaVersion != 1 {
		return UpgradeInspection{}, ErrManifestSchemaUnsupported
	}

	managedPaths := map[string]bool{}
	for _, file := range manifest.ManagedFiles {
		managedPaths[file.Path] = true
	}
	// Also inspect the files the current generator would manage for these
	// settings, so a bridge added later shows up as a candidate.
	for _, expected := range expectedManagedPaths(manifest.Settings.SupportedAgents) {
		managedPaths[expected] = true
	}

	current := map[string]UpgradeFileState{}
	fingerprintParts := []string{"MANIFEST\x00" + sha256Hex(raw)}
	for relPath := range managedPaths {
		state := inspectManagedFile(root, relPath)
		current[relPath] = state
		fingerprintParts = append(fingerprintParts, relPath+"\x00"+state.Kind+"\x00"+state.NormalizedSHA256)
	}

	_, gitErr := root.Lstat(".git")
	gitDetected := gitErr == nil

	sort.Strings(fingerprintParts)
	fingerprint := sha256.Sum256([]byte(strings.Join(fingerprintParts, "\x00") + "\x00git=" + boolText(gitDetected)))

	return UpgradeInspection{
		Path:           abs,
		IdentitySHA256: targetIdentity(resolved),
		Fingerprint:    hex.EncodeToString(fingerprint[:]),
		GitDetected:    gitDetected,
		Manifest:       manifest,
		Current:        current,
	}, nil
}

func inspectManagedFile(root *os.Root, relPath string) UpgradeFileState {
	clean := filepath.FromSlash(relPath)
	info, err := root.Lstat(clean)
	if err != nil {
		if os.IsNotExist(err) {
			return UpgradeFileState{Kind: "MISSING"}
		}
		return UpgradeFileState{Kind: "UNREADABLE"}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return UpgradeFileState{Kind: "SYMLINK"}
	}
	if !info.Mode().IsRegular() {
		return UpgradeFileState{Kind: "TYPE_CONFLICT"}
	}
	if info.Size() > maxUpgradeFileBytes {
		return UpgradeFileState{Kind: "UNREADABLE"}
	}
	data, err := readRootFile(root, clean, maxUpgradeFileBytes)
	if err != nil {
		return UpgradeFileState{Kind: "UNREADABLE"}
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
	return UpgradeFileState{Kind: "REGULAR", NormalizedSHA256: sha256Hex([]byte(normalized))}
}

// expectedManagedPaths mirrors planner.renderedFiles: the permanent core plus a
// bridge per supported agent. The manifest is self-excluded from managed_files.
func expectedManagedPaths(agents []string) []string {
	paths := []string{"AGENTS.md", "docs/PROJECT.md"}
	for _, agent := range agents {
		switch agent {
		case "claude":
			paths = append(paths, "CLAUDE.md")
		case "gemini":
			paths = append(paths, "GEMINI.md")
		}
	}
	return paths
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
