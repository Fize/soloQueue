<p align="center">
  <img src="web/public/logo.png" alt="SoloQueue Logo" width="128">
</p>

<h1 align="center">SoloQueue</h1>

<p align="center">
  一个本地运行的个人 AI 工作台，用于持续对话、团队任务处理、
  定时任务和消息应用接入。
</p>

<p align="center">
  <a href="README.md">English</a> ·
  <a href="README.zh-CN.md">简体中文</a> ·
  <a href="docs/zh/README.md">文档</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25.8-00ADD8?style=flat-square&logo=go" alt="Go 1.25.8">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/Fize/soloQueue?style=flat-square" alt="MIT License"></a>
</p>

## SoloQueue 的功能

| 功能 | 说明 |
| --- | --- |
| 对话 | 服务重启后保留对话历史，并实时显示回复和工具执行过程 |
| 项目任务 | 以选定项目作为文件和命令的工作目录，并可访问 Web 资源 |
| 团队协作 | 将任务交给可配置的团队处理，并把结果返回当前对话 |
| 模型选择 | 根据请求类型选择已配置的模型 |
| 定时任务 | 运行一次性或周期性任务，并保存执行历史 |
| 消息渠道 | 连接 QQ Bot、微信 iLink 和 Telegram 账户 |
| 浏览器界面 | 提供用于操作的 Web Console 和查看运行状态的只读页面 |

## 构建与运行

### 环境要求

- Go 1.25.8
- Node.js 和 `pnpm`
- Git
- 至少一个已启用 LLM Provider 的 API Key

### 从源码构建

```bash
git clone https://github.com/Fize/soloQueue.git
cd soloQueue

make build
export DEEPSEEK_API_KEY="your-api-key"
./soloqueue start
```

打开 <http://127.0.0.1:57689>。首次启动时，SoloQueue 创建工作目录和初始
`settings.yaml`。

### Docker

独立镜像的构建与 `docker run` 示例见 [Docker 部署](deploy/docker/README.md)。

### 初始配置

1. 打开 **Settings → Models**，配置已启用的 Provider 和 Model。
2. 打开 **Settings → Projects**，使用绝对路径注册代码仓库。
3. 打开 **Chat**，选择项目并提交 Prompt。
4. 按需在 Web Console 对应页面配置 Team、Cron 任务或渠道账户。

## 命令

```bash
./soloqueue start   # 启动 SoloQueue
./soloqueue --help  # 查看命令和参数
```

## Skills

SoloQueue 从 `${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}/skills/` 加载全局
Skills，并从 `<project>/.claude/skills/` 加载兼容的项目 Skills。Skill
目录或受支持的入口文件变化时，全局 `SKILL.md` 定义会重新加载。

全局 Skills 的安装和更新使用独立的
[ClawHub](https://github.com/openclaw/clawhub) CLI：

```bash
SOLOQUEUE_HOME="${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}"
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills search "calendar"
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills install @owner/slug
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills update --all
```

发现规则和生命周期命令见[功能说明](docs/zh/features.md)和
[参考手册](docs/zh/reference.md)。

## 文档

| 主题 | 中文 | English |
| --- | --- | --- |
| 构建、配置与故障排查 | [快速入门](docs/zh/getting-started.md) | [Getting Started](docs/getting-started.md) |
| 运行时功能 | [功能](docs/zh/features.md) | [Features](docs/features.md) |
| 进程与子系统边界 | [架构与设计](docs/zh/architecture.md) | [Architecture](docs/architecture.md) |
| 配置、CLI、存储与备份 | [参考手册](docs/zh/reference.md) | [Reference](docs/reference.md) |

## 运行边界

- 服务默认绑定 `127.0.0.1`。
- SoloQueue 不提供应用层 HTTP 认证、TLS 终止或公网监听。远程访问时，这些功能由用户配置的入口提供。
- SoloQueue 不创建执行沙箱。直接在宿主机部署时，工具继承 SoloQueue 进程的权限；已配置的容器或 VM 构成隔离边界。

## 开发

```bash
# 后端与 Status UI
go run ./cmd/soloqueue serve --port 8765 --verbose

# Web Console 开发服务
cd web
pnpm install
pnpm dev
```

Web Console 开发服务将 `/api` 和 `/ws` 代理到端口 `8765`。

### 验证

```bash
go test ./...
cd web && pnpm test && pnpm build
cd status-ui && pnpm test && pnpm build
```

这些命令验证当前检出的源码，不会重新构建或测试已安装的桌面应用。

## 许可证

[MIT](LICENSE)
