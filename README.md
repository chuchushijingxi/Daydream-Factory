# 白日梦工厂 · Daydream Factory

一个可自行部署的短剧创作平台：从剧本整理角色、地点和道具，到编写资产视觉提示词、分集分镜与视频提示词。

## 一键部署

安装并启动 Docker（Compose v2.20+），下载源码 ZIP 并解压，在源码根目录执行：

```sh
sh deploy.sh
```

脚本会自动构建、生成私有基础设施凭据、初始化空库并启动整套系统，无需手工建库、执行 SQL、安装 Go/Node.js 或准备字体。首次构建需要联网，资源要求见[部署指南](guide/deployment.md#部署前准备)。

- 工作台：<http://127.0.0.1:8787/demo/app/>
- 运营后台：<http://127.0.0.1:8787/demo/app/#/operations-login>
- 初始账号：`Daydream` / `Daydream123456`（两个入口通用）。**首次登录后请修改公开密码。**

默认仅供本机访问。启动后，在运营后台配置自己的分析模型 API Key、启用模型并设置积分价格，再到工作台选择模型、导入剧本并开始；完整顺序见[第一次使用](guide/deployment.md#第一次使用)。单人体验无需先配置邮件。

## 功能与界面

- **剧本与资产**：提取角色、地点、道具，编辑视觉描述与提示词；浏览、筛选和复用创作资产。
- **分集与分镜**：按集整理场景、镜头和带资产引用的视频提示词。
- **运营后台**：管理账号、模型连接、积分定价、使用记录及邮件配置。

文本分析需自备可用模型与凭据；**图片和视频任务目前仅支持本地免费 Mock，真实媒体厂商尚未接入。** 截图使用虚构演示数据展示实际界面，新部署不附带这些演示项目。

| 项目工作台 | 角色资产 |
| --- | --- |
| ![项目工作台](assets/screenshots/projects.jpg) | ![角色资产](assets/screenshots/asset-library.jpg) |
| **分镜创作** | **运营后台** |
| ![分镜创作](assets/screenshots/storyboard.jpg) | ![运营后台](assets/screenshots/operations.jpg) |

## 使用指南

- [部署与第一次使用](guide/deployment.md)
- [运行、更新与备份恢复](guide/maintenance.md)
- [常见问题与排障](guide/troubleshooting.md)
- [源码构建、字体与贡献](guide/building.md)

## 许可

项目代码采用 [Apache-2.0](LICENSE)；MiSans 字体和第三方组件保留各自许可，见 [NOTICE](NOTICE)。本仓库提供自部署源码，不提供托管服务。
