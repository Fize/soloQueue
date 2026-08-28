# SoloQueue

**本地优先的个人 AI Agent Harness 与多智能体工作台。**

[English](README.md) | 简体中文

我在学习和实践 Harness Engineering 的过程中构建了 SoloQueue，也参考了
[OpenClaw](https://github.com/openclaw/openclaw) 的一些实践。现在我把它作为
一个完整的软件在日常使用，用来探索任务路由、委派、工具、Skills、Memory、
定时任务、消息渠道和运行观测如何协同工作。

我目前主要面向希望学习或运行自托管 Agent Harness 的开发者分享 SoloQueue。
我仍然把它作为持续演进的个人项目，不把它定位为企业级生产平台、多租户 SaaS
或 OpenClaw 的兼容实现。

## 核心功能

- 使用本地优先的运行时维护长期 Agent 会话。
- 使用团队、Agent 模板、委派和工具确认构建多智能体工作台。
- 支持通过任务路由、Memory、Skills、MCP/LSP、定时任务和消息渠道进行
  Harness Engineering 实验。
- 提供完整的浏览器 Web Console 和独立的嵌入式只读状态页。

## 从源码快速开始

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

## 常用命令

~~~bash
./soloqueue version
./soloqueue --help
./soloqueue skills report
./soloqueue memory audit
./soloqueue memory cleanup
./soloqueue wechat login --id personal
~~~

## 文档

建议从[中文文档中心](docs/zh/README.md)开始：

- [快速入门](docs/zh/getting-started.md) · [English](docs/getting-started.md)
- [核心功能](docs/zh/features.md) · [English](docs/features.md)
- [架构与设计](docs/zh/architecture.md) · [English](docs/architecture.md)
- [参考手册](docs/zh/reference.md) · [English](docs/reference.md)


## 许可证

SoloQueue 使用 [MIT License](LICENSE) 发布。
