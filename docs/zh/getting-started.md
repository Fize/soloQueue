# 快速入门

[English](../getting-started.md) | 简体中文

本指南涵盖源码构建、首次运行配置、本地开发及远程 Core 连接。

---

## 前置条件

从源码构建需要 Go 1.25.8、Node.js、`pnpm` 和 Make。API Key 可在首次启动后通过 Web Console 配置。

---

## 安装与构建

### 1. 嵌入式浏览器应用

构建 Web Console 和状态页、嵌入 Go 服务端并启动：

```bash
git clone https://github.com/Fize/soloQueue.git
cd soloQueue

make build
export DEEPSEEK_API_KEY="your-api-key"
./soloqueue start
```

打开 `http://127.0.0.1:57689`。首次启动时，SoloQueue 会自动在 `~/.soloqueue/` 下创建工作目录并生成初始 `settings.yaml`。

> **提示**：构建用于分发的 Go 二进制前运行 `make build-assets`，以嵌入 Web Console 和状态页。

### 2. Docker

独立镜像的构建与 `docker run` 示例见 [Docker 部署](../../deploy/docker/README.md)。

### 3. 浏览器开发

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

### 使用本地 Web Console 连接远程 Core

你可以在本机使用 Web Console，连接运行在另一台机器上的 Core。先启动本机独立 Web Console（`soloqueue web`，默认地址为 `http://127.0.0.1:57648`），再进入 **Settings → Connection**，选择 **Remote**，填写浏览器可访问的 Core 地址（如 `https://core.example.com`）并保存。连接选择保存在当前浏览器中；Core 必须已启动，且浏览器能够访问该地址。

如果远程 Core 只监听远程主机的回环地址，请先建立 SSH 隧道，再将 `http://127.0.0.1:57689` 填入 Remote URL。如果 Core 位于反向代理之后，请使用代理的 HTTPS 地址，并确保代理转发 `/api` 请求和 `/ws` WebSocket 流量。

Core 本身不提供身份认证。不要将其直接暴露到公网；请使用 SSH 隧道，或配置启用了 TLS 和身份认证的安全反向代理。手动建立 SSH 隧道时，将本机 `57689` 端口转发到 Core 主机的 `127.0.0.1:57689`。

### 4. 构建目标说明

| 命令 | 产物描述 |
| --- | --- |
| `make build-web` | 构建 Web Console |
| `make build-go` | 构建 Go 二进制（要求浏览器资源已存在） |
| `make build` | 构建浏览器资源及 Go 二进制 |
| `make build-status` | 构建只读状态页 |
| `make build-assets` | 构建 Web Console 和状态页 |
| `make start` | 构建并启动后端与两个浏览器前端 |

---

## 首次运行与工作流

### 管理 Skills

在 Web Console 中打开 **Skills** 页面即可查看已安装技能。SoloQueue 从 `${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}/skills/` 加载全局技能，也会加载 `<project>/.claude/skills/` 中兼容的项目级技能。Web Console 暂不支持安装和更新 Skill；ClawHub 操作方式见[参考手册](reference.md)。

### 1. 模型 Provider 配置
在 Web Console 中打开 **Settings → Models**，配置 Provider、API Key、Model 和任务路由。默认配置使用 DeepSeek，并从环境变量 `DEEPSEEK_API_KEY` 读取 Key。路由值格式为 `provider:model`。

### 2. 注册项目
在 UI 中打开 **Settings → Projects**，使用绝对路径添加已有代码库并命名。该路径会成为项目的默认工作目录，但不构成安全沙箱。

### 3. 创建会话
打开 **Chat**，选择已注册的项目，发送提示词：
```text
检查 README.md 并列出构建命令，不要修改文件。
```

### 4. 工具执行安全
SoloQueue 不创建沙箱。在已配置的 Docker 容器或 VM 中运行时，该环境构成隔离边界。直接在宿主机运行时，工具使用 SoloQueue 进程的权限。工具层会执行 Shell 黑名单、WebFetch 私有地址阻止以及文件、路径、大小和超时限制。

### 高级：使用 `settings.yaml` 配置

当前配置文件位于 `${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}/settings.yaml`；SoloQueue 首次启动时会创建该文件。你可以直接编辑，格式有效的变更会自动加载。未填写的字段使用内置默认值；显式配置的 `providers`、`models` 等列表会替换对应默认列表。

例如，添加一个兼容 OpenAI API 的 Provider 和 Model，并将任务路由到该 Model：

```yaml
providers:
  - id: my-provider
    name: My Provider
    base_url: https://api.example.com/v1
    api_key_env: MY_PROVIDER_API_KEY
    enabled: true
    is_default: true

models:
  - id: my-model
    provider_id: my-provider
    name: My Model
    context_window: 32768
    enabled: true

model_routes:
  general: my-provider:my-model
  engineering: my-provider:my-model
  research: my-provider:my-model
```

启动 SoloQueue 前，请在环境变量中设置 `MY_PROVIDER_API_KEY`。建议使用环境变量保存 API Key，避免将密钥直接写入 YAML。所有配置字段见[配置参考](reference.md)。

---

## 服务边界

原生 `serve` 和 `start` 命令默认绑定 `127.0.0.1`。可使用 `--host`
指定其他监听地址。Docker 镜像传入 `--host 0.0.0.0`，以便 Docker 发布
默认端口 `57689`。
