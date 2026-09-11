package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/kimNarr/Exord-init/engine/internal/canonicaljson"
	"github.com/kimNarr/Exord-init/engine/internal/id"
	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	"github.com/kimNarr/Exord-init/engine/internal/target"
)

const currentGeneratorVersion = "0.4.0-dev"

// currentTemplateVersions is the generator's view of every managed template's
// version. An upgrade plan compares these to the versions recorded in the
// manifest; a template_id missing here is one this generator no longer emits.
var currentTemplateVersions = map[string]int{
	"agents-core-en":   1,
	"agents-core-ko":   1,
	"project-core-en":  1,
	"project-core-ko":  1,
	"claude-bridge-v1": 1,
	"gemini-bridge-v1": 1,
}

// BuildUpgrade produces a read-only UPGRADE plan: a per-managed-file assessment
// of what a later upgrade apply would do, plus a canonical hash. It never
// stages files or requests an approval.
func BuildUpgrade(inspection target.UpgradeInspection) (protocol.SetupPlan, error) {
	planID, err := id.UUID4()
	if err != nil {
		return protocol.SetupPlan{}, err
	}
	manifest := inspection.Manifest
	if !validUUID(manifest.ProjectID) {
		return protocol.SetupPlan{}, errors.New("manifest project ID is invalid")
	}

	recorded := map[string]target.ManifestManagedFile{}
	for _, file := range manifest.ManagedFiles {
		recorded[file.Path] = file
	}
	expected := map[string]bool{}
	for _, path := range expectedUpgradePaths(manifest.Settings.SupportedAgents, manifest.Settings.Languages.Documentation) {
		expected[path] = true
	}

	paths := map[string]bool{}
	for path := range recorded {
		paths[path] = true
	}
	for path := range expected {
		paths[path] = true
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)

	files := make([]protocol.UpgradeFileAnalysis, 0, len(ordered))
	disposition := "UP_TO_DATE"
	promote := func(next string) {
		disposition = worseDisposition(disposition, next)
	}
	for _, path := range ordered {
		record, isRecorded := recorded[path]
		state := inspection.Current[path]
		analysis := protocol.UpgradeFileAnalysis{Path: path, LocalState: state.Kind}
		if isRecorded {
			analysis.TemplateID = record.TemplateID
			analysis.UpdatePolicy = record.UpdatePolicy
			analysis.RecordedTemplateVersion = record.TemplateVersion
		}
		analysis.CurrentTemplateVersion = currentTemplateVersions[analysis.TemplateID]

		switch {
		case isRecorded && record.HashMode != "TEXT_UTF8_LF_V1":
			analysis.LocalState = "UNKNOWN_HASH_MODE"
			analysis.UpgradeAction = "BLOCKED"
			promote("BLOCKED")
		case state.Kind == "SYMLINK", state.Kind == "TYPE_CONFLICT", state.Kind == "UNREADABLE":
			analysis.UpgradeAction = "BLOCKED"
			promote("BLOCKED")
		case !isRecorded:
			// Expected by the current generator but not tracked yet.
			analysis.LocalState = "UNTRACKED_BY_MANIFEST"
			if state.Kind == "MISSING" {
				analysis.UpgradeAction = "NEW_MANAGED_FILE"
			} else {
				analysis.UpgradeAction = "ADOPT_EXISTING_FILE"
			}
			promote("REVIEW_REQUIRED")
		case !expected[path]:
			analysis.UpgradeAction = "ORPHAN_MANAGED_FILE"
			promote("REVIEW_REQUIRED")
		case analysis.CurrentTemplateVersion == 0:
			// Recorded template_id the generator no longer emits.
			analysis.UpgradeAction = "ORPHAN_MANAGED_FILE"
			promote("REVIEW_REQUIRED")
		case state.Kind == "MISSING":
			analysis.UpgradeAction = "RECREATE_MISSING"
			promote("UPGRADE_AVAILABLE")
		case state.NormalizedSHA256 != record.BaselineSHA256:
			analysis.UpgradeAction = "REVIEW_LOCAL_EDIT"
			promote("REVIEW_REQUIRED")
		case analysis.CurrentTemplateVersion > analysis.RecordedTemplateVersion:
			if record.UpdatePolicy == "REPLACE_IF_UNMODIFIED" {
				analysis.UpgradeAction = "TEMPLATE_UPDATE_AVAILABLE"
				promote("UPGRADE_AVAILABLE")
			} else {
				analysis.UpgradeAction = "PROPOSE_ONLY_UPDATE"
				promote("REVIEW_REQUIRED")
			}
		default:
			analysis.UpgradeAction = "UP_TO_DATE"
		}
		files = append(files, analysis)
	}

	direction := compareGeneratorVersions(manifest.Generator.Version, currentGeneratorVersion)
	if direction == "DOWNGRADE" {
		promote("BLOCKED")
	}

	gitMode := "DOCUMENT_ONLY"
	if inspection.GitDetected {
		gitMode = "ENABLED"
	}
	gitConsistent := manifest.Settings.GitMode == "" || manifest.Settings.GitMode == gitMode ||
		(manifest.Settings.GitMode == "NONE" && gitMode == "DOCUMENT_ONLY")
	if !gitConsistent {
		promote("REVIEW_REQUIRED")
	}

	agents := append([]string(nil), manifest.Settings.SupportedAgents...)
	sort.Strings(agents)
	upgrade := &protocol.UpgradeAnalysis{
		ManifestSchemaVersion: manifest.SchemaVersion,
		Generator:             protocol.UpgradeGenerator{Recorded: manifest.Generator.Version, Current: currentGeneratorVersion, Direction: direction},
		Settings: protocol.UpgradeSettings{
			GitMode:               manifest.Settings.GitMode,
			BranchProfile:         manifest.Settings.BranchProfile,
			DocumentationLanguage: manifest.Settings.Languages.Documentation,
			SupportedAgents:       agents,
		},
		GitConsistent: gitConsistent,
		Files:         files,
		Disposition:   disposition,
	}

	spec := protocol.PlanSpec{
		Mode: "UPGRADE", Depth: "QUICK", ProjectID: manifest.ProjectID,
		TargetIdentitySHA256: inspection.IdentitySHA256, TargetFingerprint: inspection.Fingerprint,
		Operations: []protocol.Operation{}, Validation: []protocol.Validation{},
		RiskSummary:     protocol.RiskSummary{},
		UpgradeAnalysis: upgrade,
	}
	canonical, err := canonicaljson.Marshal(spec)
	if err != nil {
		return protocol.SetupPlan{}, err
	}
	sum := sha256.Sum256(canonical)
	return protocol.SetupPlan{
		SchemaVersion: 1, PlanID: planID, TargetFingerprint: inspection.Fingerprint,
		Spec: spec, SpecSHA256: hex.EncodeToString(sum[:]),
	}, nil
}

