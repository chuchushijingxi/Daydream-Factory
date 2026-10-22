"""Initialize only this Compose project's private configuration volume."""
import os
from pathlib import Path
import secrets
import stat
import tempfile


def add_redis(target: Path):
    config = target / "app.local.yaml"
    raw = config.read_text()
    password_file = target / "redis-password"
    if "\nredis:" in "\n" + raw:
        info = password_file.lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077 or not info.st_size:
            raise ValueError("existing Redis secret is not private")
        return
    if password_file.exists():
        info = password_file.lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077:
            raise ValueError("Redis secret is not private")
        password = password_file.read_text()
        if len(password) != 48 or any(c not in "0123456789abcdef" for c in password):
            raise ValueError("incomplete Redis upgrade")
    else:
        password = secrets.token_hex(24)
        with open(password_file, "x", opener=lambda path, flags: os.open(path, flags, 0o600)) as output:
            output.write(password)
            output.flush()
            os.fsync(output.fileno())
    descriptor, temporary = tempfile.mkstemp(prefix=".redis-upgrade-", dir=target)
    with os.fdopen(descriptor, "w") as output:
        output.write(raw.rstrip() + f"\nredis:\n  address: redis:6379\n  password: {password}\n  namespace: daydream-factory:compose\n")
        output.flush()
        os.fsync(output.fileno())
    os.replace(temporary, config)


def initialize(root: Path):
    target = root / "config"
    names = ("app.local.yaml", "postgres-password", "minio-password", "minio-user")
    if target.exists():
        # 已有配置不能通过补造密码修复，否则会与已有 PG/S3 事实分离。
        for name in names:
            path = target / name
            info = path.lstat()
            if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077 or not info.st_size:
                raise ValueError("existing Compose configuration is incomplete or not private")
        add_redis(target)
        print("Existing PG/S3 secrets preserved; Redis configuration ready.", flush=True)
        return

    root.mkdir(parents=True, exist_ok=True)
    # 配置与基础设施凭证一起原子发布；中断目录不会被当作有效配置。
    staging = Path(tempfile.mkdtemp(prefix=".config-", dir=root))
    database_password, storage_password = secrets.token_hex(24), secrets.token_hex(24)
    # 开源模板默认可体验完整流程：演示收银台开启（不真实扣款）。只写入新部署的配置，已有部署不被改动。
    # 正式上线前把 environment 改为 production 并关闭 payment.demo，见 README「上线前」。
    config = f"""environment: development
payment:
  demo:
    enabled: true
database:
  url: postgres://drama:{database_password}@postgres:5432/drama_agent?sslmode=disable
nsq:
  address: 127.0.0.1:4150
  topic: drama.tasks
  channel: workers
storage:
  endpoint: http://minio:9000
  bucket: drama-agent
  accessKey: drama
  secretKey: {storage_password}
"""
    values = (config, database_password, storage_password, "drama")
    for name, value in zip(names, values):
        with open(staging / name, "x", opener=lambda path, flags: os.open(path, flags, 0o600)) as output:
            output.write(value)
            output.flush()
            os.fsync(output.fileno())
    staging.rename(target)
    add_redis(target)
    print("Private Compose configuration initialized; credentials are not logged.", flush=True)


if __name__ == "__main__":
    try:
        initialize(Path("/runtime"))
    except (OSError, ValueError):
        raise SystemExit("Cannot initialize Compose configuration; check or restore its original private volume.")
