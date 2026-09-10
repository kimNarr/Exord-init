package apply

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kimNarr/Exord-init/engine/internal/protocol"
)

// Executable CREATE validations. Each entry declared in the plan spec is run
// against the freshly published bytes after per-file hash verification and
// before the run is finalized. An unknown ID fails closed so a plan can never
// declare a check the engine does not actually enforce.
const (
	maxAgentsFileBytes = 16 * 1024
	maxAgentsFileLines = 200
)

// projectRequiredSections lists the section headings docs/PROJECT.md must keep
// for every supported documentation language. The file passes when it still
// contains, on its own line, every heading of at least one language set; a
// project summary that injects extra markdown cannot remove a real heading.
var projectRequiredSections = map[string][]string{
	"en": {
		"# Project",
		"## Users and problem",
		"## MVP and non-goals",
		"## Success criteria",
		"## Boundaries and constraints",
	},
	"ko": {
		"# 프로젝트",
		"## 사용자와 문제",
		"## MVP와 비목표",
		"## 성공 기준",
		"## 경계와 제약",
	},
}

// validateCreatedFiles is the seam execute() calls after hash verification.
// Tests may replace it to exercise the finalize-time rollback path; production
// always uses runValidations.
var validateCreatedFiles = runValidations

func runValidations(targetRoot *os.Root, plan protocol.SetupPlan, created []createdFile) error {
	createdPaths := make(map[string]bool, len(created))
	for _, file := range created {
		createdPaths[filepath.ToSlash(file.Path)] = true
	}
	for _, validation := range plan.Spec.Validation {
		content, err := readValidationTarget(targetRoot, validation.Path, createdPaths)
		if err != nil {
			return fmt.Errorf("validation %s: %w", validation.ID, err)
		}
		var checkErr error
		switch validation.ID {
		case "agents-size-v1":
			checkErr = checkAgentsSize(content)
		case "manifest-schema-v1":
			checkErr = checkManifestSchema(content, plan.Spec.ProjectID)
		case "project-required-sections-v1":
			checkErr = checkProjectRequiredSections(content)
		default:
			return fmt.Errorf("validation %s: unknown validation ID", validation.ID)
		}
		if checkErr != nil {
			return fmt.Errorf("validation %s failed: %w", validation.ID, checkErr)
		}
	}
	return nil
}

func readValidationTarget(targetRoot *os.Root, relative string, createdPaths map[string]bool) ([]byte, error) {
	if relative == "" {
		return nil, errors.New("validation target path is empty")
	}
	if !createdPaths[filepath.ToSlash(relative)] {
		return nil, errors.New("validation target was not created by this run")
	}
	return readRootRegular(targetRoot, relative, maxStagedFileBytes)
}

func checkAgentsSize(content []byte) error {
	if len(content) > maxAgentsFileBytes {
		return fmt.Errorf("AGENTS.md is %d bytes, limit is %d", len(content), maxAgentsFileBytes)
	}
	if lines := strings.Count(string(content), "\n") + 1; lines > maxAgentsFileLines {
		return fmt.Errorf("AGENTS.md has %d lines, limit is %d", lines, maxAgentsFileLines)
	}
	return nil
}

func checkManifestSchema(content []byte, expectedProjectID string) error {
	var manifest map[string]json.RawMessage
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	if err := decoder.Decode(&manifest); err != nil {
		return errors.New("manifest is not a JSON object")
	}
	var schemaVersion int
	if err := json.Unmarshal(manifest["schema_version"], &schemaVersion); err != nil || schemaVersion != 1 {
		return errors.New("manifest schema_version must be 1")
	}
	for _, key := range []string{"generator", "project_id", "settings", "managed_files"} {
		if _, ok := manifest[key]; !ok {
			return fmt.Errorf("manifest is missing required key %q", key)
		}
	}
	var objectProbe map[string]json.RawMessage
	if err := json.Unmarshal(manifest["generator"], &objectProbe); err != nil {
		return errors.New("manifest generator must be an object")
	}
	if err := json.Unmarshal(manifest["settings"], &objectProbe); err != nil {
		return errors.New("manifest settings must be an object")
	}
	var managed []json.RawMessage
	if err := json.Unmarshal(manifest["managed_files"], &managed); err != nil {
		return errors.New("manifest managed_files must be an array")
	}
	var projectID string
	if err := json.Unmarshal(manifest["project_id"], &projectID); err != nil {
		return errors.New("manifest project_id must be a string")
	}
	if projectID != expectedProjectID {
		return errors.New("manifest project_id does not match the planned project ID")
	}
	return nil
}

func checkProjectRequiredSections(content []byte) error {
	lines := make(map[string]bool)
	for _, line := range strings.Split(string(content), "\n") {
		lines[strings.TrimRight(line, " \t\r")] = true
	}
	var missingByLanguage []string
	for language, headings := range projectRequiredSections {
		missing := make([]string, 0)
		for _, heading := range headings {
			if !lines[heading] {
				missing = append(missing, heading)
			}
		}
		if len(missing) == 0 {
			return nil
		}
		missingByLanguage = append(missingByLanguage, fmt.Sprintf("%s: %s", language, strings.Join(missing, ", ")))
	}
	return fmt.Errorf("docs/PROJECT.md is missing required sections (%s)", strings.Join(missingByLanguage, "; "))
}
