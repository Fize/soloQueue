<p align="center">
  <img src="web/public/logo.png" alt="SoloQueue Logo" width="128">
</p>

<h1 align="center">SoloQueue</h1>

<p align="center">
  <a href="README.md">English</a> ·
  <a href="README.zh-CN.md">简体中文</a> ·
  <a href="docs/zh/README.md">文档</a>
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/Fize/soloQueue?style=flat-square" alt="MIT License"></a>
</p>

SoloQueue 是一个开源、本地优先的个人 AI 工作台。你可以围绕项目与它持续对话、推进任务，并借助文件、命令和外部工具完成实际工作；还可以通过 Skills 扩展能力、积累长期记忆，或组织多个 AI Agent 协同处理复杂任务。

SoloQueue 的灵感来自 OpenClaw，也源于我对多 Agent 编排与协作的探索。在学习和实践了多种 Agent Harness 方案后，我使用 Go 构建了 SoloQueue 的核心，并针对 DeepSeek 进行了优化，希望在执行性能、使用成本与日常体验之间取得平衡。这也是一次持续迭代的 Agent Harness 工程实践。

SoloQueue 已在我的个人电脑上长期运行。它轻量、易于部署和扩展；接入兼容的模型服务后，可运行在个人电脑或服务器上，并通过 Web、QQ、微信或 Telegram 发起任务、查看进度和接收结果。

## 快速开始

准备好 Go 1.25、Node.js、`pnpm`、Git 和 DeepSeek API Key：

```bash
git clone https://github.com/Fize/soloQueue.git
cd soloQueue

make build
./soloqueue start
```

打开 <http://127.0.0.1:57689>，配置一个可用模型并添加项目目录，即可开始对话和处理任务。

其他模型、Docker 部署与故障排查请阅读[快速入门](docs/zh/getting-started.md)。

## 安全提示

SoloQueue 默认仅监听 `127.0.0.1`。工具会继承 SoloQueue 进程的系统权限，可以读取文件并执行命令；安全起见，强烈建议在容器或虚拟机中运行。

## 文档

- [快速入门](docs/zh/getting-started.md)：完成安装、配置与首次使用
- [参考手册](docs/zh/reference.md)：查阅配置、命令与数据管理方式

## 许可证

[MIT](LICENSE)
