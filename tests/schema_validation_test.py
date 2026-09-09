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
        intent = json.loads(
            (ROOT / "tests" / "fixtures" / "intent-create-quick-ko.json").read_text(
                encoding="utf-8"
            )
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


if __name__ == "__main__":
    unittest.main()
