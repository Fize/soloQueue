# SoloQueue

**本地优先的个人 AI Agent Harness 与多智能体工作台。**

[English README](README.md) · [中文文档中心](docs/zh/README.md) ·
[English documentation hub](docs/README.md)

我在学习和实践 Harness Engineering 的过程中构建了 SoloQueue，也参考了
[OpenClaw](https://github.com/openclaw/openclaw) 的一些实践。现在我把它作为
一个完整的软件在日常使用，用来探索任务路由、委派、工具、Skills、Memory、
Workflow、定时任务、消息渠道和运行观测如何协同工作。

我目前主要面向希望学习或运行自托管 Agent Harness 的开发者分享 SoloQueue。
我仍然把它作为持续演进的个人项目，不把它定位为企业级生产平台、多租户 SaaS
或 OpenClaw 的兼容实现。

## 我构建的内容

- 我使用本地优先的运行时维护长期 Agent 会话。
- 我使用团队、Agent 模板、委派和工具确认构建多智能体工作台。
- 我用任务路由、Workflow、Memory、Skills、MCP/LSP、定时任务和消息渠道进行
  Harness Engineering 实验。
- 我提供 Electron 桌面控制台和嵌入式浏览器 Portal。

## 从源码快速开始

### 前置条件

- Go 1.25.8 或兼容的 1.25 版本。
- Node.js 和 pnpm。
- 至少一个已启用 LLM Provider 的 API Key。

### 构建并运行嵌入式 Portal

~~~bash
git clone https://github.com/Fize/soloQueue.git
cd soloQueue

make build
export DEEPSEEK_API_KEY="your-api-key"
./soloqueue serve
~~~

我打开 http://127.0.0.1:57647。第一次启动时，我会在 ~/.soloqueue/ 下创建工作
目录和 settings.yaml。

### 运行桌面开发客户端

我在两个终端中分别运行后端和桌面客户端：

~~~bash
# 终端 1
go run ./cmd/soloqueue serve --port 8765 --verbose

# 终端 2
cd desktop
pnpm approve-builds
pnpm install
pnpm dev
~~~

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

我建议从[中文文档中心](docs/zh/README.md)开始；英文文档仍然是我的主要
维护入口：

- [安装与第一次运行](docs/zh/getting-started/installation.md) ·
  [English](docs/getting-started/installation.md)
- [第一个有用任务](docs/zh/getting-started/first-task.md) ·
  [English](docs/getting-started/first-task.md)
- [功能指南](docs/zh/guides/) · [English](docs/guides/)
- [运维与安全](docs/zh/operations/) · [English](docs/operations/)
- [配置与 CLI 参考](docs/zh/reference/) · [English](docs/reference/)
- [架构概览](docs/zh/architecture/overview.md) ·
  [English](docs/architecture/overview.md)

## 许可证

我使用 [MIT License](LICENSE) 发布 SoloQueue。
