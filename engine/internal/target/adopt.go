package target

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	"golang.org/x/text/unicode/norm"
)

const (
	maxInventoryEntries = 200_000
	maxHashedFileBytes  = 16 << 20
	maxTotalHashedBytes = 512 << 20
	maxKnownFileBytes   = 1 << 20
	maxSecretScanBytes  = 64 << 20
)

var adoptKnownPaths = []string{
	".exord/manifest.json", "AGENTS.md", "CLAUDE.md", "GEMINI.md",
	"TASK.md", ".exord/TASK.md", "docs/PROJECT.md",
}

var excludedInventoryDirectories = map[string]bool{
	"node_modules": true, ".venv": true, "venv": true, "__pycache__": true,
}

type AdoptInspection struct {
	Path              string                      `json:"path"`
	IdentitySHA256    string                      `json:"identity_sha256"`
	Fingerprint       string                      `json:"fingerprint"`
	Inventory         protocol.InventorySummary   `json:"inventory"`
	Git               protocol.GitAnalysis        `json:"git"`
	Task              protocol.TaskAnalysis       `json:"task"`
	Guidance          []protocol.GuidanceAnalysis `json:"guidance"`
	ManifestProjectID string                      `json:"manifest_project_id,omitempty"`
	entries           map[string]adoptEntry
	managedBaselines  map[string]string
}

type adoptEntry struct {
	Path   string
	Kind   string
	Size   int64
	SHA256 string
}

type manifestSubset struct {
	SchemaVersion int    `json:"schema_version"`
	ProjectID     string `json:"project_id"`
	ManagedFiles  []struct {
		Path           string `json:"path"`
		BaselineSHA256 string `json:"baseline_sha256"`
	} `json:"managed_files"`
}

