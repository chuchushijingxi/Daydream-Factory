"""Offline BGE inference with ONNX Runtime. No downloads, external APIs, truncation or remote code.

The backend's local mode runs this file once per batch over stdin/stdout; the
embedding service imports load() and embed() and keeps the model loaded. Both
hash this file into the index identity, so they always agree. model.onnx is
exported from the pinned weights by scripts/export-embedding-onnx.py, which
also proves parity with the original PyTorch model.
"""
import json
import os
import sys

import numpy as np
import onnxruntime
from tokenizers import Tokenizer

QUERY_PREFIX = "为这个句子生成表示以用于检索相关文章："
CAPACITY_EXCEEDED = "EMBEDDING_CAPACITY_EXCEEDED"


class Model:
    def __init__(self, session, limit):
        self.session, self.limit = session, limit


def validate(texts):
    if not texts or len(texts) > 32 or any(not isinstance(t, str) for t in texts):
        raise ValueError("invalid local embedding batch")


def load(folder):
    tokenizer = Tokenizer.from_file(os.path.join(folder, "tokenizer.json"))
    tokenizer.no_truncation()
    tokenizer.enable_padding(pad_id=0, pad_token="[PAD]", pad_type_id=0)
    with open(os.path.join(folder, "config.json"), encoding="utf-8") as config:
        limit = json.load(config)["max_position_embeddings"]
    options = onnxruntime.SessionOptions()
    options.intra_op_num_threads = 2
    options.inter_op_num_threads = 1
    session = onnxruntime.InferenceSession(os.path.join(folder, "model.onnx"), options,
                                           providers=["CPUExecutionProvider"])
    return tokenizer, Model(session, limit)


def embed(tokenizer, model, texts, query):
    """Return L2-normalized CLS vectors, or None when the batch exceeds model capacity."""
    validate(texts)
    if query:
        texts = [QUERY_PREFIX + text for text in texts]
    encodings = tokenizer.encode_batch(texts)
    if len(encodings[0].ids) > model.limit:
        return None
    inputs = {
        "input_ids": np.array([e.ids for e in encodings], dtype=np.int64),
        "attention_mask": np.array([e.attention_mask for e in encodings], dtype=np.int64),
        "token_type_ids": np.array([e.type_ids for e in encodings], dtype=np.int64),
    }
    (vectors,) = model.session.run(["cls"], inputs)
    # Same as torch.nn.functional.normalize(p=2, eps=1e-12).
    norms = np.linalg.norm(vectors, axis=1, keepdims=True)
    return (vectors / np.maximum(norms, 1e-12)).tolist()


def main():
    request = json.load(sys.stdin)
    validate(request["texts"])
    tokenizer, model = load(sys.argv[1])
    vectors = embed(tokenizer, model, request["texts"], request.get("query"))
    if vectors is None:
        print(json.dumps({"error": CAPACITY_EXCEEDED}), flush=True)
        return
    print(json.dumps({"vectors": vectors}, allow_nan=False), flush=True)


if __name__ == "__main__":
    main()
