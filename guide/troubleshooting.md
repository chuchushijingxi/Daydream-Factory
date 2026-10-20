# 常见问题与排障

[返回首页](../README.md) · [部署](deployment.md) · [运行与备份](maintenance.md)

先在正确版本的源码根目录执行；自定义部署继续带上原有 `-p` / `-f` 参数：

```sh
docker compose version
docker compose ps -a
docker compose logs --tail=100 config-init database-init app postgres redis minio nsq milvus
# 仅在启用了额外 worker 时查看。
docker compose --profile workers logs --tail=100 task-worker
```

| 现象 | 先检查什么 |
| --- | --- |
| 提示缺少 Docker、Compose 或无法连接 Docker | 安装 Docker Engine/Desktop 与 Compose v2.20+ 并启动 Docker 服务，Linux 用户确认当前账号有 Docker 使用权限 |
| 端口或容器名冲突 | 本机 `8787` / `55432` 是否已占用、是否已有另一套同名容器。核对归属后决定使用哪套，不自动停止无关进程 |
| `config-init` 失败 | 原 `runtime-config` 卷是否完整、原文件权限是否私有。已有数据必须找回配套原凭据，不能删除配置卷重新生成 |
| `database-init` 失败 | 查看该服务及 PostgreSQL 日志，确认数据库与原凭据一致；样例密码不能替代实际密码，不要为排错删卷 |
| 构建下载、字体或权重校验失败 | 检查网络、磁盘及日志中的固定下载源。修复后重试，保留 SHA-256 校验；字体可按[源码构建指南](building.md#字体)导入已校验的离线缓存 |
| 页面没有可选分析模型 | 模型是否已启用、连接密钥和容量是否有效、计价配置是否保存；项目是否选择了该分析模型 |
| 提示价格未配置或积分不足 | 检查站内积分价格、账号余额和限免有效期；厂商账户余额独立检查 |
| 真实图片/视频模型不可用 | 当前只接入本地 Mock；给真实媒体厂商预设填 Key 也不会使其变为可执行任务 |
| 任务一直排队或检索不可用 | 查看 app、NSQ、Milvus 及额外 worker 日志，确认更新时一起重建了共享网络的服务 |

可从当前 app 的网络中检查依赖：

```sh
docker compose exec app python -c 'import socket; s=socket.create_connection(("127.0.0.1",4150),5); s.close(); print("NSQ TCP connected")'
docker compose exec app curl -fsS http://127.0.0.1:4151/ping
docker compose exec app curl -fsS http://127.0.0.1:9091/healthz
```

分享故障信息前先脱敏，不上传私有 YAML、完整备份、API Key、密码、邮箱、剧本或业务数据。避免公开粘贴完整容器环境、命令行参数和未经检查的日志。
