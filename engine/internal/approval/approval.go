package approval

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/kimNarr/Exord-init/engine/internal/protocol"
)

// clockSkewAllowance bounds how far an approval's approved_at may sit ahead of
// the engine's current clock. It absorbs benign wall-clock skew between the
// machine that wrote the approval and the machine that runs apply; it is not a
// validity window and must stay small. The lower bound stays a fixed one-second
// grace so an approval created in the same second as the run still verifies.
const clockSkewAllowance = 5 * time.Minute

func Decode(data []byte) (protocol.Approval, error) {
	var value protocol.Approval
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return value, errors.New("approval contains trailing JSON values")
	}
	if value.SchemaVersion != 1 || (value.ApprovedAction != "APPLY_CREATE" && value.ApprovedAction != "RECOVER_ROLLBACK" && value.ApprovedAction != "RECOVER_DISCARD") {
		return value, errors.New("unsupported approval")
	}
	if _, err := time.Parse(time.RFC3339, value.ApprovedAt); err != nil {
		return value, errors.New("invalid approval timestamp")
	}
	return value, nil
}

func Verify(value protocol.Approval, action, runID, planID, specSHA256, targetIdentitySHA256, startedAt string) error {
	if value.SchemaVersion != 1 || value.ApprovedAction != action {
		return errors.New("approval action mismatch")
	}
	if value.RunID != runID || value.PlanID != planID || value.SpecSHA256 != specSHA256 || value.TargetIdentitySHA256 != targetIdentitySHA256 {
		return errors.New("approval binding mismatch")
	}
	approvedAt, err := time.Parse(time.RFC3339, value.ApprovedAt)
	if err != nil {
		return errors.New("invalid approval timestamp")
	}
	started, err := time.Parse(time.RFC3339Nano, startedAt)
	if err != nil || approvedAt.Before(started.Add(-time.Second)) || approvedAt.After(time.Now().UTC().Add(clockSkewAllowance)) {
		return errors.New("approval timestamp is outside the run window")
	}
	return nil
}
