# 后端镜像：只含 Go 程序（API、任务 worker 与初始化工具）。
# 媒体工具见 deploy/Dockerfile.media，检索模型见 deploy/Dockerfile.embedding，工作台页面见 deploy/Dockerfile.frontend。
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
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/drama-server ./cmd/server \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/drama-task-worker ./cmd/task-worker \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/drama-accounts-init ./cmd/accounts-init \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/drama-compose-init ./cmd/compose-init \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/drama-smtp-import ./cmd/smtp-import

FROM debian:trixie-slim@sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f
RUN apt-get update \
    && apt-get upgrade -y \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=backend /out/ /usr/local/bin/
# 风格目录 catalog.json 等运行时数据由后端读取；页面文件只在前端镜像中。
COPY demo/assets/ ./demo/assets/
COPY LICENSE NOTICE /usr/share/licenses/drama-agent/
COPY deploy/licenses/go-dependencies.txt /usr/share/licenses/drama-agent/third-party/
RUN mkdir -p /app/.data /runtime \
    && ln -s /runtime/config /app/config
EXPOSE 8787
CMD ["drama-server", "-config", "/app/config/app.local.yaml", "-listen", "0.0.0.0:8787", \
     "-media-endpoint", "http://media:8790", "-embedding-endpoint", "http://embedding:8791"]
