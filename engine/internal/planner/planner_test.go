package planner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	"github.com/kimNarr/Exord-init/engine/internal/target"
)

func TestBuildCreatesDeterministicallyOrderedSafePlan(t *testing.T) {
	intent := protocol.SetupIntent{
		SchemaVersion: 1, Mode: "CREATE", Depth: "QUICK", ProjectSummary: "A test project",
		DocumentationLanguage: "en", SupportedAgents: []string{"gemini", "codex", "claude"},
	}
	plan, err := Build(intent, target.Inspection{IdentitySHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Fingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Spec.RiskSummary.DestructiveOperations != 0 {
		t.Fatal("CREATE plan must be non-destructive")
	}
	for i := 1; i < len(plan.Spec.Operations); i++ {
		if plan.Spec.Operations[i-1].Path > plan.Spec.Operations[i].Path {
			t.Fatal("operations are not sorted")
		}
	}
	if plan.SpecSHA256 == "" {
		t.Fatal("missing plan hash")
	}
}

func TestRenderedManifestTracksManagedFilesButNotItself(t *testing.T) {
	intent := protocol.SetupIntent{
		SchemaVersion: 1, Mode: "CREATE", Depth: "QUICK", ProjectSummary: "A test project",
		DocumentationLanguage: "en", SupportedAgents: []string{"codex", "claude"},
	}
	files, err := renderedFiles(intent, "123e4567-e89b-42d3-a456-426614174000", "ENABLED")
	if err != nil {
		t.Fatal(err)
	}
	var manifestContent string
	for _, file := range files {
		if file.Path == ".exord/manifest.json" {
			manifestContent = file.Content
		}
	}
	if manifestContent == "" {
		t.Fatal("manifest operation missing")
	}
	var manifest struct {
		ManagedFiles []struct {
			Path string `json:"path"`
		} `json:"managed_files"`
	}
	if err := json.Unmarshal([]byte(manifestContent), &manifest); err != nil {
		t.Fatal(err)
	}
	seenAgents := false
	for _, file := range manifest.ManagedFiles {
		if file.Path == ".exord/manifest.json" {
			t.Fatal("manifest must not hash itself")
		}
		if file.Path == "AGENTS.md" {
			seenAgents = true
		}
	}
	if !seenAgents {
		t.Fatal("AGENTS.md missing from managed files")
	}
}

func TestDecodeIntentRejectsUnknownAndTrailingFields(t *testing.T) {
	for _, raw := range []string{
		`{"schema_version":1,"mode":"CREATE","depth":"QUICK","project_summary":"x","documentation_language":"en","supported_agents":["codex"],"unexpected":true}`,
		`{"schema_version":1,"mode":"CREATE","depth":"QUICK","project_summary":"x","documentation_language":"en","supported_agents":["codex"]} {}`,
	} {
		if _, err := DecodeIntent([]byte(raw)); err == nil {
			t.Fatalf("expected invalid intent: %s", raw)
		}
	}
}

func TestStableProjectIDProducesStableSpecHash(t *testing.T) {
	intent := protocol.SetupIntent{SchemaVersion: 1, Mode: "CREATE", Depth: "QUICK", ProjectSummary: "stable", DocumentationLanguage: "en", SupportedAgents: []string{"codex"}}
	inspection := target.Inspection{IdentitySHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Fingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	first, err := PrepareWithProjectID(intent, inspection, "123e4567-e89b-42d3-a456-426614174000")
	if err != nil {
		t.Fatal(err)
	}
	second, err := PrepareWithProjectID(intent, inspection, "123e4567-e89b-42d3-a456-426614174000")
	if err != nil {
		t.Fatal(err)
	}
	if first.Plan.SpecSHA256 != second.Plan.SpecSHA256 {
		t.Fatal("same project identity and target state produced different spec hashes")
	}
	if first.Plan.PlanID == second.Plan.PlanID {
		t.Fatal("separate plan envelopes must retain unique plan IDs")
	}
}

func TestBuildAdoptProposesOnlyMissingFilesAndClassifiesConflict(t *testing.T) {
	// Resolve links so target inspection accepts the path on Windows/macOS CI,
	// where t.TempDir returns a short-name or /var-symlinked path.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("user rules\n"), 0600); err != nil {
		t.Fatal(err)
	}
	inspection, err := target.InspectAdopt(dir)
	if err != nil {
		t.Fatal(err)
	}
	intent := protocol.SetupIntent{SchemaVersion: 1, Mode: "ADOPT", Depth: "QUICK", ProjectSummary: "adopt", DocumentationLanguage: "en", SupportedAgents: []string{"codex", "claude", "gemini"}}
	plan, err := BuildAdopt(intent, inspection, "123e4567-e89b-42d3-a456-426614174000")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Spec.AdoptAnalysis == nil {
		t.Fatal("missing ADOPT analysis")
	}
	for _, operation := range plan.Spec.Operations {
		if operation.Path == "AGENTS.md" {
			t.Fatal("ADOPT proposed overwriting user-owned AGENTS.md")
		}
	}
	foundConflict := false
	for _, conflict := range plan.Spec.AdoptAnalysis.Conflicts {
		if conflict.Path == "AGENTS.md" && conflict.Reason == "USER_OWNED" {
			foundConflict = true
		}
	}
	if !foundConflict {
		t.Fatal("user-owned AGENTS.md conflict was not reported")
	}
	second, err := BuildAdopt(intent, inspection, "123e4567-e89b-42d3-a456-426614174000")
	if err != nil {
		t.Fatal(err)
	}
	if plan.SpecSHA256 != second.SpecSHA256 {
		t.Fatal("same ADOPT inspection and project identity produced different spec hashes")
	}
	if plan.PlanID == second.PlanID {
		t.Fatal("separate ADOPT plan envelopes must retain unique plan IDs")
	}
}
