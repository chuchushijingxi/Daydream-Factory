"""Long-running offline BGE embedding service for the backend.

Serves only the internal Compose network. Loads the model once and reuses the
same inference code (embed.py) as the backend's local mode, so vectors and the
index identity are identical whichever mode produced them.
"""
import argparse
import hashlib
import json
import os
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import embed  # noqa: E402  (shared inference code)

# Must match localembedding.ModelFiles in embed.go.
MODEL_FILES = ["model.onnx", "config.json", "tokenizer.json"]
DIMENSION = 512
MAX_BODY = 1 << 20


def identity(folder):
    """Same digest as localembedding.New: embed.py, then each model file name and bytes."""
    digest = hashlib.sha256()
    with open(os.path.join(HERE, "embed.py"), "rb") as worker:
        digest.update(worker.read())
    for name in MODEL_FILES:
        digest.update(name.encode())
        with open(os.path.join(folder, name), "rb") as part:
            for chunk in iter(lambda: part.read(1 << 20), b""):
                digest.update(chunk)
    return "bge-small-zh-v1.5:cls-l2:" + digest.hexdigest()


def serve(listen, folder):
    tokenizer, model = embed.load(folder)
    model_identity = identity(folder)
    lock = threading.Lock()

    class Handler(BaseHTTPRequestHandler):
        def reply(self, status, payload):
            body = json.dumps(payload, allow_nan=False).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self):
            if self.path == "/healthz":
                self.reply(200, {"status": "ok"})
            elif self.path == "/v1/identity":
                self.reply(200, {"identity": model_identity, "dimension": DIMENSION})
            else:
                self.reply(404, {"error": "NOT_FOUND"})

        def do_POST(self):
            if self.path != "/v1/embed":
                self.reply(404, {"error": "NOT_FOUND"})
                return
            length = int(self.headers.get("Content-Length") or 0)
            if length <= 0 or length > MAX_BODY:
                self.reply(400, {"error": "INVALID_INPUT"})
                return
            try:
                request = json.loads(self.rfile.read(length))
                texts, query = request["texts"], bool(request.get("query"))
                embed.validate(texts)
            except (ValueError, KeyError, TypeError):
                self.reply(400, {"error": "INVALID_INPUT"})
                return
            # 推理本身已用多线程，串行处理请求以控制内存与 CPU。
            with lock:
                vectors = embed.embed(tokenizer, model, texts, query)
            if vectors is None:
                self.reply(200, {"error": embed.CAPACITY_EXCEEDED})
                return
            self.reply(200, {"vectors": vectors})

        def log_message(self, *args):
            # 不记录请求正文或文本内容。
            pass

    host, _, port = listen.rpartition(":")
    server = ThreadingHTTPServer((host or "0.0.0.0", int(port)), Handler)
    print(f"embedding service listening at http://{listen} ({model_identity})", flush=True)
    server.serve_forever()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--listen", default="0.0.0.0:8791")
    parser.add_argument("--model", required=True)
    args = parser.parse_args()
    serve(args.listen, args.model)


if __name__ == "__main__":
    main()
