package task

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const validDocument = `---
schema_version: 1
task_id: T-20261016-7K3M2Q9R
title: 첫 작업
type: feature
areas: [frontend]
scope_paths: [src/]
external_ref: null
status: DRAFT
owner: UNASSIGNED
contributors: []
base_branch: main
work_branch: feat/first-task
updated_at: 2026-10-16
---
## 목표
첫 구현 준비
`

func TestParseDocumentRecognizesBothLanguagesAndCRLF(t *testing.T) {
	for _, text := range []string{validDocument, strings.ReplaceAll(validDocument, "## 목표", "## Goal"), strings.ReplaceAll(validDocument, "\n", "\r\n")} {
		meta, err := ParseDocument([]byte(text))
		if err != nil || meta.TaskID != "T-20261016-7K3M2Q9R" {
			t.Fatalf("metadata: %#v %v", meta, err)
		}
	}
}

func TestParseDocumentRejectsAmbiguousOrForeignMetadata(t *testing.T) {
	for name, text := range map[string]string{
		"body lookalike": "notes\ntask_id: T-20261016-7K3M2Q9R\nstatus: READY\n## Goal",
		"duplicate":      strings.Replace(validDocument, "status: DRAFT", "status: DRAFT\nstatus: READY", 1),
		"unknown":        strings.Replace(validDocument, "schema_version: 1", "schema_version: 2", 1),
		"alias":          strings.Replace(validDocument, "areas: [frontend]", "areas: &areas [frontend]\nextra: *areas", 1),
		"invalid date":   strings.Replace(validDocument, "20261016", "20260230", 1),
		"unknown field":  strings.Replace(validDocument, "title: 첫 작업", "title: 첫 작업\nunexpected: true", 1),
		"coerced title":  strings.Replace(validDocument, "title: 첫 작업", "title: 123", 1),
		"owner":          strings.Replace(validDocument, "status: DRAFT", "status: READY", 1),
		"path":           strings.Replace(validDocument, "scope_paths: [src/]", "scope_paths: [../outside]", 1),
		"id":             strings.Replace(validDocument, "7K3M2Q9R", "IIIIIIII", 1),
		"oversize":       validDocument + strings.Repeat("x", MaxDocumentBytes),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseDocument([]byte(text)); err == nil {
				t.Fatal("unsafe metadata accepted")
			}
		})
	}
}

func TestDecodeIntentValidatesFixtureAndReadiness(t *testing.T) {
	raw, err := os.ReadFile("../../../tests/fixtures/intent-task-review-ko.json")
	if err != nil {
		t.Fatal(err)
	}
	intent, err := DecodeIntent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(ReadinessGaps(intent)) != 3 {
		t.Fatalf("unexpected gaps: %v", ReadinessGaps(intent))
	}
	for _, change := range []string{
		strings.Replace(string(raw), `"schema_version": 1`, `"schema_version": 1, "schema_version": 2`, 1),
		strings.Replace(string(raw), `"owner_confirmed": false`, `"owner_confirmed": null`, 1),
		string(raw) + ` {}`,
		strings.Replace(string(raw), `"scope_paths": ["docs/"]`, `"scope_paths": ["C:/outside"]`, 1),
		strings.Replace(string(raw), `"status": "DRAFT"`, `"status": "COMPLETE"`, 1),
		strings.Replace(string(raw), `"schema_version": 1`, `"schema_version": 1, "operations": []`, 1),
		strings.Replace(string(raw), `"schema_version": 1`, `"schema_version": 1, "OWNER": "unexpected"`, 1),
	} {
		if _, err := DecodeIntent([]byte(change)); err == nil {
			t.Fatal("invalid intent accepted")
		}
	}
	intent.Owner = "@example"
	intent.OwnerConfirmed = true
	intent.AcceptanceCriteria = []string{"a reviewed outcome"}
	intent.Validation = []string{"review the outcome with the owner"}
	if gaps := ReadinessGaps(intent); len(gaps) != 0 {
		t.Fatal(gaps)
	}
	encoded, _ := json.Marshal(intent)
	if _, err := DecodeIntent(encoded); err != nil {
		t.Fatal(err)
	}
}

func TestBranchAndScopePaths(t *testing.T) {
	for _, s := range []string{"-option", "a..b", "refs/.hidden", "HEAD", "a@{b", "a.lock", "a/", "a b"} {
		if ValidBranch(s) {
			t.Errorf("unsafe branch %q", s)
		}
	}
	for _, s := range []string{"../a", "/a", `a\b`, "C:/a", ".", "a/../b", "a//b"} {
		if ValidScopePath(s) {
			t.Errorf("unsafe scope %q", s)
		}
	}
}
