# 运行、更新与备份恢复

[返回首页](../README.md) · [部署](deployment.md) · [排障](troubleshooting.md)

下列命令均在源码根目录执行。

## 运行与更新

```sh
docker compose ps -a
docker compose logs --tail=100 frontend backend media embedding nsq milvus etcd
# 停止整套环境（包括已启用的额外 worker），保留数据卷。
docker compose --profile workers down
```

更新前先完成下节的冷备，保留对应源码版本和镜像记录。获取要升级的源码版本：使用 Git 的部署先切换到已确认的发布版本；使用 ZIP 的部署先解压新版，再把自行维护的 Compose 覆盖文件带入新版根目录。确认版本与配置后，才在新版根目录重建：

```sh
sh deploy.sh
# 原先启用了额外 worker 时，使用这条替代上面的命令。
sh deploy.sh --profile workers
```

NSQ 和额外 worker 共用 backend 的网络命名空间，必须一起重建，不能只重建 backend。Milvus 与其元数据 etcd 独立运行，重建 backend 不会重启它们。曾使用 `-p` 项目名、`-f` 覆盖文件或固定镜像的部署，启动、备份、更新和恢复须沿用同一组参数，避免意外启动另一个空环境。健康检查通过不等于模型调用或业务结果已经验证。

已有 MinIO 或 Redis 7 部署应先保留旧卷，并在隔离副本验证升级，不能直接让新镜像写真实旧卷。Milvus 2.x 至 3.x 使用新索引卷，从 PostgreSQL 重建派生索引；改用独立 etcd 后，Milvus 使用新的 `milvus-index` 与 `milvus-etcd` 卷并自动重建索引，旧的 `milvus-data` 卷不再使用，确认无需回退后可自行删除。升级后的数据库迁移不保证能直接降级；回退使用配套的旧源码、镜像及完整备份恢复到新卷。

## 备份与恢复

下面提供默认 Compose 配置的**整套离线冷备**。它会停机，保存八个卷，保留数字 UID、权限和符号链接。`runtime-config` 保存数据库、Redis、S3 原凭据，`app-data` 保存模型/SMTP 原加密密钥；仅有 PostgreSQL 备份无法恢复加密配置和对象。NSQ、Redis 的持久状态及可重建的 Milvus 索引也一起保存。

备份包含私有数据和密钥，应加密保存到可信位置；`backups/` 已忽略提交。以下命令整体交给 `sh` 执行（在 macOS 默认的 zsh 中直接粘贴也按 POSIX 规则拆分卷名），在源码根目录执行。默认项目是 `daydream-factory`；使用过 `-p`、`-f` 或自定义卷名时，先修改 `project`、`dc` 和卷映射，不能直接照抄默认卷名。

### 冷备

确认没有其他程序写入这些卷，并留出与数据量相符的备份空间。命令先核验八卷均存在，停止包括可选 worker 在内的全部服务，再检查没有容器继续使用它们；失败后保持停机，先解决问题。

```sh
sh <<'EOF'
set -eu
umask 077
project=daydream-factory
dc() { docker compose -p "$project" -f "$PWD/docker-compose.yml" "$@"; }
volumes='runtime-config postgres-data objects-data app-data nsq-data milvus-index milvus-etcd redis-data'
backup_image=$(docker image inspect --format '{{.Id}}' daydream-backend:local)
for volume in $volumes; do
  docker volume inspect "${project}_${volume}" >/dev/null
done
dc --profile workers stop
for volume in $volumes; do
  test -z "$(docker ps -q --filter "volume=${project}_${volume}")"
done
mkdir -p "$PWD/backups"
backup="$PWD/backups/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir "$backup"
printf '%s\n' "$backup_image" > "$backup/tar-image-id.txt"
dc config > "$backup/compose.resolved.yaml"
dc images > "$backup/images.txt"
for volume in $volumes; do
  docker run --rm --network none --read-only --user 0 \
    --mount "type=volume,src=${project}_${volume},dst=/data,readonly" \
    --mount "type=bind,src=$backup,dst=/backup" --entrypoint tar "$backup_image" \
    --numeric-owner -czpf "/backup/$volume.tar.gz" -C /data .
done
docker run --rm --network none --read-only \
  --mount "type=bind,src=$backup,dst=/backup" --entrypoint sh "$backup_image" \
  -ec 'cd /backup; sha256sum ./*.tar.gz > SHA256SUMS'
printf '备份完成：%s\n' "$backup"
EOF
```

把备份、对应源码和必要的私有覆盖文件另存到可靠位置。若要继续原部署，执行 `docker compose up --no-build --pull never -d --wait --wait-timeout 600`；原先启用了额外 worker 时也加 `--profile workers`，随后检查全部服务健康。

### 恢复到新空卷

先准备备份对应版本的源码和镜像，保持 PostgreSQL、Redis、Milvus 版本及容器架构一致；本方案不承诺跨版本或跨架构的数据目录迁移。下面只创建全新的目标卷，校验后恢复全部八卷，**不会启动服务或覆盖原卷**。修改绝对备份路径和新的项目名；指定名字的任一目标卷已存在时立即退出，不向其中解包。若在另一台机器恢复，先准备本地可用的对应应用镜像，并将 `backup_image` 改为它的镜像 ID。

```sh
sh <<'EOF'
set -eu
backup=/absolute/path/to/backups/20261020T120000Z
restore_project=daydream-restored-01
volumes='runtime-config postgres-data objects-data app-data nsq-data milvus-index milvus-etcd redis-data'
backup_image=$(cat "$backup/tar-image-id.txt")
docker image inspect "$backup_image" >/dev/null
for volume in $volumes; do
  test -f "$backup/$volume.tar.gz"
  if docker volume inspect "${restore_project}_${volume}" >/dev/null 2>&1; then
    printf '目标卷已存在，停止恢复：%s\n' "${restore_project}_${volume}" >&2
    exit 1
  fi
done
docker run --rm --network none --read-only \
  --mount "type=bind,src=$backup,dst=/backup,readonly" --entrypoint sh "$backup_image" \
  -ec 'cd /backup; sha256sum -c SHA256SUMS'
for volume in $volumes; do
  docker volume create --label "com.docker.compose.project=$restore_project" \
    --label "com.docker.compose.volume=$volume" "${restore_project}_${volume}" >/dev/null
  docker run --rm --network none --read-only --user 0 \
    --mount "type=volume,src=${restore_project}_${volume},dst=/data" \
    --mount "type=bind,src=$backup,dst=/backup,readonly" --entrypoint sh "$backup_image" \
    -ec 'test -z "$(find /data -mindepth 1 -maxdepth 1 -print -quit)"; tar --numeric-owner -xzpf "/backup/$1.tar.gz" -C /data' \
    sh "$volume"
done
printf '恢复完成，待核对并启动的项目：%s\n' "$restore_project"
EOF
```

恢复不需要重新生成密钥。保持原始配置、对象和数据库配套，确认八卷、镜像版本与覆盖配置对应后再启动。默认固定容器名和端口不支持同机并行：如要在同机切换，先在**原部署目录**执行原参数的 `docker compose --profile workers down`（不带 `-v`，原卷保留），再在对应版本目录运行 `docker compose -p daydream-restored-01 up --no-build --pull never -d --wait --wait-timeout 600`；原先启用了额外 worker 时也加 `--profile workers`。使用自定义配置时，同步核对配置内数据库和 S3 地址。

恢复后核对登录、项目、上传对象、模型配置能否解密及原任务状态；只看到健康探针成功不能代替这些核对。不要用 `down -v`、清空密钥卷或重新导入初始化数据来修复已有部署。