func InspectAdopt(path string) (AdoptInspection, error) {
	abs, resolved, err := resolveSafeRoot(path)
	if err != nil {
		return AdoptInspection{}, err
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return AdoptInspection{}, err
	}
	defer root.Close()

	inspection := AdoptInspection{Path: abs, IdentitySHA256: targetIdentity(resolved), entries: map[string]adoptEntry{}, managedBaselines: map[string]string{}}
	fingerprintEntries := []string{}
	normalizedSeen := map[string]bool{}
	entryCount := 0
	var hashedBytes int64
	var secretScannedBytes int64
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}
		entryCount++
		if entryCount > maxInventoryEntries {
			return errors.New("inventory entry limit exceeded")
		}
		if !utf8.ValidString(path) {
			return errors.New("inventory contains a non-UTF-8 path")
		}
		normalized := norm.NFC.String(filepath.ToSlash(path))
		if strings.Count(normalized, "/") > 64 {
			return errors.New("inventory path depth limit exceeded")
		}
		if normalizedSeen[normalized] {
			return errors.New("inventory contains NFC-colliding paths")
		}
		normalizedSeen[normalized] = true
		name := entry.Name()
		if entry.IsDir() {
			if name == ".git" {
				inspection.putEntry(adoptEntry{Path: normalized, Kind: "GIT_DIRECTORY"})
				fingerprintEntries = append(fingerprintEntries, normalized+"\x00GIT_DIRECTORY")
				return fs.SkipDir
			}
			if excludedInventoryDirectories[strings.ToLower(name)] {
				inspection.Inventory.ExcludedDirectories++
				fingerprintEntries = append(fingerprintEntries, normalized+"\x00EXCLUDED_DIRECTORY")
				return fs.SkipDir
			}
			nestedGit, err := hasNestedGitMarker(root, filepath.FromSlash(path))
			if err != nil {
				return err
			}
			if nestedGit {
				inspection.Inventory.NestedGitRepositories++
				inspection.putEntry(adoptEntry{Path: normalized, Kind: "NESTED_GIT_DIRECTORY"})
				fingerprintEntries = append(fingerprintEntries, normalized+"\x00NESTED_GIT_DIRECTORY")
				return fs.SkipDir
			}
			inspection.Inventory.Directories++
			inspection.putEntry(adoptEntry{Path: normalized, Kind: "DIRECTORY"})
			fingerprintEntries = append(fingerprintEntries, normalized+"\x00DIRECTORY")
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		kind := "SPECIAL"
		if info.Mode()&os.ModeSymlink != 0 {
			kind = "SYMLINK"
			inspection.Inventory.Symlinks++
		} else if info.Mode().IsRegular() {
			kind = "REGULAR"
			inspection.Inventory.Files++
			if info.Size() > 0 && inspection.Inventory.TotalBytes > math.MaxInt64-info.Size() {
				return errors.New("inventory byte count overflow")
			}
			inspection.Inventory.TotalBytes += info.Size()
		} else {
			inspection.Inventory.SpecialFiles++
		}
		item := adoptEntry{Path: normalized, Kind: kind, Size: info.Size()}
		if kind == "REGULAR" && normalized != ".git" {
			if info.Size() <= maxHashedFileBytes && hashedBytes <= maxTotalHashedBytes-info.Size() {
				item.SHA256, err = hashRootFile(root, filepath.FromSlash(path), maxHashedFileBytes)
				if err != nil {
					return err
				}
				hashedBytes += info.Size()
			} else {
				inspection.Inventory.UnhashedFiles++
				if info.Size() > maxHashedFileBytes {
					inspection.Inventory.LargeFiles++
				}
			}
			secretCandidate := secretNameCandidate(normalized)
			if info.Size() <= maxKnownFileBytes && secretScannedBytes <= maxSecretScanBytes-info.Size() {
				secretCandidate = secretCandidate || secretContentCandidate(root, filepath.FromSlash(path))
				secretScannedBytes += info.Size()
			} else {
				inspection.Inventory.SecretScanLimited = true
			}
			if secretCandidate {
				inspection.Inventory.SecretCandidates++
			}
		}
		inspection.putEntry(item)
		fingerprintEntries = append(fingerprintEntries, normalized+"\x00"+kind+"\x00"+decimalSize(info.Size())+"\x00"+item.SHA256)
		return nil
	})
	if err != nil {
		return AdoptInspection{}, err
	}

	inspection.readManifest(root)
	inspection.Guidance = inspection.guidanceAnalysis()
	inspection.Task = inspection.taskAnalysis(root)
	inspection.Git = inspectGit(abs, inspection.entries[pathKey(".git")])
	fingerprintEntries = append(fingerprintEntries, gitFingerprint(inspection.Git))
	sort.Strings(fingerprintEntries)
	sum := sha256.Sum256([]byte(strings.Join(fingerprintEntries, "\x00")))
	inspection.Fingerprint = hex.EncodeToString(sum[:])
	return inspection, nil
}

