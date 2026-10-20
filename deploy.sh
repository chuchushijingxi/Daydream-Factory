#!/bin/sh
set -eu

project_dir=$(CDPATH= cd -P "$(dirname "$0")" && pwd)
cd "$project_dir"

fail() {
    printf '%s\n' "$1" >&2
    printf '%s\n' '部署说明：guide/deployment.md' >&2
    exit 1
}

if ! command -v docker >/dev/null 2>&1; then
    fail '未找到 Docker。请先安装并启动 Docker，再运行 sh deploy.sh。'
fi

if ! compose_version=$(docker compose version --short 2>/dev/null); then
    fail 'Docker Compose 插件不可用。请安装 Docker Compose 2.20 或更新版本。'
fi

# Docker Desktop 常在版本后附加 -desktop.1；只比较已核验的数字部分。
compose_version=${compose_version#v}
case "$compose_version" in
    ''|*[!0-9A-Za-z.+-]*|*[-+]) fail '无法识别 Docker Compose 版本，请使用 2.20 或更新版本。' ;;
esac
version_numbers=${compose_version%%[-+]*}
major=${version_numbers%%.*}
version_tail=${version_numbers#*.}
minor=${version_tail%%.*}
patch=${version_tail#*.}
case "$major:$minor:$patch" in
    *[!0-9:]*|:*|*::*|*:) fail '无法识别 Docker Compose 版本，请使用 2.20 或更新版本。' ;;
esac
if [ "$version_numbers" != "$major.$minor.$patch" ]; then
    fail '无法识别 Docker Compose 版本，请使用 2.20 或更新版本。'
fi
if ! { [ "$major" -gt 2 ] 2>/dev/null ||
    { [ "$major" -eq 2 ] 2>/dev/null && [ "$minor" -ge 20 ] 2>/dev/null; }; }; then
    fail 'Docker Compose 版本过低。请使用 2.20 或更新版本。'
fi

if ! docker info >/dev/null 2>&1; then
    fail '无法连接 Docker。请启动 Docker，并确认当前用户有访问权限。'
fi

printf '%s\n' '正在构建并启动系统；首次部署需要联网下载依赖、字体和模型，请耐心等待。'
if docker compose "$@" up --build --force-recreate -d --wait --wait-timeout 600; then
    printf '\n%s\n' '部署成功。'
    printf '%s\n' '工作台：http://127.0.0.1:8787/demo/app/' '运营后台：http://127.0.0.1:8787/demo/app/#/operations-login'
    printf '%s\n' '首次空库初始化账号：Daydream，密码：Daydream123456。登录后请修改密码。'
    printf '%s\n' '已有数据时沿用现有账号和密码。首次使用见 guide/deployment.md。'
else
    deploy_status=$?
    printf '%s\n' '启动失败。请在项目目录运行 docker compose logs --tail 100 查看日志；自定义 Compose 参数需一并沿用。' >&2
    printf '%s\n' '排查说明：guide/troubleshooting.md' >&2
    exit "$deploy_status"
fi
