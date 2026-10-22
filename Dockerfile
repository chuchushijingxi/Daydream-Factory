# 后端镜像：API、任务执行与初始化工具。工作台页面由 deploy/Dockerfile.frontend 单独构建。
# Pin the actual compiler independently of the published base image tag.
FROM golang:1.26.9-bookworm AS backend
ENV GOTOOLCHAIN=go1.26.10 GOMAXPROCS=2 GOFLAGS=-p=2
WORKDIR /src
ARG GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download -x
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -o /out/drama-server ./cmd/server \
    && CGO_ENABLED=0 go build -trimpath -o /out/drama-task-worker ./cmd/task-worker \
    && CGO_ENABLED=0 go build -trimpath -o /out/drama-accounts-init ./cmd/accounts-init \
    && CGO_ENABLED=0 go build -trimpath -o /out/drama-smtp-import ./cmd/smtp-import

# Build only the local media tools we use, with their pinned source and licenses.
FROM debian:trixie-slim@sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f AS media
COPY deploy/media/ /setup/media/
RUN sh /setup/media/build.sh
COPY deploy/Dockerfile.media /opt/media/source/Dockerfile.media

FROM python:3.12-slim-bookworm AS embedding
COPY deploy/pip-requirements.txt /setup/deploy/pip-requirements.txt
RUN python -m venv /opt/evidence-embedding/runtime \
    && /opt/evidence-embedding/runtime/bin/pip install --no-cache-dir --require-hashes \
       -r /setup/deploy/pip-requirements.txt
COPY deploy/embedding-requirements.txt /setup/deploy/embedding-requirements.txt
RUN /opt/evidence-embedding/runtime/bin/pip install --no-cache-dir --require-hashes \
       -r /setup/deploy/embedding-requirements.txt --extra-index-url https://download.pytorch.org/whl/cpu
COPY scripts/setup-local-embedding.py /setup/scripts/setup-local-embedding.py
COPY deploy/licenses/bge-small-zh-v1.5-LICENSE /setup/deploy/licenses/bge-small-zh-v1.5-LICENSE
RUN python /setup/scripts/setup-local-embedding.py \
    && mv /setup/.data/evidence-embedding/model /opt/evidence-embedding/model \
    && /opt/evidence-embedding/runtime/bin/python -m pip uninstall -y pip

FROM python:3.12-slim-trixie
RUN apt-get update \
    && apt-get upgrade -y \
    && apt-get install -y --no-install-recommends ca-certificates curl libgomp1 libstdc++6 \
    && python -m pip uninstall -y pip \
    && rm -rf /var/lib/apt/lists/* /usr/local/lib/python3.12/ensurepip
WORKDIR /app
COPY --from=backend /out/ /usr/local/bin/
# 风格目录 catalog.json 等运行时数据由后端读取；页面文件只在前端镜像中。
COPY demo/assets/ ./demo/assets/
COPY --from=embedding /opt/evidence-embedding/ /opt/evidence-embedding/
COPY --from=media /opt/media/ /opt/media/
ENV PATH="/opt/media/bin:${PATH}"
COPY deploy/bootstrap-compose.py /usr/local/lib/drama-agent/bootstrap-compose.py
COPY LICENSE NOTICE /usr/share/licenses/drama-agent/
COPY deploy/licenses/ /usr/share/licenses/drama-agent/third-party/
# 可变密钥保存在卷，固定推理资源随镜像更新，避免把旧权重固化在数据卷。
RUN mkdir -p /app/.data /runtime \
    && ln -s /opt/evidence-embedding /app/.data/evidence-embedding \
    && ln -s /runtime/config /app/config
EXPOSE 8787
CMD ["drama-server", "-config", "/app/config/app.local.yaml", "-listen", "0.0.0.0:8787"]