func hasNestedGitMarker(root *os.Root, path string) (bool, error) {
	_, err := root.Lstat(filepath.Join(path, ".git"))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (inspection AdoptInspection) Entry(path string) (string, string, string, bool) {
	entry, ok := inspection.entries[pathKey(norm.NFC.String(filepath.ToSlash(path)))]
	return entry.Path, entry.Kind, entry.SHA256, ok
}

func (inspection *AdoptInspection) putEntry(entry adoptEntry) {
	key := pathKey(entry.Path)
	if previous, exists := inspection.entries[key]; exists && previous.Path != entry.Path {
		inspection.entries[key] = adoptEntry{Path: previous.Path, Kind: "CASE_CONFLICT"}
		return
	}
	inspection.entries[key] = entry
}

func (inspection *AdoptInspection) readManifest(root *os.Root) {
	entry, ok := inspection.entries[pathKey(".exord/manifest.json")]
	if !ok || entry.Kind != "REGULAR" || entry.Size > maxKnownFileBytes {
		return
	}
	data, err := readRootFile(root, filepath.FromSlash(entry.Path), maxKnownFileBytes)
	if err != nil {
		return
	}
	var manifest manifestSubset
	if json.Unmarshal(data, &manifest) != nil || manifest.SchemaVersion != 1 || !validUUID(manifest.ProjectID) {
		return
	}
	inspection.ManifestProjectID = manifest.ProjectID
	for _, file := range manifest.ManagedFiles {
		if validSHA256(file.BaselineSHA256) {
			inspection.managedBaselines[pathKey(norm.NFC.String(filepath.ToSlash(file.Path)))] = file.BaselineSHA256
		}
	}
}

func (inspection AdoptInspection) guidanceAnalysis() []protocol.GuidanceAnalysis {
	result := make([]protocol.GuidanceAnalysis, 0, len(adoptKnownPaths))
	for _, wanted := range adoptKnownPaths {
		entry, exists := inspection.entries[pathKey(wanted)]
		analysis := protocol.GuidanceAnalysis{Path: wanted, Status: "MISSING"}
		if exists {
			analysis.ActualPath = entry.Path
			analysis.SHA256 = entry.SHA256
			switch entry.Kind {
			case "SYMLINK":
				analysis.Status = "LINKED_PATH"
			case "REGULAR":
				baseline, managed := inspection.managedBaselines[pathKey(wanted)]
				if managed && baseline == entry.SHA256 {
					analysis.Status = "MANAGED_UNMODIFIED"
				} else if managed {
					analysis.Status = "MANAGED_MODIFIED"
				} else {
					analysis.Status = "USER_OWNED"
				}
			default:
				analysis.Status = "TYPE_CONFLICT"
			}
		}
		result = append(result, analysis)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}

func (inspection AdoptInspection) taskAnalysis(root *os.Root) protocol.TaskAnalysis {
	result := protocol.TaskAnalysis{Path: "TASK.md", Status: "MISSING", AlternativePath: ".exord/TASK.md"}
	entry, exists := inspection.entries[pathKey("TASK.md")]
	if !exists {
		if alternative, ok := inspection.entries[pathKey(".exord/TASK.md")]; ok {
			result.Path = alternative.Path
			result.Status = classifyTask(root, alternative)
		}
		return result
	}
	result.Path = entry.Path
	result.Status = classifyTask(root, entry)
	return result
}

func classifyTask(root *os.Root, entry adoptEntry) string {
	if entry.Kind == "SYMLINK" {
		return "LINKED_PATH"
	}
	if entry.Kind != "REGULAR" || entry.Size > maxKnownFileBytes {
		return "USER_OWNED"
	}
	data, err := readRootFile(root, filepath.FromSlash(entry.Path), maxKnownFileBytes)
	if err != nil {
		return "UNREADABLE"
	}
	text := string(data)
	if strings.Contains(text, "task_id:") && strings.Contains(text, "status:") && strings.Contains(text, "## Goal") {
		return "EXORD_TASK"
	}
	return "USER_OWNED"
}

func inspectGit(targetPath string, marker adoptEntry) protocol.GitAnalysis {
	result := protocol.GitAnalysis{Detected: marker.Path != "", GitFile: marker.Kind == "REGULAR", Status: "NOT_GIT"}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		if result.Detected {
			result.Status = "CLI_UNAVAILABLE"
		}
		return result
	}
	result.CLIAvailable = true
	top, err := runGit(gitPath, targetPath, "rev-parse", "--show-toplevel")
	if err != nil {
		if result.Detected {
			result.Status = "INVALID_REPOSITORY"
		}
		return result
	}
	result.Detected = true
	result.RootMatch = samePath(strings.TrimSpace(top), targetPath)
	if !result.RootMatch {
		result.Status = "NESTED_DIRECTORY"
		return result
	}
	result.Status = "OK"
	if branch, err := runGit(gitPath, targetPath, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		result.Branch = strings.TrimSpace(branch)
	}
	if head, err := runGit(gitPath, targetPath, "rev-parse", "--verify", "HEAD"); err == nil {
		result.Head = strings.TrimSpace(head)
	} else {
		result.Unborn = true
	}
	if status, err := runGit(gitPath, targetPath, "status", "--porcelain=v1", "--untracked-files=no"); err == nil {
		result.TrackedDirty = strings.TrimSpace(status) != ""
	} else {
		result.Status = "STATUS_UNAVAILABLE"
	}
	if untracked, err := runGitBytes(gitPath, targetPath, "ls-files", "--others", "--exclude-standard", "-z"); err == nil {
		result.UntrackedFiles = bytes.Count(untracked, []byte{0})
	} else {
		result.Status = "STATUS_UNAVAILABLE"
	}
	for _, operation := range []string{"MERGE_HEAD", "REBASE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD"} {
		if _, err := runGit(gitPath, targetPath, "rev-parse", "--quiet", "--verify", operation); err == nil {
			result.InProgress = strings.TrimSuffix(operation, "_HEAD")
			break
		}
	}
	if result.InProgress == "" {
		for _, marker := range []string{"rebase-merge", "rebase-apply"} {
			gitStatePath, err := runGit(gitPath, targetPath, "rev-parse", "--git-path", marker)
			if err != nil {
				continue
			}
			gitStatePath = strings.TrimSpace(gitStatePath)
			if !filepath.IsAbs(gitStatePath) {
				gitStatePath = filepath.Join(targetPath, gitStatePath)
			}
			if _, err := os.Lstat(gitStatePath); err == nil {
				result.InProgress = "REBASE"
				break
			} else if !os.IsNotExist(err) {
				result.Status = "STATUS_UNAVAILABLE"
			}
		}
	}
	return result
}

func runGit(gitPath, targetPath string, args ...string) (string, error) {
	data, err := runGitBytes(gitPath, targetPath, args...)
	return string(data), err
}

func runGitBytes(gitPath, targetPath string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	commandArgs := append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "maintenance.auto=false", "-C", targetPath}, args...)
	command := exec.CommandContext(ctx, gitPath, commandArgs...)
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	command.Stderr = io.Discard
	data, err := command.Output()
	if ctx.Err() != nil {
		return nil, errors.New("Git inspection timed out")
	}
	return data, err
}

