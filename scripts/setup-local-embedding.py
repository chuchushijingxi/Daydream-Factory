"""Download pinned public BAAI weights only. Never reads or sends project text.

Host setup (the Compose image does the same in its export stage). The runtime
venv carries ONNX Runtime only; PyTorch lives in a throwaway export venv:

uv venv --python 3.12 .data/evidence-embedding/runtime
uv pip install --python .data/evidence-embedding/runtime/bin/python -r deploy/embedding-requirements.txt --require-hashes
python3 scripts/setup-local-embedding.py
uv venv --python 3.12 .data/evidence-embedding/export
uv pip install --python .data/evidence-embedding/export/bin/python -r deploy/embedding-export-requirements.txt --extra-index-url https://download.pytorch.org/whl/cpu --require-hashes
.data/evidence-embedding/export/bin/python scripts/export-embedding-onnx.py .data/evidence-embedding/model
"""
import hashlib
import json
from pathlib import Path
import urllib.request

REVISION = "7999e1d3359715c523056ef9478215996d62a620"
# Fixed-revision Git LFS pointer from the model publisher, not a local download digest.
MODEL_SIZE = 95827648
MODEL_SHA256 = "354763b9b1357bc9c44f62c6be2276321081ed2567773608c0d0785b61d5a026"
FILES = ["model.safetensors", "config.json", "tokenizer.json", "tokenizer_config.json",
         "special_tokens_map.json", "vocab.txt", "README.md"]


def main():
    folder = Path(__file__).resolve().parents[1] / ".data/evidence-embedding/model"
    folder.mkdir(parents=True, exist_ok=True)
    manifest = {"model": "BAAI/bge-small-zh-v1.5", "revision": REVISION, "files": {}}
    for name in FILES:
        url = f"https://huggingface.co/BAAI/bge-small-zh-v1.5/resolve/{REVISION}/{name}?download=true"
        target = folder / name
        # Explicit setup downloads a pinned version, using an atomic rename.
        temporary = target.with_suffix(target.suffix + ".download")
        with urllib.request.urlopen(url, timeout=120) as response, temporary.open("wb") as output:
            digest = hashlib.sha256()
            while block := response.read(1 << 20):
                output.write(block)
                digest.update(block)
        if name == "model.safetensors" and (temporary.stat().st_size != MODEL_SIZE or digest.hexdigest() != MODEL_SHA256):
            temporary.unlink()
            raise ValueError("BGE weight size/SHA-256 does not match its pinned official LFS pointer")
        temporary.replace(target)
        manifest["files"][name] = digest.hexdigest()
        print(f"Saved {name}", flush=True)
    license_file = Path(__file__).resolve().parents[1] / "deploy/licenses/bge-small-zh-v1.5-LICENSE"
    license_data = license_file.read_bytes()
    (folder / "LICENSE").write_bytes(license_data)
    manifest["files"]["LICENSE"] = hashlib.sha256(license_data).hexdigest()
    (folder / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")


if __name__ == "__main__":
    main()
