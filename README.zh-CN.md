# SoloQueue

SoloQueue 是一个本地优先、面向单用户的 AI Agent Harness，包含持久会话、
Team 委派、定时任务、消息渠道和浏览器界面。

[English](README.md) | 简体中文

运行时在一个自托管进程中组合任务路由、委派、工具、Skills、Memory、Cron、
渠道适配器和运行状态检查。

## 分支边界

`main` 不包含模拟功能。`experimental/simulation` 保留了剥离模拟功能前
`6efd796` 对应的仓库状态。

已有的模拟数据库文件（`simulation.db` 及其可能存在的 `-wal` / `-shm` 附属文件）会保留，`main` 不会打开或初始化它们。在 `experimental/simulation` 上实验时，请使用独立工作目录，避免配置保存时重写 `main` 所用目录中的设置。例如，构建该分支后运行：

~~~bash
SOLOQUEUE_WORK_DIR="$HOME/.soloqueue-simulation" ./soloqueue start
~~~

## 功能

- 使用本地优先的运行时维护长期 Agent 会话。
- 使用团队、Agent 模板和委派构建多智能体工作台。
- 支持任务路由、Memory、Skills、MCP/LSP、定时任务和消息渠道。
- 提供浏览器 Web Console 和独立的嵌入式只读状态页。

Skills 使用独立的 [ClawHub](https://github.com/openclaw/clawhub) 安装和更新。SoloQueue 加载 `${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}/skills/` 中已经存在的全局技能包，并在技能目录或受支持的入口文件变化时热加载其 `SKILL.md` 定义；项目 Agent 创建时，也会加载 `<project>/.claude/skills/` 中兼容的项目级技能。Web Console 仅提供只读查看。可以设置 `SOLOQUEUE_WORK_DIR` 使用其他工作目录。ClawHub 的所有者限定资源使用 `@owner/slug`，卸载使用已安装技能的 slug。

~~~bash
SOLOQUEUE_HOME="${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}"
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills search "calendar"
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills inspect @owner/slug
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills install @owner/slug
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills update @owner/slug
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills update --all
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills uninstall slug
~~~

## 范围边界

SoloQueue 不实现多租户账户、应用层 HTTP 认证、TLS 终止、公网监听或
OpenClaw 兼容。SoloQueue 不创建执行沙箱；直接在宿主机部署时，工具继承
SoloQueue 进程的权限。

## 从源码构建

### 前置条件

- Go 1.25.8 或兼容的 1.25 版本。
- Node.js 和 pnpm。
- 至少一个已启用 LLM Provider 的 API Key。

### 构建并运行嵌入式浏览器应用

~~~bash
git clone https://github.com/Fize/soloQueue.git
cd soloQueue

make build
export DEEPSEEK_API_KEY="your-api-key"
./soloqueue start
~~~

打开 http://127.0.0.1:57647。第一次启动时，SoloQueue 会在 ~/.soloqueue/ 下创建工作
目录和 settings.yaml。

### 开发浏览器前端

在两个终端中分别运行后端和 Web Console：

~~~bash
# 终端 1
go run ./cmd/soloqueue serve --port 8765 --verbose

# 终端 2：Web Console
cd web
pnpm install
pnpm dev
~~~

只读状态页可以在 `status-ui/` 中独立开发。`soloqueue serve` 默认提供状态页，
`soloqueue web` 只启动 Web Console，`soloqueue start` 在一个端口同时提供两者。

SoloQueue 服务绑定 `127.0.0.1`，不提供应用层 HTTP 认证。远程访问由用户配置的
反向代理提供，认证、TLS、CORS 和 WebSocket 代理也由该入口处理。

## 命令

~~~bash
./soloqueue version
./soloqueue --help
./soloqueue skills report
./soloqueue memory audit
./soloqueue memory cleanup              # 仅生成清理计划
./soloqueue memory cleanup --apply      # 备份后执行清理计划
./soloqueue wechat login --id personal
~~~

## 文档

文档入口为[中文文档中心](docs/zh/README.md)：

- [快速入门](docs/zh/getting-started.md) · [English](docs/getting-started.md)
- [功能](docs/zh/features.md) · [English](docs/features.md)
- [架构与设计](docs/zh/architecture.md) · [English](docs/architecture.md)
- [参考手册](docs/zh/reference.md) · [English](docs/reference.md)

## 测试

~~~bash
go test ./...
cd web && pnpm test && pnpm build
cd status-ui && pnpm test && pnpm build
~~~

这些命令验证当前检出的源码，不会重新构建或测试已安装的桌面应用。


## 许可证

SoloQueue 使用 [MIT License](LICENSE) 发布。
