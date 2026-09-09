package target

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var harmless = map[string]bool{".git": true, ".DS_Store": true, "Thumbs.db": true, "desktop.ini": true}

type Inspection struct {
	Path           string   `json:"path"`
	IdentitySHA256 string   `json:"identity_sha256"`
	Fingerprint    string   `json:"fingerprint"`
	Entries        []string `json:"entries"`
}

func InspectCreate(path string) (Inspection, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Inspection{}, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return Inspection{}, err
	}
	if !info.IsDir() {
		return Inspection{}, errors.New("target is not a directory")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Inspection{}, errors.New("symlink target is not supported")
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return Inspection{}, err
	}
	if !samePath(abs, resolved) {
		return Inspection{}, errors.New("target path traverses a symbolic link or junction")
	}
	volume := filepath.VolumeName(abs) + string(os.PathSeparator)
	if samePath(abs, volume) {
		return Inspection{}, errors.New("filesystem root is not a valid target")
	}
	if home, err := os.UserHomeDir(); err == nil && samePath(abs, home) {
		return Inspection{}, errors.New("user home is not a valid target")
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return Inspection{}, err
	}
	names := make([]string, 0, len(entries))
	fingerprintEntries := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !harmless[name] {
			return Inspection{}, fmt.Errorf("CREATE target contains user-owned entry: %s", name)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return Inspection{}, fmt.Errorf("CREATE target contains symlink: %s", name)
		}
		names = append(names, name)
		kind := "file"
		if entry.IsDir() {
			kind = "directory"
		}
		fingerprintEntries = append(fingerprintEntries, name+"\x00"+kind)
	}
	sort.Strings(names)
	sort.Strings(fingerprintEntries)
	sum := sha256.Sum256([]byte(strings.Join(fingerprintEntries, "\x00")))
	identityPath := filepath.ToSlash(filepath.Clean(resolved))
	if filepath.Separator == '\\' {
		identityPath = strings.ToLower(identityPath)
	}
	identity := sha256.Sum256([]byte(identityPath))
	return Inspection{Path: abs, IdentitySHA256: hex.EncodeToString(identity[:]), Fingerprint: hex.EncodeToString(sum[:]), Entries: names}, nil
}

func samePath(a, b string) bool {
	aa := strings.TrimRight(filepath.Clean(a), `\/`)
	bb := strings.TrimRight(filepath.Clean(b), `\/`)
	return strings.EqualFold(aa, bb)
}
