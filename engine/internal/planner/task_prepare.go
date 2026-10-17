package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	exordinit "github.com/kimNarr/Exord-init"
	"github.com/kimNarr/Exord-init/engine/internal/buildinfo"
	"github.com/kimNarr/Exord-init/engine/internal/canonicaljson"
	"github.com/kimNarr/Exord-init/engine/internal/id"
	"github.com/kimNarr/Exord-init/engine/internal/protocol"
	"github.com/kimNarr/Exord-init/engine/internal/target"
	taskcontract "github.com/kimNarr/Exord-init/engine/internal/task"
	"go.yaml.in/yaml/v3"
)

func PrepareTask(intent protocol.TaskIntent, inspection target.TaskInspection, draft protocol.TaskDraft, scan protocol.TaskCollisionScan) (protocol.TaskPreparation, error) {
	review, err := BuildTaskReview(intent, inspection)
	if err != nil {
		return protocol.TaskPreparation{}, err
	}
	if review.Spec.Disposition == "BLOCKED" {
		return protocol.TaskPreparation{}, errors.New("task preparation is blocked")
	}
	created, err := time.Parse(time.RFC3339, draft.CreatedAt)
	if err != nil || draft.SchemaVersion != 1 || !taskcontract.ValidID(draft.TaskID) || !validUUID(draft.DraftID) || draft.ProjectID != inspection.ProjectID || draft.TargetIdentitySHA256 != inspection.IdentitySHA256 {
		return protocol.TaskPreparation{}, errors.New("draft binding mismatch")
	}
	meta := taskcontract.Metadata{SchemaVersion: 1, TaskID: draft.TaskID, Title: intent.Title, Type: intent.Type, Areas: intent.Areas, ScopePaths: intent.ScopePaths, ExternalRef: intent.ExternalRef, Status: intent.Status, Owner: intent.Owner, Contributors: intent.Contributors, BaseBranch: intent.BaseBranch, WorkBranch: intent.WorkBranch, UpdatedAt: created.UTC().Format("2006-01-02")}
	frontmatter, err := yaml.Marshal(meta)
	if err != nil {
		return protocol.TaskPreparation{}, err
	}
	document, err := exordinit.RenderTaskTemplate(intent.Language, exordinit.TaskTemplateData{Title: intent.Title, Metadata: string(frontmatter), Goal: intent.Goal, Scope: intent.Scope, NonGoals: intent.NonGoals, AcceptanceCriteria: intent.AcceptanceCriteria, Plan: intent.Plan, Validation: intent.Validation})
	if err != nil {
		return protocol.TaskPreparation{}, err
	}
	if _, err := taskcontract.ParseDocument([]byte(document)); err != nil {
		return protocol.TaskPreparation{}, err
	}
	sum := sha256.Sum256([]byte(document))
	spec := protocol.TaskPreparationSpec{Mode: "TASK_PREPARE", GeneratorVersion: buildinfo.Version, Draft: draft, Review: review.Spec, CollisionScan: scan, Operations: []protocol.Operation{{Kind: "CREATE", Path: inspection.TaskPath, TemplateID: "task-core-" + intent.Language + "-v1", ExpectedSHA256: hex.EncodeToString(sum[:])}}}
	canonical, err := canonicaljson.Marshal(spec)
	if err != nil {
		return protocol.TaskPreparation{}, err
	}
	hash := sha256.Sum256(canonical)
	planID, err := id.UUID4()
	if err != nil {
		return protocol.TaskPreparation{}, err
	}
	return protocol.TaskPreparation{SchemaVersion: 1, PlanID: planID, Spec: spec, SpecSHA256: hex.EncodeToString(hash[:]), Document: document}, nil
}

func ValidateTaskPreparation(value protocol.TaskPreparation) error {
	if value.SchemaVersion != 1 || value.Spec.Mode != "TASK_PREPARE" || !validUUID(value.PlanID) || len(value.Spec.Operations) != 1 {
		return errors.New("invalid task preparation")
	}
	canonical, err := canonicaljson.Marshal(value.Spec)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(canonical)
	if value.SpecSHA256 != hex.EncodeToString(sum[:]) {
		return errors.New("task preparation hash mismatch")
	}
	op := value.Spec.Operations[0]
	docHash := sha256.Sum256([]byte(value.Document))
	if op.Kind != "CREATE" || (op.Path != "TASK.md" && op.Path != ".exord/TASK.md") || op.Path != value.Spec.Review.TaskPath || op.ExpectedSHA256 != hex.EncodeToString(docHash[:]) || strings.Contains(value.Document, "\r") {
		return errors.New("task document hash or operation mismatch")
	}
	meta, err := taskcontract.ParseDocument([]byte(value.Document))
	if err != nil {
		return err
	}
	if meta.TaskID != value.Spec.Draft.TaskID || meta.WorkBranch != value.Spec.Review.Intent.WorkBranch {
		return errors.New("task document identity mismatch")
	}
	return nil
}
