# 快速入门

[English](../getting-started.md) | 简体中文

本指南涵盖环境前置条件、源码构建、首次运行配置、远程访问设置及常见故障排查。

---

## 前置条件

- **Go**：1.25.8 或兼容的 1.25 版本。
- **Node.js 与 pnpm**：已安装 Node.js 及 `pnpm`。
- **Git**。
- **API Key**：至少一个已启用的 LLM Provider API Key（如 `DEEPSEEK_API_KEY`）。

---

## 安装与构建

### 1. 嵌入式 Portal 与服务端（默认模式）

构建 Web Portal、嵌入 Go 服务端并启动：

```bash
git clone https://github.com/Fize/soloQueue.git
cd soloQueue

make build
export DEEPSEEK_API_KEY="your-api-key"
./soloqueue serve
```

打开 `http://127.0.0.1:57647`。首次启动时，SoloQueue 会自动在 `~/.soloqueue/` 下创建工作目录并生成初始 `settings.yaml`。

> **提示**：未运行 `make build-web` 直接使用 `go run ./cmd/soloqueue serve` 启动会导致浏览器 UI 为空白。

### 2. 桌面开发客户端

在独立终端中分别运行 Electron 桌面前端与后端：

```bash
# 终端 1：后端服务
go run ./cmd/soloqueue serve --port 8765 --verbose

# 终端 2：桌面客户端
cd desktop
pnpm approve-builds
pnpm install
pnpm dev
```

Vite 开发服务器会自动把 `/api` 与 `/ws` 转发至 `http://localhost:8765`。

### 3. 构建目标说明

| 命令 | 产物描述 |
| --- | --- |
| `make build-web` | 构建 Web Portal 并复制到 `internal/server/dist/` |
| `make build-go` | 构建 Go 二进制（要求 Portal 资源已存在） |
| `make build` | 构建 Web Portal 及 Go 二进制 |
| `make build-desktop` | 构建 Electron 桌面渲染资源 |
| `make build-all` | 构建 Portal、Go 二进制及桌面渲染资源 |
| `make package-desktop PLATFORM=mac` | 打包桌面客户端（支持 `mac`、`win` 或 `linux`） |

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

## 远程访问与安全

默认情况下，SoloQueue 仅绑定 `127.0.0.1:57647`，回环请求自动绕过认证。

### 监听非回环网卡

在外部网卡监听服务：

```bash
./soloqueue serve --host 0.0.0.0 --port 57647
```

### 配置 HTTP 认证

在 `settings.yaml` 中配置 Basic Auth 凭据：

```yaml
auth:
  user: soloqueue
  password: replace-with-a-long-random-password
```

或者通过环境变量注入：

```bash
export SOLOQUEUE_AUTH_USER="soloqueue"
export SOLOQUEUE_AUTH_PASSWORD="replace-with-a-long-random-password"
```

配置凭据后，非回环请求必须通过 Basic Auth 认证。若未配置凭据且尝试远程访问，服务将返回 `403 Forbidden`。

---

## 故障排查

- **Portal 界面空白**：依次运行 `make build-web` 与 `make build` 后重启服务。
- **端口被占用**：使用 `--port` 参数指定新端口（如 `./soloqueue serve --port 8765`）。
- **模型无响应**：检查环境变量 API Key、核对 `model_routes` 配置，并查看服务端日志。
- **远程访问 403**：确认已在 `settings.yaml` 或环境变量中配置 `auth.user` 与 `auth.password`。
- **工具操作被阻断**：阅读卡片提示，或在 `settings.yaml` 的 `tools` 区段调整 Shell/路径策略。
