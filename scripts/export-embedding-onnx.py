"""Export the pinned BGE weights to model.onnx once, then prove parity with PyTorch.

Runs only while building the embedding image (or once on a developer host after
setup-local-embedding.py); the runtime never imports torch. The graph returns
the CLS row of the last hidden state, and embed.py applies the same L2
normalization as before, so vectors stay consistent with the PyTorch model.

python scripts/export-embedding-onnx.py .data/evidence-embedding/model
"""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys
import tempfile

import numpy as np
import torch
from transformers import AutoModel, AutoTokenizer

# Covers short/long, mixed scripts, punctuation, padding inside a batch and the query prefix.
PROBES = [
    ["雨夜里主角推开旧书店的门"],
    ["她", "第三幕：林晚在医院走廊接到电话，脸色骤变。", "Act 2 — the CEO's secret, 揭晓！"],
    ["反派回到老宅，翻出二十年前的照片" * 12, "短"],
]
TOLERANCE = 1e-5


class ClsOutput(torch.nn.Module):
    def __init__(self, model):
        super().__init__()
        self.model = model

    def forward(self, input_ids, attention_mask, token_type_ids):
        out = self.model(input_ids=input_ids, attention_mask=attention_mask, token_type_ids=token_type_ids)
        return out.last_hidden_state[:, 0]


def runtime_module():
    path = Path(__file__).resolve().parents[1] / "internal/adapter/localembedding/embed.py"
    spec = importlib.util.spec_from_file_location("embed", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def load_model(folder, **options):
    return AutoModel.from_pretrained(folder, local_files_only=True, trust_remote_code=False,
                                     use_safetensors=True, **options).eval().to("cpu")


def export(folder):
    torch.manual_seed(0)
    torch.set_num_threads(2)
    # Eager attention builds its mask from tensors only; the default SDPA path
    # branches on Python shapes, which tracing would freeze to the sample's size.
    model = load_model(folder, attn_implementation="eager")
    tokenizer = AutoTokenizer.from_pretrained(folder, local_files_only=True, trust_remote_code=False)
    sample = tokenizer(["示例", "更长一点的示例文本"], padding=True, truncation=False, return_tensors="pt")
    names = ["input_ids", "attention_mask", "token_type_ids"]
    target = Path(folder) / "model.onnx"
    temporary = target.with_suffix(".onnx.partial")
    with torch.inference_mode():
        torch.onnx.export(
            ClsOutput(model).eval(), tuple(sample[name] for name in names), str(temporary),
            input_names=names, output_names=["cls"],
            dynamic_axes={**{name: {0: "batch", 1: "sequence"} for name in names}, "cls": {0: "batch"}},
            opset_version=17, do_constant_folding=True, dynamo=False,
        )
    return tokenizer, temporary


def reference(model, tokenizer, texts, query, prefix):
    """The previous PyTorch implementation, kept here only to prove parity."""
    if query:
        texts = [prefix + text for text in texts]
    inputs = tokenizer(texts, padding=True, truncation=False, return_tensors="pt")
    with torch.inference_mode():
        vectors = model(**inputs).last_hidden_state[:, 0]
        return torch.nn.functional.normalize(vectors, p=2, dim=1).numpy(), inputs["input_ids"].numpy()


def verify(model, tokenizer, folder, candidate):
    """Run the candidate through the exact runtime code path; return the worst difference."""
    embed = runtime_module()
    with tempfile.TemporaryDirectory() as staging:
        for name in ("config.json", "tokenizer.json"):
            os.symlink(Path(folder).resolve() / name, Path(staging) / name)
        os.symlink(candidate.resolve(), Path(staging) / "model.onnx")
        fast, onnx_model = embed.load(staging)
        worst = 0.0
        for texts in PROBES:
            for query in (False, True):
                expected, ids = reference(model, tokenizer, texts, query, embed.QUERY_PREFIX)
                encoded = fast.encode_batch([embed.QUERY_PREFIX + t if query else t for t in texts])
                if not np.array_equal(np.array([e.ids for e in encoded]), ids):
                    raise ValueError("tokenizer ids differ from the original tokenizer")
                actual = np.array(embed.embed(fast, onnx_model, texts, query), dtype=np.float32)
                worst = max(worst, float(np.abs(actual - expected).max()))
    if worst > TOLERANCE:
        raise ValueError(f"ONNX vectors differ from PyTorch by {worst:.2e} (limit {TOLERANCE:.0e})")
    return worst


def record(folder, target, worst):
    manifest_path = Path(folder) / "manifest.json"
    if not manifest_path.exists():
        return
    manifest = json.loads(manifest_path.read_text())
    digest = hashlib.sha256(target.read_bytes()).hexdigest()
    manifest["files"]["model.onnx"] = digest
    manifest["onnx"] = {"exportedFrom": "model.safetensors", "opset": 17, "torch": torch.__version__,
                        "maxAbsDiffVsPyTorch": worst}
    manifest_path.write_text(json.dumps(manifest, indent=2) + "\n")


def main():
    folder = sys.argv[1]
    tokenizer, temporary = export(folder)
    try:
        # Parity is checked against the previous runtime's default model, not the export copy.
        worst = verify(load_model(folder), tokenizer, folder, temporary)
    except BaseException:
        temporary.unlink(missing_ok=True)
        raise
    # A failed check never replaces an existing model.onnx.
    target = Path(folder) / "model.onnx"
    temporary.replace(target)
    record(folder, target, worst)
    print(f"Exported model.onnx; max |ONNX - PyTorch| = {worst:.2e}", flush=True)


if __name__ == "__main__":
    main()
