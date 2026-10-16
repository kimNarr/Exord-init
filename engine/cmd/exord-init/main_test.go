package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	applyengine "github.com/kimNarr/Exord-init/engine/internal/apply"
	"github.com/kimNarr/Exord-init/engine/internal/protocol"
)

func TestFailureRedactsCause(t *testing.T) {
	result := failure("plan", "UNSUPPORTED_TARGET", "error.target_unsafe", secretError("C:\\private\\secret.txt"))
	if len(result.Warnings) != 0 {
		t.Fatalf("cause leaked into result: %v", result.Warnings)
	}
}

func TestHardLinkFailureProvidesRedactedRecoveryGuidance(t *testing.T) {
	for _, code := range []string{"APPLY_FAILED", "RECOVERY_REQUIRED"} {
		cause := fmt.Errorf("private-path-secret: %w", applyengine.ErrHardLinksUnsupported)
		result := failure("apply", code, "error.apply_failed", cause)
		if result.Code != code || result.MessageKey != "error.hard_links_unsupported" || len(result.NextActions) == 0 {
			t.Fatalf("missing guidance or lost recovery code: %#v", result)
		}
		encoded, err := json.Marshal(result)
		if err != nil || strings.Contains(string(encoded), "private-path-secret") {
			t.Fatalf("error cause leaked: %s (%v)", encoded, err)
		}
	}
}

type secretError string

func TestCLITaskRejectsUnsupportedActionsAndInvalidFlags(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		code   int
		reason string
	}{
		{[]string{"task", "apply"}, 7, "UNSUPPORTED_CAPABILITY"},
		{[]string{"task", "plan"}, 2, "INVALID_INTENT"},
		{[]string{"task", "plan", "--intent", "missing.json", "--target", ".", "--protocol-version", "999"}, 7, "VERSION_MISMATCH"},
		{[]string{"task", "plan", "--intent", "missing.json", "--target", ".", "extra"}, 2, "INVALID_INTENT"},
	} {
		res, code, raw := runCLI(t, tc.args...)
		if code != tc.code || res.Code != tc.reason || res.Command != "task" || res.Changed {
			t.Fatalf("unexpected task failure: %s (exit %d)", raw, code)
		}
	}
}

func (e secretError) Error() string { return string(e) }

// TestHelperProcess is used to run main() in a subprocess for end-to-end CLI testing.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	os.Args = append([]string{"exord-init"}, args...)
	main()
}

func runCLI(t *testing.T, args ...string) (protocol.Result, int, string) {
	t.Helper()
	cmdArgs := append([]string{"-test.run=TestHelperProcess", "--"}, args...)
	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to execute helper process: %v", err)
		}
	}
	var res protocol.Result
	if jsonErr := json.Unmarshal(out, &res); jsonErr != nil {
		// Output may not be JSON if a panic or unexpected stdout occurred
		return res, exitCode, string(out)
	}
	return res, exitCode, string(out)
}

func TestCLINoArgsEmitsCommandRequired(t *testing.T) {
	res, code, raw := runCLI(t)
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d. Output: %s", code, raw)
	}
	if res.Status != "FAILED" || res.Code != "INVALID_INTENT" || res.MessageKey != "error.command_required" {
		t.Fatalf("unexpected result: %#v", res)
	}
}

func TestCLIUnknownCommandEmitsUnknownCommand(t *testing.T) {
	res, code, raw := runCLI(t, "foobar")
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d. Output: %s", code, raw)
	}
	if res.Status != "FAILED" || res.Code != "INVALID_INTENT" || res.MessageKey != "error.unknown_command" {
		t.Fatalf("unexpected result: %#v", res)
	}
}

func TestCLIDoctorOutputsValidJSONAndCapabilities(t *testing.T) {
	res, code, raw := runCLI(t, "doctor", "--json")
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d. Output: %s", code, raw)
	}
	if res.Status != "OK" || res.Code != "OK" || res.MessageKey != "doctor.ok" {
		t.Fatalf("unexpected doctor result: %#v", res)
	}
	dataMap, ok := res.Data.(map[string]any)
	if !ok {
		t.Fatalf("res.Data is not map[string]any: %T", res.Data)
	}
	capsRaw, ok := dataMap["capabilities"]
	if !ok {
		t.Fatalf("missing capabilities in doctor data: %#v", res.Data)
	}
	slice, ok := capsRaw.([]any)
	if !ok {
		t.Fatalf("capabilities is not a slice: %T", capsRaw)
	}
	caps := make([]string, len(slice))
	for i, c := range slice {
		caps[i] = c.(string)
	}
	if !reflect.DeepEqual(caps, engineCapabilities) {
		t.Fatalf("doctor capabilities mismatch:\ngot  %v\nwant %v", caps, engineCapabilities)
	}
}

