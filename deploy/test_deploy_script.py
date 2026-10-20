import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


DEPLOY_SCRIPT = Path(__file__).resolve().parents[1] / "deploy.sh"
UP_ARGUMENTS = ["up", "--build", "--force-recreate", "-d", "--wait", "--wait-timeout", "600"]
PRIVATE_MARKER = "private-docker-diagnostic-must-not-be-printed"


class DeployScriptTests(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory(prefix="deployment script ")
        self.addCleanup(self.folder.cleanup)
        self.root = Path(self.folder.name).resolve()
        self.project = self.root / "project with spaces"
        self.project.mkdir()
        self.script = self.project / "deploy.sh"
        shutil.copy2(DEPLOY_SCRIPT, self.script)
        self.other = self.root / "caller directory"
        self.other.mkdir()
        self.bin = self.root / "tools"
        self.bin.mkdir()
        # An isolated PATH prevents an accidentally available real Docker from running.
        (self.bin / "dirname").symlink_to(shutil.which("dirname"))
        self.log = self.root / "docker-calls.jsonl"
        self.docker = self.bin / "docker"
        self.docker.write_text(
            f"#!{sys.executable}\n"
            "import json, os, sys\n"
            "from pathlib import Path\n"
            "args = sys.argv[1:]\n"
            "with Path(os.environ['DEPLOY_TEST_LOG']).open('a') as output:\n"
            "    output.write(json.dumps({'argv': args, 'cwd': os.getcwd()}) + '\\n')\n"
            "if args == ['compose', 'version', '--short']:\n"
            "    print(os.environ['DEPLOY_TEST_VERSION'])\n"
            f"    print({PRIVATE_MARKER!r}, file=sys.stderr)\n"
            "    sys.exit(int(os.environ['DEPLOY_TEST_COMPOSE_STATUS']))\n"
            "if args == ['info']:\n"
            f"    print({PRIVATE_MARKER!r})\n"
            f"    print({PRIVATE_MARKER!r}, file=sys.stderr)\n"
            "    sys.exit(int(os.environ['DEPLOY_TEST_DAEMON_STATUS']))\n"
            f"if args and args[0] == 'compose' and args[-7:] == {UP_ARGUMENTS!r}:\n"
            "    sys.exit(int(os.environ['DEPLOY_TEST_UP_STATUS']))\n"
            "sys.exit(97)\n"
        )
        self.docker.chmod(0o755)

    def run_deploy(self, args=(), version="v2.20.0-desktop.1", compose_status=0,
                   daemon_status=0, up_status=0, relative=False):
        environment = os.environ.copy()
        environment.update({
            "PATH": str(self.bin),
            "DEPLOY_TEST_LOG": str(self.log),
            "DEPLOY_TEST_VERSION": version,
            "DEPLOY_TEST_COMPOSE_STATUS": str(compose_status),
            "DEPLOY_TEST_DAEMON_STATUS": str(daemon_status),
            "DEPLOY_TEST_UP_STATUS": str(up_status),
        })
        script_path = "../project with spaces/deploy.sh" if relative else str(self.script)
        result = subprocess.run(["/bin/sh", script_path, *args], cwd=self.other,
                                env=environment, capture_output=True, text=True, timeout=10)
        self.assertNotIn(PRIVATE_MARKER, result.stdout + result.stderr)
        if result.returncode:
            self.assertNotIn("部署成功", result.stdout + result.stderr)
        return result

    def calls(self):
        if not self.log.exists():
            return []
        return [json.loads(line) for line in self.log.read_text().splitlines()]

    def assert_call_sequence(self, arguments):
        calls = self.calls()
        self.assertEqual([call["argv"] for call in calls], arguments)
        self.assertTrue(all(call["cwd"] == str(self.project) for call in calls))

    def test_success_uses_project_directory_and_reports_actual_login_routes(self):
        result = self.run_deploy(relative=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("部署成功", result.stdout)
        self.assertIn("http://127.0.0.1:8787/demo/app/", result.stdout)
        self.assertIn("http://127.0.0.1:8787/demo/app/#/operations-login", result.stdout)
        self.assertIn("首次空库初始化账号：Daydream", result.stdout)
        self.assertIn("已有数据时沿用现有账号和密码", result.stdout)
        self.assert_call_sequence([
            ["compose", "version", "--short"], ["info"], ["compose", *UP_ARGUMENTS],
        ])

    def test_compose_global_arguments_remain_separate_and_literal(self):
        arguments = ["--profile", "workers", "-p", "example-project", "-f", "custom file.yml",
                     "--env-file", "literal $(touch do-not-create).env"]
        result = self.run_deploy(arguments)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.calls()[-1]["argv"], ["compose", *arguments, *UP_ARGUMENTS])
        self.assertFalse((self.project / "do-not-create").exists())

    def test_repeat_deployment_only_runs_same_non_destructive_commands(self):
        first = self.run_deploy()
        second = self.run_deploy()
        self.assertEqual((first.returncode, second.returncode), (0, 0))
        expected = [["compose", "version", "--short"], ["info"], ["compose", *UP_ARGUMENTS]]
        self.assert_call_sequence(expected * 2)
        for call in self.calls():
            self.assertTrue({"down", "rm", "remove", "volume", "prune"}.isdisjoint(call["argv"]))

    def test_missing_docker_fails_before_any_docker_call(self):
        self.docker.unlink()
        result = self.run_deploy()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("未找到 Docker", result.stderr)
        self.assertIn("guide/deployment.md", result.stderr)
        self.assertEqual(self.calls(), [])

    def test_missing_compose_fails_before_daemon_or_up(self):
        result = self.run_deploy(compose_status=17)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Compose 插件不可用", result.stderr)
        self.assert_call_sequence([["compose", "version", "--short"]])

    def test_supported_versions_include_desktop_suffix_and_future_major(self):
        for version in ["2.20.0", "v2.20.0-desktop.1", "v2.39.4+azure-1", "3.0.0", "10.0.0"]:
            with self.subTest(version=version):
                result = self.run_deploy(version=version)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.log.unlink()

    def test_old_or_malformed_version_never_starts_services(self):
        versions = ["1.29.2", "2.9.0", "2.19.99", "", "unknown", "2.20", "v2.20.x",
                    "v2.20.0-", "2.20.0+", "2.20.0 unexpected", "2.20.0\n3.0.0", "2.20.0.1"]
        for version in versions:
            with self.subTest(version=version):
                result = self.run_deploy(version=version)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("Compose", result.stderr)
                self.assert_call_sequence([["compose", "version", "--short"]])
                self.log.unlink()

    def test_unreachable_daemon_does_not_call_up(self):
        result = self.run_deploy(daemon_status=13)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("无法连接 Docker", result.stderr)
        self.assert_call_sequence([["compose", "version", "--short"], ["info"]])

    def test_failed_up_preserves_failure_status_and_gives_log_hint(self):
        result = self.run_deploy(up_status=42)
        self.assertEqual(result.returncode, 42)
        self.assertIn("docker compose logs --tail 100", result.stderr)
        self.assertIn("guide/troubleshooting.md", result.stderr)
        self.assert_call_sequence([
            ["compose", "version", "--short"], ["info"], ["compose", *UP_ARGUMENTS],
        ])


if __name__ == "__main__":
    unittest.main()