func expectedUpgradePaths(agents []string, language string) []string {
	if language != "en" && language != "ko" {
		language = "en"
	}
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

// worseDisposition keeps the most serious disposition seen so far.
func worseDisposition(current, next string) string {
	rank := map[string]int{"UP_TO_DATE": 0, "UPGRADE_AVAILABLE": 1, "REVIEW_REQUIRED": 2, "BLOCKED": 3}
	if rank[next] > rank[current] {
		return next
	}
	return current
}

// compareGeneratorVersions compares dotted numeric prefixes, ignoring any
// pre-release or build suffix. An unparseable recorded version is UNKNOWN.
func compareGeneratorVersions(recorded, current string) string {
	recordedParts, ok := numericVersion(recorded)
	if !ok {
		return "UNKNOWN"
	}
	currentParts, _ := numericVersion(current)
	for i := 0; i < 3; i++ {
		r, c := recordedParts[i], currentParts[i]
		if r < c {
			return "UPGRADE"
		}
		if r > c {
			return "DOWNGRADE"
		}
	}
	return "SAME"
}

func numericVersion(value string) ([3]int, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	for _, sep := range []string{"-", "+"} {
		if index := strings.Index(value, sep); index >= 0 {
			value = value[:index]
		}
	}
	fields := strings.Split(value, ".")
	if len(fields) == 0 || fields[0] == "" {
		return [3]int{}, false
	}
	var parts [3]int
	for i := 0; i < 3 && i < len(fields); i++ {
		n, err := strconv.Atoi(fields[i])
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		parts[i] = n
	}
	return parts, true
}
