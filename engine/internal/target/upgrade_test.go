package target

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeManifest(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".exord"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".exord", "manifest.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const validUpgradeManifest = `{
  "schema_version": 1,
  "generator": {"name": "exord-init", "version": "0.3.0"},
  "project_id": "123e4567-e89b-42d3-a456-426614174000",
  "settings": {
    "git_mode": "ENABLED", "branch_profile": "SIMPLE",
    "languages": {"documentation": "en"},
    "supported_agents": ["codex", "claude"]
  },
  "managed_files": [
    {"path": "AGENTS.md", "template_id": "agents-core-en", "template_version": 1,
     "baseline_sha256": "0000000000000000000000000000000000000000000000000000000000000000",
     "hash_mode": "TEXT_UTF8_LF_V1", "update_policy": "PROPOSE_ONLY"}
  ]
}`

func TestInspectUpgradeReadsManifestAndFileState(t *testing.T) {
	dir := canonicalTempDir(t)
	writeManifest(t, dir, validUpgradeManifest)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("line one\r\nline two\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	inspection, err := InspectUpgrade(dir)
	if err != nil {
		t.Fatalf("InspectUpgrade failed: %v", err)
	}
	if inspection.Manifest.ProjectID != "123e4567-e89b-42d3-a456-426614174000" {
		t.Fatalf("unexpected project id: %s", inspection.Manifest.ProjectID)
	}
	if !inspection.GitDetected {
		t.Fatal("expected .git to be detected")
	}
	agents := inspection.Current["AGENTS.md"]
	if agents.Kind != "REGULAR" {
		t.Fatalf("AGENTS.md kind: %s", agents.Kind)
	}
	// CRLF must be normalized before hashing so a Windows checkout still matches.
	lfInspection := canonicalTempDir(t)
	if err := os.WriteFile(filepath.Join(lfInspection, "x"), []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := sha256Hex([]byte("line one\nline two\n"))
	if agents.NormalizedSHA256 != want {
		t.Fatalf("CRLF not normalized before hashing: got %s want %s", agents.NormalizedSHA256, want)
	}
	// CLAUDE.md is expected (claude agent) but absent -> MISSING, not an error.
	if inspection.Current["CLAUDE.md"].Kind != "MISSING" {
		t.Fatalf("CLAUDE.md should be MISSING, got %s", inspection.Current["CLAUDE.md"].Kind)
	}
}

func TestInspectUpgradeRejectsNonExordTarget(t *testing.T) {
	if _, err := InspectUpgrade(canonicalTempDir(t)); !errors.Is(err, ErrNotExordProject) {
		t.Fatalf("expected ErrNotExordProject, got %v", err)
	}
}

func TestInspectUpgradeRejectsUnknownSchemaVersion(t *testing.T) {
	dir := canonicalTempDir(t)
	writeManifest(t, dir, `{"schema_version": 2, "generator": {"name": "exord-init", "version": "1.0.0"}, "project_id": "123e4567-e89b-42d3-a456-426614174000", "settings": {}, "managed_files": []}`)
	if _, err := InspectUpgrade(dir); !errors.Is(err, ErrManifestSchemaUnsupported) {
		t.Fatalf("expected ErrManifestSchemaUnsupported, got %v", err)
	}
}

func TestInspectUpgradeRejectsForeignGenerator(t *testing.T) {
	dir := canonicalTempDir(t)
	writeManifest(t, dir, `{"schema_version": 1, "generator": {"name": "other-tool", "version": "1.0.0"}, "project_id": "123e4567-e89b-42d3-a456-426614174000", "settings": {}, "managed_files": []}`)
	if _, err := InspectUpgrade(dir); !errors.Is(err, ErrNotExordProject) {
		t.Fatalf("expected ErrNotExordProject for a foreign generator, got %v", err)
	}
}
