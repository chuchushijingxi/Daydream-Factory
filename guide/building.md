# 源码构建、字体与贡献

[返回首页](../README.md) · [部署](deployment.md) · [排障](troubleshooting.md)

## 免费检查与构建

以下用于编译和免费检查，不是宿主机完整运行教程；完整启动执行 `sh deploy.sh`。需要 Go 1.26.10+、Node.js 22+ 和 Python 3.9+（字体获取与部署脚本检查）；本地检索由独立检索服务容器中的 Python CPU 环境提供。在源码根目录执行：

```sh
python3 -m unittest discover -s deploy -p 'test_*.py'
go test ./...
go vet ./...
go build ./cmd/...
npm --prefix frontend ci
npm --prefix frontend run font:fetch
python3 -m unittest discover -s scripts -p 'test_fetch_misans.py'
npm --prefix frontend test
npm --prefix frontend run build
```

前端构建输出到 `demo/app`，由 Go 同源提供。`npm --prefix frontend run dev` 为构建监听。需要真实数据库、队列或本地模型的集成检查默认跳过，普通测试使用免费夹具。

## 字体

MiSans 使用小米官方静态 WOFF2 分片，字体二进制不随仓库分发。`npm --prefix frontend run font:fetch` 按[固定清单](../frontend/public/fonts/misans-manifest.json)获取四档字重的原始资源并校验 SHA-256；Docker 构建自动完成获取。页面运行时仅请求同源字体，并按出现的字符加载分片。

已校验的本地文件可离线复用，也可在源码根目录导入已缓存的官方分片目录。把下面的示例路径替换为实际缓存目录：

```sh
python3 scripts/fetch-misans.py --source-dir /absolute/path/to/misans-cache
```

保留原始资源和 SHA-256 校验，不自行替换下载源、修改字体内容或绕过校验。独立字体许可和声明见[字体目录](../frontend/public/fonts/)。

## 反馈与贡献

欢迎通过仓库 Issues 提交可复现的问题或功能建议，说明使用版本、平台、操作步骤、预期结果和脱敏后的错误信息。提交改动前运行相关免费检查；不要附带真实凭据、业务数据、收费调用证据或本地开发资料。涉及安全问题时，不要在公开 Issue 中披露可被利用的细节或密钥。

## 第三方许可

项目代码采用 [Apache-2.0](../LICENSE)。MiSans 字体、供应商图标、本地 BGE 模型和独立媒体工具保留各自许可，见 [NOTICE](../NOTICE)。FFmpeg/libx264 工具的 GPL 许可及对应源码随媒体服务镜像保留在 `/opt/media/`。Redis 8 选择独立的 AGPLv3 许可，Silo 使用独立 AGPL 许可。