func readRootFile(root *os.Root, path string, limit int64) ([]byte, error) {
	file, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("file exceeds inspection limit")
	}
	return data, nil
}

func hashRootFile(root *os.Root, path string, limit int64) (string, error) {
	data, err := readRootFile(root, path, limit)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func gitFingerprint(git protocol.GitAnalysis) string {
	return strings.Join([]string{"GIT", git.Status, git.Branch, git.Head, boolText(git.TrackedDirty), decimalSize(int64(git.UntrackedFiles)), git.InProgress}, "\x00")
}

func pathKey(path string) string { return strings.ToLower(norm.NFC.String(filepath.ToSlash(path))) }

func decimalSize(value int64) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	buffer := [24]byte{}
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		index--
		buffer[index] = '-'
	}
	return string(buffer[index:])
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func validUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, char := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
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

func secretNameCandidate(path string) bool {
	name := strings.ToLower(filepath.Base(filepath.FromSlash(path)))
	if name == ".env" || strings.HasPrefix(name, ".env.") {
		return true
	}
	for _, candidate := range []string{"id_rsa", "id_ed25519", ".npmrc", ".pypirc", ".netrc", "credentials.json", "service-account.json"} {
		if name == candidate {
			return true
		}
	}
	return false
}

func secretContentCandidate(root *os.Root, path string) bool {
	data, err := readRootFile(root, path, maxKnownFileBytes)
	if err != nil {
		return false
	}
	for _, marker := range [][]byte{
		[]byte("-----BEGIN RSA PRIVATE KEY-----"),
		[]byte("-----BEGIN OPENSSH PRIVATE KEY-----"),
		[]byte("-----BEGIN EC PRIVATE KEY-----"),
		[]byte("github_pat_"),
		[]byte("ghp_"),
	} {
		if bytes.Contains(data, marker) {
			return true
		}
	}
	return false
}
