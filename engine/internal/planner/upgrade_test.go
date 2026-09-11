package planner

import (
	"testing"

	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	"github.com/kimNarr/Exord-init/engine/internal/target"
)

const upgradeProjectID = "123e4567-e89b-42d3-a456-426614174000"
const zeroHash = "0000000000000000000000000000000000000000000000000000000000000000"
const oneHash = "1111111111111111111111111111111111111111111111111111111111111111"

func baseInspection() target.UpgradeInspection {
	return target.UpgradeInspection{
		Path:           "/tmp/project",
		IdentitySHA256: zeroHash,
		Fingerprint:    oneHash,
		GitDetected:    true,
		Manifest: target.ManifestDocument{
			SchemaVersion: 1,
			ProjectID:     upgradeProjectID,
			ManagedFiles: []target.ManifestManagedFile{
				{Path: "AGENTS.md", TemplateID: "agents-core-en", TemplateVersion: 1, BaselineSHA256: zeroHash, HashMode: "TEXT_UTF8_LF_V1", UpdatePolicy: "PROPOSE_ONLY"},
				{Path: "CLAUDE.md", TemplateID: "claude-bridge-v1", TemplateVersion: 1, BaselineSHA256: zeroHash, HashMode: "TEXT_UTF8_LF_V1", UpdatePolicy: "REPLACE_IF_UNMODIFIED"},
				{Path: "docs/PROJECT.md", TemplateID: "project-core-en", TemplateVersion: 1, BaselineSHA256: zeroHash, HashMode: "TEXT_UTF8_LF_V1", UpdatePolicy: "PROPOSE_ONLY"},
			},
		},
	}
	// Settings set by callers.
}

func withSettings(inspection target.UpgradeInspection) target.UpgradeInspection {
	inspection.Manifest.Generator.Name = "exord-init"
	inspection.Manifest.Generator.Version = "0.4.0-dev"
	inspection.Manifest.Settings.GitMode = "ENABLED"
	inspection.Manifest.Settings.BranchProfile = "SIMPLE"
	inspection.Manifest.Settings.Languages.Documentation = "en"
	inspection.Manifest.Settings.SupportedAgents = []string{"codex", "claude"}
	return inspection
}

func matchedCurrent() map[string]target.UpgradeFileState {
	return map[string]target.UpgradeFileState{
		"AGENTS.md":       {Kind: "REGULAR", NormalizedSHA256: zeroHash},
		"CLAUDE.md":       {Kind: "REGULAR", NormalizedSHA256: zeroHash},
		"docs/PROJECT.md": {Kind: "REGULAR", NormalizedSHA256: zeroHash},
	}
}

func fileByPath(t *testing.T, analysis *protocol.UpgradeAnalysis, path string) protocol.UpgradeFileAnalysis {
	t.Helper()
	for _, file := range analysis.Files {
		if file.Path == path {
			return file
		}
	}
	t.Fatalf("no upgrade file analysis for %s", path)
	return protocol.UpgradeFileAnalysis{}
}

func TestBuildUpgradeUpToDate(t *testing.T) {
	inspection := withSettings(baseInspection())
	inspection.Current = matchedCurrent()
	plan, err := BuildUpgrade(inspection)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Spec.Mode != "UPGRADE" || plan.Spec.ProjectID != upgradeProjectID {
		t.Fatalf("unexpected spec: %#v", plan.Spec)
	}
	analysis := plan.Spec.UpgradeAnalysis
	if analysis.Disposition != "UP_TO_DATE" {
		t.Fatalf("expected UP_TO_DATE, got %s", analysis.Disposition)
	}
	if analysis.Generator.Direction != "SAME" {
		t.Fatalf("expected SAME generator direction, got %s", analysis.Generator.Direction)
	}
	for _, file := range analysis.Files {
		if file.UpgradeAction != "UP_TO_DATE" {
			t.Fatalf("%s: expected UP_TO_DATE, got %s", file.Path, file.UpgradeAction)
		}
	}
}

func TestBuildUpgradeDeterministicHashDistinctPlanID(t *testing.T) {
	inspection := withSettings(baseInspection())
	inspection.Current = matchedCurrent()
	first, err := BuildUpgrade(inspection)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildUpgrade(inspection)
	if err != nil {
		t.Fatal(err)
	}
	if first.SpecSHA256 != second.SpecSHA256 {
		t.Fatal("same inspection produced different spec hashes")
	}
	if first.PlanID == second.PlanID {
		t.Fatal("plan envelope IDs must stay unique")
	}
}

