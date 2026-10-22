import contextlib
import importlib.util
import io
from pathlib import Path
import stat
import tempfile
import unittest


spec = importlib.util.spec_from_file_location("bootstrap", Path(__file__).with_name("bootstrap-compose.py"))
bootstrap = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bootstrap)


class BootstrapTests(unittest.TestCase):
    def initialize(self, root):
        with contextlib.redirect_stdout(io.StringIO()):
            bootstrap.initialize(root)

    def test_fresh_configuration_is_private_and_repeatable(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            self.initialize(root)
            target = root / "config"
            original = {path.name: path.read_bytes() for path in target.iterdir()}
            self.assertEqual(set(original), {"app.local.yaml", "postgres-password", "minio-password", "minio-user", "redis-password"})
            # 新部署默认开启演示收银台，开箱可体验充值全流程。
            config = (target / "app.local.yaml").read_text()
            self.assertTrue(config.startswith("environment: development\npayment:\n  demo:\n    enabled: true\n"))
            for path in target.iterdir():
                self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
            self.initialize(root)
            self.assertTrue(original == {path.name: path.read_bytes() for path in target.iterdir()})

    def test_existing_environment_only_adds_redis(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            self.initialize(root)
            target = root / "config"
            config = target / "app.local.yaml"
            legacy = config.read_text().split("\nredis:\n")[0] + "\n"
            config.write_text(legacy)
            (target / "redis-password").unlink()
            original = {name: (target / name).read_bytes() for name in ["postgres-password", "minio-password"]}
            self.initialize(root)
            self.assertTrue(config.read_text().startswith(legacy))
            self.assertIn("\nredis:\n", config.read_text())
            self.assertTrue(all((target / name).read_bytes() == value for name, value in original.items()))

    def test_interrupted_redis_upgrade_reuses_its_secret(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            self.initialize(root)
            target = root / "config"
            config = target / "app.local.yaml"
            config.write_text(config.read_text().split("\nredis:\n")[0] + "\n")
            secret = (target / "redis-password").read_bytes()
            self.initialize(root)
            self.assertTrue(secret == (target / "redis-password").read_bytes())

    def test_insecure_existing_config_is_rejected(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            self.initialize(root)
            (root / "config" / "redis-password").chmod(0o644)
            with self.assertRaises(ValueError):
                self.initialize(root)


if __name__ == "__main__":
    unittest.main()
