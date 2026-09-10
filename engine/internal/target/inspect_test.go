package target

import (
	"os"
	"path/filepath"
	"testing"
)

// canonicalTempDir returns a fully resolved temp directory. t.TempDir alone is
// not safe for target inspection: on Windows CI runners it carries 8.3 short
// names and on macOS it lives under the /var -> /private/var symlink, both of
// which resolveSafeRoot correctly rejects as link traversal.
func canonicalTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInspectCreateAcceptsEmptyDirectory(t *testing.T) {
	result, err := InspectCreate(canonicalTempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 0 {
		t.Fatalf("unexpected entries: %v", result.Entries)
	}
}

func TestInspectCreateRejectsUserOwnedEntry(t *testing.T) {
	dir := canonicalTempDir(t)
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectCreate(dir); err == nil {
		t.Fatal("expected rejection")
	}
}

func TestSamePathUsesPlatformCaseSemantics(t *testing.T) {
	equal := samePath(filepath.Join("root", "Project"), filepath.Join("root", "project"))
	if filepath.Separator == '\\' && !equal {
		t.Fatal("Windows paths should be compared case-insensitively")
	}
	if filepath.Separator != '\\' && equal {
		t.Fatal("case-sensitive platform paths must not be folded")
	}
}
