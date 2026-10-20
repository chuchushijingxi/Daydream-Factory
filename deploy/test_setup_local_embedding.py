import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "setup_embedding", Path(__file__).resolve().parents[1] / "scripts/setup-local-embedding.py"
)
setup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(setup)


class WeightIntegrityTests(unittest.TestCase):
    def download(self, root, content):
        # All writes and HTTP responses are isolated; real models stay untouched.
        license_file = root / "deploy/licenses/bge-small-zh-v1.5-LICENSE"
        license_file.parent.mkdir(parents=True, exist_ok=True)
        license_file.write_text("fixture license")
        with patch.object(setup, "__file__", str(root / "scripts/setup-local-embedding.py")), \
             patch.object(setup, "FILES", ["model.safetensors"]), \
             patch.object(setup, "MODEL_SIZE", 3), \
             patch.object(setup, "MODEL_SHA256", hashlib.sha256(b"abc").hexdigest()), \
             patch.object(setup.urllib.request, "urlopen", return_value=io.BytesIO(content)):
            setup.main()

    def test_valid_model_and_license_are_recorded(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            self.download(root, b"abc")
            model = root / ".data/evidence-embedding/model"
            manifest = json.loads((model / "manifest.json").read_text())
            self.assertEqual((model / "model.safetensors").read_bytes(), b"abc")
            self.assertEqual(manifest["files"]["model.safetensors"], hashlib.sha256(b"abc").hexdigest())
            self.assertEqual((model / "LICENSE").read_text(), "fixture license")

    def test_partial_or_wrong_weight_never_replaces_existing_model(self):
        for content in [b"ab", b"bad"]:
            with self.subTest(content=content), tempfile.TemporaryDirectory() as folder:
                root = Path(folder)
                model = root / ".data/evidence-embedding/model"
                model.mkdir(parents=True)
                target = model / "model.safetensors"
                target.write_bytes(b"previous validated model")
                with self.assertRaises(ValueError):
                    self.download(root, content)
                self.assertEqual(target.read_bytes(), b"previous validated model")
                self.assertFalse((model / "manifest.json").exists())
                self.assertFalse((model / "model.download").exists())


if __name__ == "__main__":
    unittest.main()
