package planner

import (
	"encoding/json"
	"testing"

	"example.com/exord-init/engine/internal/protocol"
	"example.com/exord-init/engine/internal/target"
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
	files, err := renderedFiles(intent, "123e4567-e89b-42d3-a456-426614174000")
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