func TestCLIPlanMissingRequiredFlags(t *testing.T) {
	res, code, raw := runCLI(t, "plan", "--json")
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d. Output: %s", code, raw)
	}
	if res.Status != "FAILED" || res.Code != "INVALID_INTENT" || res.MessageKey != "error.plan_flags" {
		t.Fatalf("unexpected plan result: %#v", res)
	}
}

func TestCLIPlanProtocolVersionMismatch(t *testing.T) {
	res, code, raw := runCLI(t, "plan", "--intent", "i.json", "--target", ".", "--protocol-version", "999", "--json")
	if code != 7 {
		t.Fatalf("expected exit code 7 for version mismatch, got %d. Output: %s", code, raw)
	}
	if res.Status != "FAILED" || res.Code != "VERSION_MISMATCH" || res.MessageKey != "error.protocol_version" {
		t.Fatalf("unexpected version mismatch result: %#v", res)
	}
}

func TestCLIApplyMissingRequiredFlags(t *testing.T) {
	res, code, raw := runCLI(t, "apply", "--json")
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d. Output: %s", code, raw)
	}
	if res.Status != "FAILED" || res.Code != "INVALID_INTENT" || res.MessageKey != "error.apply_flags" {
		t.Fatalf("unexpected apply result: %#v", res)
	}
}

func TestCLIRecoverMissingAction(t *testing.T) {
	res, code, raw := runCLI(t, "recover")
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d. Output: %s", code, raw)
	}
	if res.Status != "FAILED" || res.Code != "INVALID_INTENT" || res.MessageKey != "error.recover_action" {
		t.Fatalf("unexpected recover result: %#v", res)
	}
}

func TestCLIRecoverUnknownAction(t *testing.T) {
	res, code, raw := runCLI(t, "recover", "invalid-subaction")
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d. Output: %s", code, raw)
	}
	if res.Status != "FAILED" || res.Code != "INVALID_INTENT" || res.MessageKey != "error.recover_action" {
		t.Fatalf("unexpected recover result: %#v", res)
	}
}

func TestCLIRecoverInspectMissingFlags(t *testing.T) {
	res, code, raw := runCLI(t, "recover", "inspect", "--json")
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d. Output: %s", code, raw)
	}
	if res.Status != "FAILED" || res.Code != "INVALID_INTENT" || !strings.HasPrefix(res.MessageKey, "error.recover_") {
		t.Fatalf("unexpected recover inspect result: %#v", res)
	}
}

func TestCLIRecoverRollbackMissingApproval(t *testing.T) {
	res, code, raw := runCLI(t, "recover", "rollback", "--target", ".", "--run-id", "123e4567-e89b-42d3-a456-426614174000", "--json")
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d. Output: %s", code, raw)
	}
	if res.Status != "FAILED" || res.Code != "INVALID_INTENT" {
		t.Fatalf("unexpected recover rollback result: %#v", res)
	}
}

func TestCLIRecoverListMissingTarget(t *testing.T) {
	res, code, raw := runCLI(t, "recover", "list", "--json")
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d. Output: %s", code, raw)
	}
	if res.Status != "FAILED" || res.Code != "INVALID_INTENT" {
		t.Fatalf("unexpected recover list result: %#v", res)
	}
}

func TestCLIRecoverDiscardMissingFlags(t *testing.T) {
	res, code, raw := runCLI(t, "recover", "discard", "--json")
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d. Output: %s", code, raw)
	}
	if res.Status != "FAILED" || res.Code != "INVALID_INTENT" {
		t.Fatalf("unexpected recover discard result: %#v", res)
	}
}

func TestCLIPlanSuggestsAdoptForPreexistingRepoFile(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secretName := "README.md"
	if err := os.WriteFile(filepath.Join(dir, secretName), []byte("hosted repo readme"), 0o600); err != nil {
		t.Fatal(err)
	}
	intent := filepath.Join("..", "..", "..", "tests", "fixtures", "intent-create-quick-ko.json")
	res, code, raw := runCLI(t, "plan", "--intent", intent, "--target", dir, "--json")
	if code != 4 {
		t.Fatalf("expected exit code 4, got %d. Output: %s", code, raw)
	}
	if res.Status != "FAILED" || res.Code != "UNSUPPORTED_TARGET" || res.Changed {
		t.Fatalf("unexpected plan result: %#v", res)
	}
	if len(res.NextActions) == 0 || !strings.Contains(strings.Join(res.NextActions, "\n"), "ADOPT") {
		t.Fatalf("expected ADOPT guidance in next actions: %#v", res.NextActions)
	}
	if strings.Contains(raw, dir) || strings.Contains(raw, secretName) {
		t.Fatalf("failure output must not echo the target path or entry name: %s", raw)
	}
}
