package target

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectCreateAcceptsEmptyDirectory(t *testing.T) {
	result, err := InspectCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 0 {
		t.Fatalf("unexpected entries: %v", result.Entries)
	}
}

func TestInspectCreateRejectsUserOwnedEntry(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectCreate(dir); err == nil {
		t.Fatal("expected rejection")
	}
}
