package protocol

// TaskIntent is review input, not permission to write files or run commands.
type TaskIntent struct {
	SchemaVersion      int      `json:"schema_version"`
	Title              string   `json:"title"`
	Type               string   `json:"type"`
	Areas              []string `json:"areas"`
	ScopePaths         []string `json:"scope_paths"`
	ExternalRef        *string  `json:"external_ref"`
	Status             string   `json:"status"`
	Owner              string   `json:"owner"`
	OwnerConfirmed     bool     `json:"owner_confirmed"`
	Contributors       []string `json:"contributors"`
	BaseBranch         string   `json:"base_branch"`
	WorkBranch         string   `json:"work_branch"`
	Language           string   `json:"language"`
	Goal               string   `json:"goal"`
	Scope              []string `json:"scope"`
	NonGoals           []string `json:"non_goals"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	Plan               []string `json:"plan"`
	Validation         []string `json:"validation"`
}

type TaskReviewSpec struct {
	Mode                 string      `json:"mode"`
	ProjectID            string      `json:"project_id"`
	TargetIdentitySHA256 string      `json:"target_identity_sha256"`
	TargetFingerprint    string      `json:"target_fingerprint"`
	TaskPath             string      `json:"task_path"`
	Intent               TaskIntent  `json:"intent"`
	Git                  GitAnalysis `json:"git"`
	Disposition          string      `json:"disposition"`
	Blockers             []string    `json:"blockers"`
	ReadinessGaps        []string    `json:"readiness_gaps"`
	ValidationResult     string      `json:"validation_result"`
}

type TaskReviewPlan struct {
	SchemaVersion int            `json:"schema_version"`
	PlanID        string         `json:"plan_id"`
	Spec          TaskReviewSpec `json:"spec"`
	SpecSHA256    string         `json:"spec_sha256"`
}

// TaskDraft identifies one local task draft independently of plan revisions.
type TaskDraft struct {
	SchemaVersion        int    `json:"schema_version"`
	DraftID              string `json:"draft_id"`
	TaskID               string `json:"task_id"`
	CreatedAt            string `json:"created_at"`
	ProjectID            string `json:"project_id"`
	TargetIdentitySHA256 string `json:"target_identity_sha256"`
	WorkBranch           string `json:"work_branch"`
}

type TaskCollisionScan struct {
	Branches       int      `json:"branches"`
	Worktrees      int      `json:"worktrees"`
	HistoryCommits int      `json:"history_commits"`
	Limits         []string `json:"limits"`
}

type TaskPreparationSpec struct {
	Mode             string            `json:"mode"`
	GeneratorVersion string            `json:"generator_version"`
	Draft            TaskDraft         `json:"draft"`
	Review           TaskReviewSpec    `json:"review"`
	CollisionScan    TaskCollisionScan `json:"collision_scan"`
	Operations       []Operation       `json:"operations"`
}

// TaskPreparation is a local snapshot, never an applicable CREATE plan.
type TaskPreparation struct {
	SchemaVersion int                 `json:"schema_version"`
	PlanID        string              `json:"plan_id"`
	Spec          TaskPreparationSpec `json:"spec"`
	SpecSHA256    string              `json:"spec_sha256"`
	Document      string              `json:"document"`
}
