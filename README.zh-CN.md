<p align="center">
  <img src="web/public/logo.png" alt="SoloQueue Logo" width="128">
</p>

<h1 align="center">SoloQueue</h1>

<p align="center">
  <a href="README.md">English</a> ·
  <a href="README.zh-CN.md">简体中文</a> ·
  <a href="#文档">文档</a>
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/Fize/soloQueue?style=flat-square" alt="MIT License"></a>
</p>

SoloQueue 是一个开源、本地优先的个人 AI 工作台。你可以围绕项目与它持续对话、推进任务，并借助文件、命令和外部工具完成实际工作；还可以通过 Skills 扩展能力、积累长期记忆，或组织多个 AI Agent 协同处理复杂任务。

SoloQueue 的灵感来自 OpenClaw，也源于我对多 Agent 编排与协作的探索。在学习和实践了多种 Agent Harness 方案后，我使用 Go 构建了 SoloQueue 的核心，并针对 DeepSeek 进行了优化，希望在执行性能、使用成本与日常体验之间取得平衡。这也是一次持续迭代的 Agent Harness 工程实践。

SoloQueue 已在我的个人电脑上长期运行。它轻量、易于部署和扩展；接入兼容的模型服务后，可运行在个人电脑或服务器上，并通过 Web、QQ、微信、Telegram 或企业微信发起任务、查看进度和接收结果。

## 快速开始

源码构建需要 Go 1.25.8、支持 CGO 的 C 编译器、Node.js、`pnpm` 和 Make：

```bash
git clone https://github.com/Fize/soloQueue.git
cd soloQueue

make build
./soloqueue start
```

Windows x64 可在安装 Go、Node.js 和 pnpm，并将 MinGW-w64 C 编译器加入 `PATH` 后，通过 PowerShell 构建：

```powershell
.\scripts\build-windows.ps1
.\soloqueue.exe start
```

发布工作流构建的 Windows 压缩包包含 `silk-decoder.exe`；语音依赖安装时需将它与 `soloqueue.exe` 放在同一目录。安装器会将固定版本的 Whisper、FFmpeg 和模型下载到 SoloQueue 工作目录。源码构建未附带解码器时，安装器会自动下载它。Windows 命令使用 PowerShell，包含 Unix Shell 语法的 Skill 需要提供 Windows 兼容版本。MCP/LSP 的批处理启动器会拒绝路径或参数中的 `"`、`&`、`|`、`<`、`>`、`^`、`%`、`!` 等 cmd.exe 特殊字符；需要这些字符时可使用原生可执行文件或直接调用 Node 的 JavaScript 入口。以反斜杠结尾的参数采用兼容 Node 包装脚本的转义方式；普通批处理通过 `%~1` 读取时，末尾反斜杠可能增加。

打开 <http://127.0.0.1:57689>，配置一个可用模型并添加项目目录，即可开始对话和处理任务。

其他模型、Docker 部署与故障排查请阅读[快速入门](docs/zh/getting-started.md)。

## 安全提示

SoloQueue 默认仅监听 `127.0.0.1`。工具会继承 SoloQueue 进程的系统权限，可以读取文件并执行命令；安全起见，强烈建议在容器或虚拟机中运行。

## 文档

- [快速入门](docs/zh/getting-started.md)
- [参考手册](docs/zh/reference.md)
- [架构](docs/zh/architecture.md)
- [任务路由](docs/zh/routing.md)
- [Agent 执行](docs/zh/agent.md)
- [上下文与记忆](docs/zh/context-and-memory.md)

## 许可证

[MIT](LICENSE)