func TestBuildUpgradeClassifiesLocalEditMissingAndConflict(t *testing.T) {
	inspection := withSettings(baseInspection())
	inspection.Current = map[string]target.UpgradeFileState{
		"AGENTS.md":       {Kind: "REGULAR", NormalizedSHA256: oneHash}, // edited
		"CLAUDE.md":       {Kind: "MISSING"},
		"docs/PROJECT.md": {Kind: "SYMLINK"},
	}
	plan, err := BuildUpgrade(inspection)
	if err != nil {
		t.Fatal(err)
	}
	analysis := plan.Spec.UpgradeAnalysis
	if got := fileByPath(t, analysis, "AGENTS.md").UpgradeAction; got != "REVIEW_LOCAL_EDIT" {
		t.Fatalf("AGENTS.md: got %s", got)
	}
	if got := fileByPath(t, analysis, "CLAUDE.md").UpgradeAction; got != "RECREATE_MISSING" {
		t.Fatalf("CLAUDE.md: got %s", got)
	}
	if got := fileByPath(t, analysis, "docs/PROJECT.md").UpgradeAction; got != "BLOCKED" {
		t.Fatalf("docs/PROJECT.md: got %s", got)
	}
	if analysis.Disposition != "BLOCKED" {
		t.Fatalf("a symlinked managed file must block: %s", analysis.Disposition)
	}
}

func TestBuildUpgradeTemplateUpdateRespectsPolicy(t *testing.T) {
	inspection := withSettings(baseInspection())
	inspection.Current = matchedCurrent()
	// Pretend the generator advanced both templates.
	currentTemplateVersions["agents-core-en"] = 2
	currentTemplateVersions["claude-bridge-v1"] = 2
	defer func() {
		currentTemplateVersions["agents-core-en"] = 1
		currentTemplateVersions["claude-bridge-v1"] = 1
	}()
	plan, err := BuildUpgrade(inspection)
	if err != nil {
		t.Fatal(err)
	}
	analysis := plan.Spec.UpgradeAnalysis
	if got := fileByPath(t, analysis, "AGENTS.md").UpgradeAction; got != "PROPOSE_ONLY_UPDATE" {
		t.Fatalf("PROPOSE_ONLY template bump: got %s", got)
	}
	if got := fileByPath(t, analysis, "CLAUDE.md").UpgradeAction; got != "TEMPLATE_UPDATE_AVAILABLE" {
		t.Fatalf("REPLACE_IF_UNMODIFIED template bump: got %s", got)
	}
}

func TestBuildUpgradeRefusesDowngrade(t *testing.T) {
	inspection := withSettings(baseInspection())
	inspection.Manifest.Generator.Version = "9.0.0"
	inspection.Current = matchedCurrent()
	plan, err := BuildUpgrade(inspection)
	if err != nil {
		t.Fatal(err)
	}
	analysis := plan.Spec.UpgradeAnalysis
	if analysis.Generator.Direction != "DOWNGRADE" || analysis.Disposition != "BLOCKED" {
		t.Fatalf("expected blocked downgrade, got %s / %s", analysis.Generator.Direction, analysis.Disposition)
	}
}

func TestBuildUpgradeFlagsNewAndOrphanManagedFiles(t *testing.T) {
	inspection := withSettings(baseInspection())
	// gemini is now a supported agent but GEMINI.md is not in the manifest;
	// claude is no longer supported but CLAUDE.md still is recorded.
	inspection.Manifest.Settings.SupportedAgents = []string{"codex", "gemini"}
	inspection.Current = map[string]target.UpgradeFileState{
		"AGENTS.md":       {Kind: "REGULAR", NormalizedSHA256: zeroHash},
		"CLAUDE.md":       {Kind: "REGULAR", NormalizedSHA256: zeroHash},
		"docs/PROJECT.md": {Kind: "REGULAR", NormalizedSHA256: zeroHash},
		"GEMINI.md":       {Kind: "MISSING"},
	}
	plan, err := BuildUpgrade(inspection)
	if err != nil {
		t.Fatal(err)
	}
	analysis := plan.Spec.UpgradeAnalysis
	if got := fileByPath(t, analysis, "GEMINI.md").UpgradeAction; got != "NEW_MANAGED_FILE" {
		t.Fatalf("GEMINI.md: got %s", got)
	}
	if got := fileByPath(t, analysis, "CLAUDE.md").UpgradeAction; got != "ORPHAN_MANAGED_FILE" {
		t.Fatalf("CLAUDE.md: got %s", got)
	}
	if analysis.Disposition != "REVIEW_REQUIRED" {
		t.Fatalf("expected REVIEW_REQUIRED, got %s", analysis.Disposition)
	}
}

func TestBuildUpgradeUnknownHashModeBlocks(t *testing.T) {
	inspection := withSettings(baseInspection())
	inspection.Manifest.ManagedFiles[0].HashMode = "SOMETHING_NEW"
	inspection.Current = matchedCurrent()
	plan, err := BuildUpgrade(inspection)
	if err != nil {
		t.Fatal(err)
	}
	analysis := plan.Spec.UpgradeAnalysis
	if got := fileByPath(t, analysis, "AGENTS.md"); got.LocalState != "UNKNOWN_HASH_MODE" || got.UpgradeAction != "BLOCKED" {
		t.Fatalf("unknown hash mode: %#v", got)
	}
	if analysis.Disposition != "BLOCKED" {
		t.Fatalf("expected BLOCKED, got %s", analysis.Disposition)
	}
}
