package protocol

type SetupIntent struct {
	SchemaVersion         int      `json:"schema_version"`
	Mode                  string   `json:"mode"`
	Depth                 string   `json:"depth"`
	ProjectSummary        string   `json:"project_summary"`
	DocumentationLanguage string   `json:"documentation_language"`
	SupportedAgents       []string `json:"supported_agents"`
}

type Operation struct {
	Kind           string `json:"kind"`
	Path           string `json:"path"`
	TemplateID     string `json:"template_id"`
	ExpectedSHA256 string `json:"expected_sha256"`
}

type Validation struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

type RiskSummary struct {
	DestructiveOperations int `json:"destructive_operations"`
	UserOwnedFilesTouched int `json:"user_owned_files_touched"`
	OutsideRootPaths      int `json:"outside_root_paths"`
	SecretWarnings        int `json:"secret_warnings"`
}

type PlanSpec struct {
	Mode                 string       `json:"mode"`
	Depth                string       `json:"depth"`
	ProjectID            string       `json:"project_id"`
	TargetIdentitySHA256 string       `json:"target_identity_sha256"`
	TargetFingerprint    string       `json:"target_fingerprint"`
	Operations           []Operation  `json:"operations"`
	Validation           []Validation `json:"validation"`
	RiskSummary          RiskSummary  `json:"risk_summary"`
}

type SetupPlan struct {
	SchemaVersion     int      `json:"schema_version"`
	PlanID            string   `json:"plan_id"`
	TargetFingerprint string   `json:"target_fingerprint"`
	Spec              PlanSpec `json:"spec"`
	SpecSHA256        string   `json:"spec_sha256"`
}

type Approval struct {
	SchemaVersion        int    `json:"schema_version"`
	RunID                string `json:"run_id"`
	PlanID               string `json:"plan_id"`
	SpecSHA256           string `json:"spec_sha256"`
	TargetIdentitySHA256 string `json:"target_identity_sha256"`
	ApprovedAction       string `json:"approved_action"`
	ApprovedAt           string `json:"approved_at"`
}

type RunOperation struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

type RunJournal struct {
	SchemaVersion int            `json:"schema_version"`
	RunID         string         `json:"run_id"`
	PlanID        string         `json:"plan_id"`
	SpecSHA256    string         `json:"spec_sha256"`
	Status        string         `json:"status"`
	Stage         string         `json:"stage"`
	Operations    []RunOperation `json:"operations"`
	StartedAt     string         `json:"started_at"`
	UpdatedAt     string         `json:"updated_at"`
}

type Result struct {
	SchemaVersion int      `json:"schema_version"`
	Command       string   `json:"command"`
	Status        string   `json:"status"`
	Code          string   `json:"code"`
	MessageKey    string   `json:"message_key"`
	RunID         *string  `json:"run_id"`
	PlanID        *string  `json:"plan_id"`
	SpecSHA256    *string  `json:"spec_sha256"`
	Changed       bool     `json:"changed"`
	Warnings      []string `json:"warnings"`
	NextActions   []string `json:"next_actions"`
	Data          any      `json:"data,omitempty"`
}
