# 快速入门

[English](../getting-started.md) | 简体中文

本指南涵盖环境前置条件、源码构建、首次运行配置、本地开发及常见故障排查。

---

## 前置条件

- **Go**：1.25.8 或兼容的 1.25 版本。
- **Node.js 与 pnpm**：已安装 Node.js 及 `pnpm`。
- **Git**。
- **API Key**：至少一个已启用的 LLM Provider API Key（如 `DEEPSEEK_API_KEY`）。

---

## 安装与构建

### 1. 嵌入式浏览器应用（默认模式）

构建 Web Console 和状态页、嵌入 Go 服务端并启动：

```bash
git clone https://github.com/Fize/soloQueue.git
cd soloQueue

make build
export DEEPSEEK_API_KEY="your-api-key"
./soloqueue start
```

打开 `http://127.0.0.1:57647`。首次启动时，SoloQueue 会自动在 `~/.soloqueue/` 下创建工作目录并生成初始 `settings.yaml`。

> **提示**：生产构建前请运行 `make build-assets`，以嵌入 Web Console 和状态页。

### 2. 浏览器开发

在独立终端中分别运行 Web Console 与后端：

```bash
# 终端 1：后端服务
go run ./cmd/soloqueue serve --port 8765 --verbose

# 终端 2：Web Console
cd web
pnpm install
pnpm dev
```

Vite 开发服务器会自动把 `/api` 与 `/ws` 转发至 `http://localhost:8765`。

### 3. 构建目标说明

| 命令 | 产物描述 |
| --- | --- |
| `make build-web` | 构建完整 Web Console |
| `make build-go` | 构建 Go 二进制（要求浏览器资源已存在） |
| `make build` | 构建浏览器资源及 Go 二进制 |
| `make build-status` | 构建只读状态页 |
| `make build-assets` | 构建 Web Console、状态页并嵌入 Skills |
| `make start` | 构建并启动后端与两个浏览器前端 |

---

## 首次运行与基本工作流

### 1. 模型 Provider 配置
在 UI 中打开 **Settings → Models**，确认 Provider 与 Model 已启用。默认配置使用 DeepSeek 并从环境变量 `DEEPSEEK_API_KEY` 读取 Key。路由映射格式为 `provider:model`（如 `deepseek:deepseek-v4-flash-thinking`）。

### 2. 注册项目
在 UI 中打开 **Settings → Projects**，使用绝对路径添加已有代码库并命名。项目路径决定了工具调用的允许执行范围。

### 3. 创建会话
打开 **Chat**，选择已注册的项目，发送提示词：
```text
检查 README.md 并列出构建命令，不要修改文件。
```

### 4. 工具确认机制
当工具调用匹配确认策略（如命令执行或文件修改）时，SoloQueue 会暂停运行并在界面展示确认卡片。检查请求范围和命令后点击允许。可以使用 `--bypass` 标志全局绕过确认，但仅建议在受控测试环境中使用。

---

## 服务边界

SoloQueue 只绑定 `127.0.0.1`，不提供 HTTP 认证、TLS 或公网监听能力。
本地 Web Console 与状态页使用内置同源路由；Vite 和独立 Web Console
开发模式通过受限的回环 CORS 访问后端。

如果需要远程访问，请使用 nginx 或其他部署入口代理 Web Console、REST
API、WebSocket 和状态页。外部认证、TLS、CORS、限流和访问日志都由该
入口负责。`deploy/docker-demo/` 仅展示本地 nginx 拓扑，不是生产部署模板。

---

## 故障排查

- **Web 界面空白**：依次运行 `make build-assets` 与 `make build` 后重启服务。
- **端口被占用**：使用 `--port` 参数指定新端口（如 `./soloqueue serve --port 8765`）。
- **模型无响应**：检查环境变量 API Key、核对 `model_routes` 配置，并查看服务端日志。
- **远程访问**：配置外部反向代理，并保持 SoloQueue 监听回环地址。
- **工具操作被阻断**：阅读卡片提示，或在 `settings.yaml` 的 `tools` 区段调整 Shell/路径策略。
