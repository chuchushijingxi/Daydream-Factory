"""Offline BGE inference. No downloads, external APIs, truncation or remote code."""
import json
import sys

import torch
from transformers import AutoModel, AutoTokenizer


def main():
    request = json.load(sys.stdin)
    texts = request["texts"]
    if not texts or len(texts) > 32 or any(not isinstance(t, str) for t in texts):
        raise ValueError("invalid local embedding batch")
    folder = sys.argv[1]
    tokenizer = AutoTokenizer.from_pretrained(folder, local_files_only=True, trust_remote_code=False)
    model = AutoModel.from_pretrained(folder, local_files_only=True, trust_remote_code=False,
                                      use_safetensors=True).eval().to("cpu")
    torch.set_num_threads(2)
    if request.get("query"):
        texts = ["为这个句子生成表示以用于检索相关文章：" + text for text in texts]
    inputs = tokenizer(texts, padding=True, truncation=False, return_tensors="pt")
    if inputs["input_ids"].shape[1] > model.config.max_position_embeddings:
        print(json.dumps({"error": "EMBEDDING_CAPACITY_EXCEEDED"}), flush=True)
        return
    with torch.inference_mode():
        vectors = model(**inputs).last_hidden_state[:, 0]
        vectors = torch.nn.functional.normalize(vectors, p=2, dim=1)
    print(json.dumps({"vectors": vectors.tolist()}, allow_nan=False), flush=True)


if __name__ == "__main__":
    main()
