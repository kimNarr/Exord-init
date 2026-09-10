import json
import pathlib
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]


class ContractTests(unittest.TestCase):
    def test_all_schemas_are_valid_json_with_draft_marker(self):
        schema_paths = sorted((ROOT / "schemas").glob("*.schema.json"))
        self.assertEqual(
            {path.name for path in schema_paths},
            {
                "approval.schema.json",
                "intent.schema.json",
                "manifest.schema.json",
                "plan.schema.json",
                "recovery.schema.json",
                "result.schema.json",
                "retained-runs.schema.json",
                "run.schema.json",
            },
        )
        for path in schema_paths:
            schema = json.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(schema["$schema"], "https://json-schema.org/draft/2020-12/schema")
            self.assertEqual(schema["type"], "object")

    def test_skill_has_no_scaffold_todos(self):
        skill = (ROOT / "skill" / "exord-init" / "SKILL.md").read_text(encoding="utf-8")
        self.assertNotIn("TODO", skill)
        self.assertLess(len(skill.splitlines()), 200)

    def test_project_agent_templates_stay_small(self):
        templates = (ROOT / "skill" / "exord-init" / "assets" / "templates").glob("*/AGENTS.md.tmpl")
        for path in templates:
            content = path.read_bytes()
            self.assertLessEqual(len(content), 16 * 1024)
            self.assertLessEqual(len(content.splitlines()), 200)

    def test_feedback_is_not_part_of_runtime_package(self):
        runtime_files = list((ROOT / "skill" / "exord-init").rglob("*"))
        self.assertFalse(any("feedback" in path.parts for path in runtime_files))

    def test_english_and_korean_readmes_share_critical_contract_terms(self):
        readmes = [
            (ROOT / "README.md").read_text(encoding="utf-8"),
            (ROOT / "README.ko.md").read_text(encoding="utf-8"),
        ]
        for content in readmes:
            for required in [
                "CREATE + QUICK",
                "ADOPT + QUICK",
                "RECOVER_ROLLBACK",
                "doctor --json",
                "APPLY_CREATE",
                "spec_sha256",
                "go test ./...",
                "Apache License 2.0",
            ]:
                self.assertIn(required, content)
            self.assertIn("vibe-coding", content)

    def test_readmes_list_every_engine_capability(self):
        # engineCapabilities in engine/cmd/exord-init/main.go is the single
        # source of truth; both READMEs must mention each capability token so
        # the doctor output and the docs never drift apart.
        source = (ROOT / "engine" / "cmd" / "exord-init" / "main.go").read_text(
            encoding="utf-8"
        )
        block = source.split("engineCapabilities = []string{", 1)[1].split("}", 1)[0]
        capabilities = [line.split('"')[1] for line in block.splitlines() if '"' in line]
        self.assertEqual(len(capabilities), 7)
        readmes = [
            (ROOT / "README.md").read_text(encoding="utf-8"),
            (ROOT / "README.ko.md").read_text(encoding="utf-8"),
        ]
        for content in readmes:
            for capability in capabilities:
                self.assertIn(capability, content)


if __name__ == "__main__":
    unittest.main()
