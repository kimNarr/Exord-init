package approval

import (
	"fmt"
	"testing"
	"time"

	"github.com/kimNarr/Exord-init/engine/internal/protocol"
)

func TestDecodeAcceptsKnownActionsOnly(t *testing.T) {
	for _, action := range []string{"APPLY_CREATE", "RECOVER_ROLLBACK"} {
		raw := []byte(fmt.Sprintf(`{"schema_version":1,"run_id":"r","plan_id":"p","spec_sha256":"s","target_identity_sha256":"t","approved_action":%q,"approved_at":"2026-09-10T00:00:00Z"}`, action))
		if _, err := Decode(raw); err != nil {
			t.Fatalf("known action %s was rejected: %v", action, err)
		}
	}
	if _, err := Decode([]byte(`{"schema_version":1,"approved_action":"DELETE_ALL","approved_at":"2026-09-10T00:00:00Z"}`)); err == nil {
		t.Fatal("unknown action was accepted")
	}
}

func TestVerifyBindsRecoveryApproval(t *testing.T) {
	started := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	value := protocol.Approval{SchemaVersion: 1, RunID: "run", PlanID: "plan", SpecSHA256: "spec", TargetIdentitySHA256: "target", ApprovedAction: "RECOVER_ROLLBACK", ApprovedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := Verify(value, "RECOVER_ROLLBACK", "run", "plan", "spec", "target", started); err != nil {
		t.Fatal(err)
	}
	value.SpecSHA256 = "other"
	if err := Verify(value, "RECOVER_ROLLBACK", "run", "plan", "spec", "target", started); err == nil {
		t.Fatal("mismatched approval was accepted")
	}
}
