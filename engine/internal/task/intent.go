package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/kimNarr/Exord-init/engine/internal/protocol"
)

func DecodeIntent(data []byte) (protocol.TaskIntent, error) {
	var intent protocol.TaskIntent
	if len(data) > MaxDocumentBytes || !utf8.Valid(data) {
		return intent, errors.New("invalid task intent size or encoding")
	}
	// Reject duplicate keys before typed decoding; JSON's normal decoder uses
	// the last duplicate, which would make review input ambiguous.
	decoder := json.NewDecoder(bytes.NewReader(data))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return intent, errors.New("intent must be an object")
	}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return intent, err
		}
		name, ok := key.(string)
		if !ok || seen[name] {
			return intent, errors.New("duplicate intent key")
		}
		seen[name] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return intent, err
		}
		if name != "external_ref" && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return intent, errors.New("null intent field")
		}
	}
	if _, err := decoder.Token(); err != nil {
		return intent, err
	}
	required := []string{"schema_version", "title", "type", "areas", "scope_paths", "external_ref", "status", "owner", "owner_confirmed", "contributors", "base_branch", "work_branch", "language", "goal", "scope", "non_goals", "acceptance_criteria", "plan", "validation"}
	if len(seen) != len(required) {
		return intent, errors.New("unknown or missing intent field")
	}
	for _, key := range required {
		if !seen[key] {
			return intent, errors.New("missing intent field")
		}
	}
	strict := json.NewDecoder(bytes.NewReader(data))
	strict.DisallowUnknownFields()
	if err := strict.Decode(&intent); err != nil {
		return intent, err
	}
	var extra any
	if strict.Decode(&extra) != io.EOF {
		return intent, errors.New("trailing JSON")
	}
	return intent, ValidateIntent(intent)
}

func ValidateIntent(i protocol.TaskIntent) error {
	if i.SchemaVersion != 1 || (i.Status != "DRAFT" && i.Status != "READY") || (i.Language != "en" && i.Language != "ko") {
		return errors.New("unsupported task intent version, status or language")
	}
	m := Metadata{Title: i.Title, Type: i.Type, Owner: i.Owner, Status: "DRAFT", Areas: i.Areas, ScopePaths: i.ScopePaths, Contributors: i.Contributors, ExternalRef: i.ExternalRef, BaseBranch: i.BaseBranch, WorkBranch: i.WorkBranch}
	if err := validateMetadata(m); err != nil {
		return err
	}
	if i.Goal != "" && !validText(i.Goal, 2000) {
		return errors.New("invalid goal")
	}
	for _, list := range [][]string{i.Scope, i.NonGoals, i.AcceptanceCriteria, i.Plan, i.Validation} {
		if err := validateList(list, 64, 2000); err != nil {
			return err
		}
	}
	return nil
}

// ReadinessGaps assess supplied evidence; confirmation remains a human input,
// not proof of identity or permission to execute the listed commands.
func ReadinessGaps(i protocol.TaskIntent) []string {
	gaps := []string{}
	if i.Owner == "UNASSIGNED" || !i.OwnerConfirmed {
		gaps = append(gaps, "HUMAN_OWNER_CONFIRMATION_REQUIRED")
	}
	if i.Goal == "" {
		gaps = append(gaps, "GOAL_REQUIRED")
	}
	if len(i.Scope) == 0 {
		gaps = append(gaps, "SCOPE_REQUIRED")
	}
	if len(i.AcceptanceCriteria) == 0 {
		gaps = append(gaps, "ACCEPTANCE_CRITERIA_REQUIRED")
	}
	if len(i.Validation) == 0 {
		gaps = append(gaps, "VERIFICATION_METHOD_REQUIRED")
	}
	return gaps
}
