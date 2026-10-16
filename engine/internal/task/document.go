// Package task validates bounded, language-independent task metadata and input.
package task

import (
	"bytes"
	"errors"
	"io"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

const MaxDocumentBytes = 64 << 10

type Metadata struct {
	SchemaVersion      int      `yaml:"schema_version"`
	TaskID             string   `yaml:"task_id"`
	Title              string   `yaml:"title"`
	Type               string   `yaml:"type"`
	Areas              []string `yaml:"areas"`
	ScopePaths         []string `yaml:"scope_paths"`
	ExternalRef        *string  `yaml:"external_ref"`
	Status             string   `yaml:"status"`
	Owner              string   `yaml:"owner"`
	Contributors       []string `yaml:"contributors"`
	BaseBranch         string   `yaml:"base_branch"`
	WorkBranch         string   `yaml:"work_branch"`
	UpdatedAt          string   `yaml:"updated_at"`
	CancellationReason string   `yaml:"cancellation_reason,omitempty"`
	SupersededBy       *string  `yaml:"superseded_by,omitempty"`
}

var taskIDPattern = regexp.MustCompile(`^T-[0-9]{8}-[0-9A-HJKMNP-TV-Z]{8}$`)

// ParseDocument recognizes metadata, never file ownership or authorization.
// It ignores localized body headings and rejects YAML aliases, tags, duplicate
// keys and deep/large structures before decoding into typed metadata.
func ParseDocument(data []byte) (Metadata, error) {
	var meta Metadata
	if len(data) > MaxDocumentBytes || !utf8.Valid(data) {
		return meta, errors.New("invalid task document size or encoding")
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) < 3 || lines[0] != "---" {
		return meta, errors.New("task requires front matter")
	}
	end := 1
	for end < len(lines) && lines[end] != "---" {
		end++
	}
	if end == len(lines) {
		return meta, errors.New("unclosed front matter")
	}
	raw := []byte(strings.Join(lines[1:end], "\n"))
	var node yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&node); err != nil {
		return meta, errors.New("invalid task YAML")
	}
	if len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return meta, errors.New("metadata must be a mapping")
	}
	if err := checkNode(&node, 0); err != nil {
		return meta, err
	}
	fields := map[string]bool{}
	for i := 0; i < len(node.Content[0].Content); i += 2 {
		name := node.Content[0].Content[i].Value
		value := node.Content[0].Content[i+1]
		fields[name] = true
		switch name {
		case "schema_version":
			if value.Tag != "!!int" {
				return meta, errors.New("schema version must be an integer")
			}
		case "areas", "scope_paths", "contributors":
			if value.Kind != yaml.SequenceNode {
				return meta, errors.New("metadata list required")
			}
			for _, item := range value.Content {
				if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
					return meta, errors.New("metadata list item must be a string")
				}
			}
		case "external_ref", "superseded_by":
			if value.Tag != "!!null" && value.Tag != "!!str" {
				return meta, errors.New("reference must be a string or null")
			}
		case "updated_at":
			if value.Tag != "!!str" && value.Tag != "!!timestamp" {
				return meta, errors.New("date required")
			}
		default:
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return meta, errors.New("metadata string required")
			}
		}
	}
	for _, field := range []string{"schema_version", "task_id", "title", "type", "areas", "scope_paths", "external_ref", "status", "owner", "contributors", "base_branch", "work_branch", "updated_at"} {
		if !fields[field] {
			return meta, errors.New("missing task metadata field")
		}
	}
	strict := yaml.NewDecoder(bytes.NewReader(raw))
	strict.KnownFields(true)
	if err := strict.Decode(&meta); err != nil {
		return meta, errors.New("invalid metadata fields")
	}
	var extra any
	if err := strict.Decode(&extra); err != io.EOF {
		return meta, errors.New("multiple YAML documents")
	}
	if meta.SchemaVersion != 1 || !taskIDPattern.MatchString(meta.TaskID) {
		return meta, errors.New("invalid task version or ID")
	}
	if _, err := time.Parse("20060102", meta.TaskID[2:10]); err != nil {
		return meta, errors.New("invalid task date")
	}
	if _, err := time.Parse("2006-01-02", meta.UpdatedAt); err != nil {
		return meta, errors.New("invalid updated_at")
	}
	if !contains([]string{"DRAFT", "READY", "IN_PROGRESS", "BLOCKED", "VERIFYING", "COMPLETE", "CANCELLED"}, meta.Status) {
		return meta, errors.New("invalid task status")
	}
	if err := validateMetadata(meta); err != nil {
		return meta, err
	}
	if meta.CancellationReason != "" && !contains([]string{"NO_LONGER_NEEDED", "SUPERSEDED", "DUPLICATE", "INFEASIBLE", "OTHER"}, meta.CancellationReason) {
		return meta, errors.New("invalid cancellation reason")
	}
	return meta, nil
}

