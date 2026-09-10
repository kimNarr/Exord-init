import json
import os
import pathlib
import shutil
import subprocess
import unittest
import uuid
from datetime import datetime, timezone

try:
    import jsonschema
except ImportError:
    jsonschema = None

HAS_JSONSCHEMA = bool(
    jsonschema is not None and hasattr(jsonschema, "Draft202012Validator")
)


ROOT = pathlib.Path(__file__).resolve().parents[1]


@unittest.skipUnless(HAS_JSONSCHEMA, "install requirements-dev.txt")
class SchemaValidationTests(unittest.TestCase):
    def schema(self, name):
        return json.loads((ROOT / "schemas" / name).read_text(encoding="utf-8"))

    def validate(self, instance, schema_name):
        schema = self.schema(schema_name)
        jsonschema.Draft202012Validator.check_schema(schema)
        jsonschema.Draft202012Validator(
            schema, format_checker=jsonschema.FormatChecker()
        ).validate(instance)

    def test_intent_fixture_matches_schema(self):
        for name in ["intent-create-quick-ko.json", "intent-adopt-quick-en.json"]:
            intent = json.loads(
                (ROOT / "tests" / "fixtures" / name).read_text(encoding="utf-8")
            )
            self.validate(intent, "intent.schema.json")

    def test_all_schemas_match_their_metaschema(self):
        for path in sorted((ROOT / "schemas").glob("*.schema.json")):
            jsonschema.Draft202012Validator.check_schema(
                json.loads(path.read_text(encoding="utf-8"))
            )

    @unittest.skipUnless(os.environ.get("EXORD_INIT_BIN"), "set EXORD_INIT_BIN")
    def test_binary_plan_and_apply_results_match_schemas(self):
        binary = os.environ["EXORD_INIT_BIN"]
        intent = ROOT / "tests" / "fixtures" / "intent-create-quick-ko.json"
        local_temp = ROOT / ".tools" / "test-fixtures"
        local_temp.mkdir(parents=True, exist_ok=True)
        target = local_temp / f"schema-{uuid.uuid4().hex}"
        target.mkdir()
        (target / ".git").mkdir()
        approval_path = local_temp / f"approval-{uuid.uuid4().hex}.json"
        try:
            planned = subprocess.run(
                [binary, "plan", "--intent", str(intent), "--target", str(target), "--json"],
                check=True,
                capture_output=True,
                text=True,
                encoding="utf-8",
            )
            plan_result = json.loads(planned.stdout)
            self.validate(plan_result, "result.schema.json")
            self.validate(plan_result["data"]["plan"], "plan.schema.json")
            self.assertFalse(plan_result["changed"])
            run_journal = json.loads(
                (
                    target
                    / ".git"
                    / "exord-init"
                    / "runs"
                    / plan_result["run_id"]
                    / "run.json"
                ).read_text(encoding="utf-8")
            )
            self.validate(run_journal, "run.schema.json")

            approval = dict(plan_result["data"]["approval_request"])
            approval["approved_at"] = datetime.now(timezone.utc).isoformat().replace(
                "+00:00", "Z"
            )
            self.validate(approval, "approval.schema.json")
            approval_path.write_text(json.dumps(approval), encoding="utf-8")

            applied = subprocess.run(
                [
                    binary,
                    "apply",
                    "--target",
                    str(target),
                    "--run-id",
                    plan_result["run_id"],
                    "--approval",
                    str(approval_path),
                    "--json",
                ],
                check=True,
                capture_output=True,
                text=True,
                encoding="utf-8",
            )
            apply_result = json.loads(applied.stdout)
            self.validate(apply_result, "result.schema.json")
            self.assertTrue(apply_result["changed"])
            self.assertTrue((target / "AGENTS.md").is_file())
            self.assertTrue((target / ".exord" / "manifest.json").is_file())
        finally:
            if approval_path.exists():
                approval_path.unlink()
            shutil.rmtree(target)

    @unittest.skipUnless(os.environ.get("EXORD_INIT_BIN"), "set EXORD_INIT_BIN")
    def test_binary_adopt_plan_is_schema_valid_and_target_read_only(self):
        binary = os.environ["EXORD_INIT_BIN"]
        intent = ROOT / "tests" / "fixtures" / "intent-adopt-quick-en.json"
        local_temp = ROOT / ".tools" / "test-fixtures"
        local_temp.mkdir(parents=True, exist_ok=True)
        suffix = uuid.uuid4().hex
        target = local_temp / f"adopt-{suffix}"
        state_home = local_temp / f"adopt-state-{suffix}"
        target.mkdir()
        subprocess.run(
            ["git", "-C", str(target), "init", "-b", "main"],
            check=True,
            capture_output=True,
            text=True,
            encoding="utf-8",
        )
        (target / "src").mkdir()
        (target / "src" / "main.go").write_text("package main\n", encoding="utf-8")
        (target / "AGENTS.md").write_text("user-owned rules\n", encoding="utf-8")
        before = sorted(
            path.relative_to(target).as_posix()
            for path in target.rglob("*")
            if ".git" not in path.relative_to(target).parts
        )
        git_status_before = subprocess.run(
            ["git", "-C", str(target), "status", "--porcelain=v1", "-z"],
            check=True,
            capture_output=True,
        ).stdout
        environment = os.environ.copy()
        if os.name == "nt":
            environment["LOCALAPPDATA"] = str(state_home)
        else:
            environment["XDG_STATE_HOME"] = str(state_home)
        try:
            completed = subprocess.run(
                [binary, "plan", "--intent", str(intent), "--target", str(target), "--json"],
                check=True,
                capture_output=True,
                text=True,
                encoding="utf-8",
                env=environment,
            )
            result = json.loads(completed.stdout)
            self.validate(result, "result.schema.json")
            self.validate(result["data"]["plan"], "plan.schema.json")
            self.assertFalse(result["changed"])
            self.assertIsNone(result["run_id"])
            self.assertEqual(result["data"]["plan"]["spec"]["mode"], "ADOPT")
            conflicts = result["data"]["plan"]["spec"]["adopt_analysis"]["conflicts"]
            self.assertTrue(
                any(item["path"] == "AGENTS.md" for item in conflicts)
            )
            after = sorted(
                path.relative_to(target).as_posix()
                for path in target.rglob("*")
                if ".git" not in path.relative_to(target).parts
            )
            self.assertEqual(before, after)
            self.assertEqual(
                (target / "AGENTS.md").read_text(encoding="utf-8"),
                "user-owned rules\n",
            )
            git_status_after = subprocess.run(
                ["git", "-C", str(target), "status", "--porcelain=v1", "-z"],
                check=True,
                capture_output=True,
            ).stdout
            self.assertEqual(git_status_before, git_status_after)
        finally:
            shutil.rmtree(target)
            if state_home.exists():
                shutil.rmtree(state_home)

    @unittest.skipUnless(os.environ.get("EXORD_INIT_BIN"), "set EXORD_INIT_BIN")
    def test_binary_recovery_inspect_and_approved_rollback(self):
        binary = os.environ["EXORD_INIT_BIN"]
        intent = ROOT / "tests" / "fixtures" / "intent-create-quick-ko.json"
        local_temp = ROOT / ".tools" / "test-fixtures"
        local_temp.mkdir(parents=True, exist_ok=True)
        suffix = uuid.uuid4().hex
        target = local_temp / f"recovery-{suffix}"
        approval_path = local_temp / f"recovery-approval-{suffix}.json"
        target.mkdir()
        (target / ".git").mkdir()
        try:
            planned = subprocess.run(
                [binary, "plan", "--intent", str(intent), "--target", str(target), "--json"],
                check=True,
                capture_output=True,
                text=True,
                encoding="utf-8",
            )
            plan_result = json.loads(planned.stdout)
            run_id = plan_result["run_id"]
            run_dir = target / ".git" / "exord-init" / "runs" / run_id
            plan = plan_result["data"]["plan"]
            operation = next(
                item for item in plan["spec"]["operations"] if item["path"] == "AGENTS.md"
            )
            shutil.copyfile(run_dir / "staging" / "AGENTS.md", target / "AGENTS.md")
            journal_path = run_dir / "run.json"
            journal = json.loads(journal_path.read_text(encoding="utf-8"))
            journal["status"] = "RECOVERY_REQUIRED"
            journal["stage"] = "FILES_APPLYING"
            for item in journal["operations"]:
                if item["path"] == operation["path"]:
                    item["status"] = "APPLYING"
            journal_path.write_text(json.dumps(journal), encoding="utf-8")

            inspected = subprocess.run(
                [binary, "recover", "inspect", "--target", str(target), "--run-id", run_id, "--json"],
                check=True,
                capture_output=True,
                text=True,
                encoding="utf-8",
            )
            inspect_result = json.loads(inspected.stdout)
            self.validate(inspect_result, "result.schema.json")
            self.validate(inspect_result["data"]["recovery"], "recovery.schema.json")
            self.assertFalse(inspect_result["changed"])
            self.assertEqual(
                inspect_result["data"]["recovery"]["disposition"], "ROLLBACK_READY"
            )

            approval = dict(plan_result["data"]["approval_request"])
            approval["approved_action"] = "RECOVER_ROLLBACK"
            approval["approved_at"] = datetime.now(timezone.utc).isoformat().replace(
                "+00:00", "Z"
            )
            self.validate(approval, "approval.schema.json")
            approval_path.write_text(json.dumps(approval), encoding="utf-8")
            rolled_back = subprocess.run(
                [
                    binary,
                    "recover",
                    "rollback",
                    "--target",
                    str(target),
                    "--run-id",
                    run_id,
                    "--approval",
                    str(approval_path),
                    "--json",
                ],
                check=True,
                capture_output=True,
                text=True,
                encoding="utf-8",
            )
            rollback_result = json.loads(rolled_back.stdout)
            self.validate(rollback_result, "result.schema.json")
            self.validate(rollback_result["data"]["recovery"], "recovery.schema.json")
            self.assertTrue(rollback_result["changed"])
            self.assertFalse((target / "AGENTS.md").exists())
            retained = json.loads(journal_path.read_text(encoding="utf-8"))
            self.assertEqual(retained["status"], "FAILED")
            self.assertEqual(retained["stage"], "ROLLED_BACK")
        finally:
            if approval_path.exists():
                approval_path.unlink()
            shutil.rmtree(target)

    @unittest.skipUnless(os.environ.get("EXORD_INIT_BIN"), "set EXORD_INIT_BIN")
    def test_binary_recover_list_and_pre_plan_blocking(self):
        binary = os.environ["EXORD_INIT_BIN"]
        intent = ROOT / "tests" / "fixtures" / "intent-create-quick-ko.json"
        local_temp = ROOT / ".tools" / "test-fixtures"
        local_temp.mkdir(parents=True, exist_ok=True)
        target = local_temp / f"list-{uuid.uuid4().hex}"
        target.mkdir()
        (target / ".git").mkdir()

        def run(*args, check=True):
            return subprocess.run(
                [binary, *args, "--json"],
                check=check,
                capture_output=True,
                text=True,
                encoding="utf-8",
            )

        try:
            plan_result = json.loads(
                run("plan", "--intent", str(intent), "--target", str(target)).stdout
            )
            run_id = plan_result["run_id"]

            listed = json.loads(run("recover", "list", "--target", str(target)).stdout)
            self.validate(listed, "result.schema.json")
            self.validate(
                {
                    "retained_runs": listed["data"]["retained_runs"],
                    "blocking_runs": listed["data"]["blocking_runs"],
                },
                "retained-runs.schema.json",
            )
            self.assertEqual(len(listed["data"]["retained_runs"]), 1)
            self.assertEqual(listed["data"]["retained_runs"][0]["run_id"], run_id)
            self.assertFalse(listed["data"]["retained_runs"][0]["blocks_new_plan"])
            self.assertEqual(listed["data"]["blocking_runs"], 0)

            # A never-applied plan does not block a second plan.
            second = run("plan", "--intent", str(intent), "--target", str(target))
            self.assertEqual(json.loads(second.stdout)["status"], "OK")

            # Force one run into RECOVERY_REQUIRED and confirm blocking.
            journal_path = (
                target / ".git" / "exord-init" / "runs" / run_id / "run.json"
            )
            journal = json.loads(journal_path.read_text(encoding="utf-8"))
            journal["status"] = "RECOVERY_REQUIRED"
            journal["stage"] = "FILES_APPLYING"
            journal_path.write_text(json.dumps(journal), encoding="utf-8")

            listed = json.loads(run("recover", "list", "--target", str(target)).stdout)
            self.assertGreaterEqual(listed["data"]["blocking_runs"], 1)

            blocked = run(
                "plan", "--intent", str(intent), "--target", str(target), check=False
            )
            self.assertEqual(blocked.returncode, 6)
            blocked_result = json.loads(blocked.stdout)
            self.validate(blocked_result, "result.schema.json")
            self.assertEqual(blocked_result["status"], "BLOCKED")
            self.assertEqual(blocked_result["code"], "RECOVERY_REQUIRED")
        finally:
            shutil.rmtree(target)


if __name__ == "__main__":
    unittest.main()
