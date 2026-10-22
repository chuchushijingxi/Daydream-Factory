"""Offline BGE inference. No downloads, external APIs, truncation or remote code.

The backend's local mode runs this file once per batch over stdin/stdout; the
embedding service imports load() and embed() and keeps the model loaded. Both
hash this file into the index identity, so they always agree.
"""
import json
import sys

import torch
from transformers import AutoModel, AutoTokenizer

QUERY_PREFIX = "为这个句子生成表示以用于检索相关文章："
CAPACITY_EXCEEDED = "EMBEDDING_CAPACITY_EXCEEDED"


def validate(texts):
    if not texts or len(texts) > 32 or any(not isinstance(t, str) for t in texts):
        raise ValueError("invalid local embedding batch")


def load(folder):
    tokenizer = AutoTokenizer.from_pretrained(folder, local_files_only=True, trust_remote_code=False)
    model = AutoModel.from_pretrained(folder, local_files_only=True, trust_remote_code=False,
                                      use_safetensors=True).eval().to("cpu")
    torch.set_num_threads(2)
    return tokenizer, model


def embed(tokenizer, model, texts, query):
    """Return L2-normalized CLS vectors, or None when the batch exceeds model capacity."""
    validate(texts)
    if query:
        texts = [QUERY_PREFIX + text for text in texts]
    inputs = tokenizer(texts, padding=True, truncation=False, return_tensors="pt")
    if inputs["input_ids"].shape[1] > model.config.max_position_embeddings:
        return None
    with torch.inference_mode():
        vectors = model(**inputs).last_hidden_state[:, 0]
        vectors = torch.nn.functional.normalize(vectors, p=2, dim=1)
    return vectors.tolist()


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
