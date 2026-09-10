package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	exordinit "github.com/kimNarr/Exord-init"
	"github.com/kimNarr/Exord-init/engine/internal/canonicaljson"
	"github.com/kimNarr/Exord-init/engine/internal/id"
	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	"github.com/kimNarr/Exord-init/engine/internal/target"
)

type PreparedFile struct {
	Path    string
	Content []byte
}

type PreparedPlan struct {
	Plan  protocol.SetupPlan
	Files []PreparedFile
}

func Build(intent protocol.SetupIntent, inspection target.Inspection) (protocol.SetupPlan, error) {
	prepared, err := Prepare(intent, inspection)
	return prepared.Plan, err
}

func Prepare(intent protocol.SetupIntent, inspection target.Inspection) (PreparedPlan, error) {
	projectID, err := id.UUID4()
	if err != nil {
		return PreparedPlan{}, err
	}
	return PrepareWithProjectID(intent, inspection, projectID)
}

func PrepareWithProjectID(intent protocol.SetupIntent, inspection target.Inspection, projectID string) (PreparedPlan, error) {
	if err := validateIntent(intent); err != nil {
		return PreparedPlan{}, err
	}
	if intent.Mode != "CREATE" || intent.Depth != "QUICK" {
		return PreparedPlan{}, errors.New("current engine supports CREATE + QUICK only")
	}
	if !validUUID(projectID) {
		return PreparedPlan{}, errors.New("invalid project ID")
	}
	planID, err := id.UUID4()
	if err != nil {
		return PreparedPlan{}, err
	}

	gitMode := "DOCUMENT_ONLY"
	for _, entry := range inspection.Entries {
		if entry == ".git" {
			gitMode = "ENABLED"
		}
	}
	files, err := renderedFiles(intent, projectID, gitMode)
	if err != nil {
		return PreparedPlan{}, err
	}
	operations := make([]protocol.Operation, 0, len(files))
	for _, file := range files {
		sum := sha256.Sum256([]byte(normalizeLF(file.Content)))
		operations = append(operations, protocol.Operation{
			Kind: "CREATE", Path: file.Path, TemplateID: file.TemplateID,
			ExpectedSHA256: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(operations, func(i, j int) bool {
		if operations[i].Path != operations[j].Path {
			return operations[i].Path < operations[j].Path
		}
		if operations[i].Kind != operations[j].Kind {
			return operations[i].Kind < operations[j].Kind
		}
		return operations[i].ExpectedSHA256 < operations[j].ExpectedSHA256
	})
	validation := []protocol.Validation{
		{ID: "agents-size-v1", Path: "AGENTS.md"},
		{ID: "manifest-schema-v1", Path: ".exord/manifest.json"},
		{ID: "project-required-sections-v1", Path: "docs/PROJECT.md"},
	}
	sort.Slice(validation, func(i, j int) bool {
		if validation[i].ID != validation[j].ID {
			return validation[i].ID < validation[j].ID
		}
		return validation[i].Path < validation[j].Path
	})
	spec := protocol.PlanSpec{
		Mode: intent.Mode, Depth: intent.Depth, ProjectID: projectID,
		TargetIdentitySHA256: inspection.IdentitySHA256, TargetFingerprint: inspection.Fingerprint,
		Operations: operations, Validation: validation, RiskSummary: protocol.RiskSummary{},
	}
	canonical, err := canonicaljson.Marshal(spec)
	if err != nil {
		return PreparedPlan{}, err
	}
	sum := sha256.Sum256(canonical)
	plan := protocol.SetupPlan{
		SchemaVersion: 1, PlanID: planID, TargetFingerprint: inspection.Fingerprint,
		Spec: spec, SpecSHA256: hex.EncodeToString(sum[:]),
	}
	preparedFiles := make([]PreparedFile, 0, len(files))
	for _, file := range files {
		preparedFiles = append(preparedFiles, PreparedFile{Path: file.Path, Content: []byte(normalizeLF(file.Content))})
	}
	return PreparedPlan{Plan: plan, Files: preparedFiles}, nil
}

func BuildAdopt(intent protocol.SetupIntent, inspection target.AdoptInspection, projectID string) (protocol.SetupPlan, error) {
	if err := validateIntent(intent); err != nil {
		return protocol.SetupPlan{}, err
	}
	if intent.Mode != "ADOPT" || intent.Depth != "QUICK" {
		return protocol.SetupPlan{}, errors.New("current ADOPT planner supports QUICK only")
	}
	if !validUUID(projectID) {
		return protocol.SetupPlan{}, errors.New("invalid project ID")
	}
	planID, err := id.UUID4()
	if err != nil {
		return protocol.SetupPlan{}, err
	}
	gitMode := "DOCUMENT_ONLY"
	if inspection.Git.Detected && inspection.Git.RootMatch {
		gitMode = "ENABLED"
	}
	allFiles, err := renderedFiles(intent, projectID, gitMode)
	if err != nil {
		return protocol.SetupPlan{}, err
	}
	guidance := map[string]protocol.GuidanceAnalysis{}
	for _, item := range inspection.Guidance {
		guidance[item.Path] = item
	}
	conflicts := []protocol.Conflict{}
	candidates := []rendered{}
	for _, file := range allFiles {
		if file.Path == ".exord/manifest.json" {
			continue
		}
		item := guidance[file.Path]
		if item.Status == "MISSING" {
			if parentConflict := adoptParentConflict(file.Path, inspection); parentConflict != "" {
				conflicts = append(conflicts, protocol.Conflict{Path: file.Path, Reason: parentConflict, Options: []string{"SKIP", "ALTERNATE_PATH"}})
				continue
			}
			candidates = append(candidates, file)
			continue
		}
		conflicts = append(conflicts, conflictForGuidance(item))
	}
	manifestStatus := guidance[".exord/manifest.json"]
	if manifestStatus.Status == "MISSING" {
		if parentConflict := adoptParentConflict(".exord/manifest.json", inspection); parentConflict != "" {
			conflicts = append(conflicts, protocol.Conflict{Path: ".exord/manifest.json", Reason: parentConflict, Options: []string{"SKIP", "ALTERNATE_PATH"}})
		} else {
			manifest, err := renderManifest(intent, projectID, candidates, gitMode)
			if err != nil {
				return protocol.SetupPlan{}, err
			}
			candidates = append(candidates, manifest)
		}
	} else {
		conflicts = append(conflicts, conflictForGuidance(manifestStatus))
	}
	operations := operationsFor(candidates)
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Path < conflicts[j].Path })
	analysis := &protocol.AdoptAnalysis{Inventory: inspection.Inventory, Git: inspection.Git, Task: inspection.Task, Guidance: inspection.Guidance, Conflicts: conflicts}
	spec := protocol.PlanSpec{
		Mode: intent.Mode, Depth: intent.Depth, ProjectID: projectID,
		TargetIdentitySHA256: inspection.IdentitySHA256, TargetFingerprint: inspection.Fingerprint,
		Operations:    operations,
		Validation:    []protocol.Validation{{ID: "adopt-conflicts-classified-v1", Path: ""}, {ID: "adopt-git-boundary-v1", Path: ".git"}, {ID: "adopt-inventory-v1", Path: ""}},
		RiskSummary:   protocol.RiskSummary{SecretWarnings: inspection.Inventory.SecretCandidates},
		AdoptAnalysis: analysis,
	}
	canonical, err := canonicaljson.Marshal(spec)
	if err != nil {
		return protocol.SetupPlan{}, err
	}
	sum := sha256.Sum256(canonical)
	return protocol.SetupPlan{SchemaVersion: 1, PlanID: planID, TargetFingerprint: inspection.Fingerprint, Spec: spec, SpecSHA256: hex.EncodeToString(sum[:])}, nil
}

func operationsFor(files []rendered) []protocol.Operation {
	operations := make([]protocol.Operation, 0, len(files))
	for _, file := range files {
		sum := sha256.Sum256([]byte(normalizeLF(file.Content)))
		operations = append(operations, protocol.Operation{Kind: "CREATE", Path: file.Path, TemplateID: file.TemplateID, ExpectedSHA256: hex.EncodeToString(sum[:])})
	}
	sort.Slice(operations, func(i, j int) bool { return operations[i].Path < operations[j].Path })
	return operations
}

func adoptParentConflict(path string, inspection target.AdoptInspection) string {
	parent := filepath.Dir(filepath.FromSlash(path))
	for parent != "." {
		_, kind, _, exists := inspection.Entry(filepath.ToSlash(parent))
		if exists && kind != "DIRECTORY" {
			return "PARENT_NOT_DIRECTORY"
		}
		parent = filepath.Dir(parent)
	}
	return ""
}

func conflictForGuidance(item protocol.GuidanceAnalysis) protocol.Conflict {
	options := []string{"DIFF", "SKIP"}
	switch item.Status {
	case "LINKED_PATH", "TYPE_CONFLICT":
		options = []string{"SKIP", "ALTERNATE_PATH"}
	case "USER_OWNED":
		options = []string{"DIFF", "SKIP", "ALTERNATE_PATH"}
	case "MANAGED_UNMODIFIED":
		options = []string{"KEEP", "REVIEW_UPDATE"}
	}
	return protocol.Conflict{Path: item.Path, Reason: item.Status, Options: options}
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

func DecodeIntent(data []byte) (protocol.SetupIntent, error) {
	var intent protocol.SetupIntent
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&intent); err != nil {
		return intent, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return intent, errors.New("intent contains trailing JSON values")
	}
	return intent, validateIntent(intent)
}

func validateIntent(intent protocol.SetupIntent) error {
	if intent.SchemaVersion != 1 {
		return errors.New("unsupported intent schema_version")
	}
	if intent.Mode != "CREATE" && intent.Mode != "ADOPT" && intent.Mode != "REINITIALIZE" {
		return errors.New("invalid mode")
	}
	if intent.Depth != "QUICK" && intent.Depth != "CUSTOM" {
		return errors.New("invalid depth")
	}
	if strings.TrimSpace(intent.ProjectSummary) == "" || utf8.RuneCountInString(intent.ProjectSummary) > 2000 {
		return errors.New("project_summary must be 1..2000 characters")
	}
	if !utf8.ValidString(intent.ProjectSummary) {
		return errors.New("project_summary must be valid UTF-8")
	}
	if strings.ContainsRune(intent.ProjectSummary, '\x00') {
		return errors.New("project_summary contains NUL")
	}
	if intent.DocumentationLanguage != "en" && intent.DocumentationLanguage != "ko" {
		return errors.New("unsupported documentation_language")
	}
	if len(intent.SupportedAgents) == 0 {
		return errors.New("supported_agents must not be empty")
	}
	seen := map[string]bool{}
	for _, agent := range intent.SupportedAgents {
		if agent != "codex" && agent != "claude" && agent != "gemini" {
			return fmt.Errorf("unsupported agent: %s", agent)
		}
		if seen[agent] {
			return fmt.Errorf("duplicate agent: %s", agent)
		}
		seen[agent] = true
	}
	return nil
}

type rendered struct{ Path, TemplateID, Content string }

func renderedFiles(intent protocol.SetupIntent, projectID, gitMode string) ([]rendered, error) {
	agents := append([]string(nil), intent.SupportedAgents...)
	sort.Strings(agents)
	data := exordinit.TemplateData{ProjectSummary: intent.ProjectSummary}
	agentRules, err := exordinit.RenderTemplate(intent.DocumentationLanguage, "AGENTS.md", data)
	if err != nil {
		return nil, err
	}
	project, err := exordinit.RenderTemplate(intent.DocumentationLanguage, "PROJECT.md", data)
	if err != nil {
		return nil, err
	}
	files := []rendered{
		{Path: "AGENTS.md", TemplateID: "agents-core-" + intent.DocumentationLanguage, Content: agentRules},
		{Path: "docs/PROJECT.md", TemplateID: "project-core-" + intent.DocumentationLanguage, Content: project},
	}
	for _, bridge := range []struct{ Agent, Path, Name, ID string }{
		{Agent: "claude", Path: "CLAUDE.md", Name: "CLAUDE.md", ID: "claude-bridge-v1"},
		{Agent: "gemini", Path: "GEMINI.md", Name: "GEMINI.md", ID: "gemini-bridge-v1"},
	} {
		if !contains(agents, bridge.Agent) {
			continue
		}
		content, renderErr := exordinit.RenderTemplate(intent.DocumentationLanguage, bridge.Name, data)
		if renderErr != nil {
			return nil, renderErr
		}
		files = append(files, rendered{Path: bridge.Path, TemplateID: bridge.ID, Content: content})
	}
	manifest, err := renderManifest(intent, projectID, files, gitMode)
	if err != nil {
		return nil, err
	}
	return append(files, manifest), nil
}

func renderManifest(intent protocol.SetupIntent, projectID string, files []rendered, gitMode string) (rendered, error) {
	agents := append([]string(nil), intent.SupportedAgents...)
	sort.Strings(agents)
	managed := make([]map[string]any, 0, len(files))
	for _, file := range files {
		sum := sha256.Sum256([]byte(normalizeLF(file.Content)))
		policy := "PROPOSE_ONLY"
		if strings.Contains(file.TemplateID, "bridge") {
			policy = "REPLACE_IF_UNMODIFIED"
		}
		managed = append(managed, map[string]any{
			"path": file.Path, "template_id": file.TemplateID, "template_version": 1,
			"baseline_sha256": hex.EncodeToString(sum[:]), "hash_mode": "TEXT_UTF8_LF_V1", "update_policy": policy,
		})
	}
	sort.Slice(managed, func(i, j int) bool { return managed[i]["path"].(string) < managed[j]["path"].(string) })
	manifest, err := json.MarshalIndent(map[string]any{
		"schema_version": 1,
		"generator":      map[string]any{"name": "exord-init", "version": "0.4.0-dev"},
		"project_id":     projectID,
		"settings": map[string]any{
			"git_mode": gitMode, "branch_profile": "SIMPLE", "task_path": "TASK.md",
			"languages":        map[string]any{"documentation": intent.DocumentationLanguage, "commit_messages": intent.DocumentationLanguage, "code_comments": "en"},
			"supported_agents": agents,
		},
		"managed_files": managed,
	}, "", "  ")
	if err != nil {
		return rendered{}, err
	}
	return rendered{Path: ".exord/manifest.json", TemplateID: "manifest-v1", Content: string(manifest) + "\n"}, nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func normalizeLF(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
}