func checkNode(node *yaml.Node, depth int) error {
	if depth > 8 || len(node.Content) > 256 || node.Kind == yaml.AliasNode || node.Anchor != "" {
		return errors.New("unsupported YAML structure")
	}
	if node.Tag != "" && !contains([]string{"!!map", "!!seq", "!!str", "!!int", "!!null", "!!bool", "!!timestamp"}, node.Tag) {
		return errors.New("unsupported YAML tag")
	}
	seen := map[string]bool{}
	for i, child := range node.Content {
		if node.Kind == yaml.MappingNode && i%2 == 0 {
			if child.Kind != yaml.ScalarNode || child.Tag != "!!str" || seen[child.Value] {
				return errors.New("duplicate or non-string YAML key")
			}
			seen[child.Value] = true
		}
		if err := checkNode(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func validateMetadata(m Metadata) error {
	if !validText(m.Title, 200) || !validText(m.Owner, 100) || !contains([]string{"feature", "fix", "chore", "docs", "spike"}, m.Type) {
		return errors.New("invalid task title, owner or type")
	}
	if m.Status != "DRAFT" && m.Owner == "UNASSIGNED" {
		return errors.New("owner is required")
	}
	if !ValidBranch(m.BaseBranch) || !ValidBranch(m.WorkBranch) || m.BaseBranch == m.WorkBranch {
		return errors.New("invalid task branches")
	}
	if err := validateList(m.Areas, 32, 100); err != nil {
		return err
	}
	if err := validateList(m.Contributors, 32, 100); err != nil {
		return err
	}
	if err := validateList(m.ScopePaths, 64, 512); err != nil {
		return err
	}
	for _, p := range m.ScopePaths {
		if !ValidScopePath(p) {
			return errors.New("unsafe scope path")
		}
	}
	if m.ExternalRef != nil && !validText(*m.ExternalRef, 512) {
		return errors.New("invalid external reference")
	}
	return nil
}

func ValidScopePath(p string) bool {
	trimmed := strings.TrimSuffix(p, "/")
	return validText(p, 512) && !strings.ContainsAny(p, "\\:") && !strings.HasPrefix(p, "/") && trimmed != "." && path.Clean(trimmed) == trimmed && trimmed != ".." && !strings.HasPrefix(trimmed, "../")
}

func ValidBranch(s string) bool {
	if !validText(s, 200) || strings.ContainsAny(s, " ~^:?*[\\") || strings.Contains(s, "..") || strings.Contains(s, "@{") || strings.HasPrefix(s, "-") || s == "HEAD" || s == "@" {
		return false
	}
	for _, part := range strings.Split(s, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

func validText(s string, max int) bool {
	if !utf8.ValidString(s) || strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > max {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validateList(values []string, count, length int) error {
	if values == nil || len(values) > count {
		return errors.New("invalid task list")
	}
	seen := map[string]bool{}
	for _, s := range values {
		if !validText(s, length) || seen[s] {
			return errors.New("invalid or duplicate list item")
		}
		seen[s] = true
	}
	return nil
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
