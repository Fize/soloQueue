# 故障排查

[English: Troubleshooting](../../operations/troubleshooting.md)

我先看服务端终端和 ~/.soloqueue/logs/ 下最新的文件。我会先用最小请求复现，
再修改多个设置。

## Portal 空白

我把 Portal 资源嵌入 internal/server/dist/ 的 Go 二进制，并运行：

~~~bash
make build-web
make build
~~~

然后重启二进制。桌面开发时，我会按[安装文档](../getting-started/installation.md)
分别运行后端和桌面 Vite Server。

## 服务无法启动

- 我检查端口是否已被占用。
- 我使用 --verbose 并查看初始化错误。
- 我确认 settings.yaml 是有效 YAML，Provider/Model ID 能够解析。
- 如果旧进程仍然存在，我先停止它。

## 模型没有响应

- 我确认 Provider 已启用，服务端进程能读取 API Key。
- 我确认选择的 Model 属于该 Provider。
- 我检查 model_routes 和 fallback。
- 我检查 Provider Base URL、超时和网络访问。

## 工具操作被阻止

我先阅读确认卡片和服务日志。确认被拒绝与工具执行失败是两种不同问题；
我会分别检查项目路径、Shell 策略、HTTP Host 策略和 MCP 策略。

## 远程返回 403 或 401

我未配置凭据时，远程访问会被拒绝；我会配置两项 auth 字段，或同时配置
SOLOQUEUE_AUTH_USER 和 SOLOQUEUE_AUTH_PASSWORD，再使用 Basic Auth 重试。

## Cron 运行但没有消息

我以 Web UI 的 Cron History 为准，然后检查 Agent 的 notify_channel、QQ/微信
活动、渠道连接和服务日志。重启后或遇到上游限流时，渠道消息可能丢失。

## 配置变化没有生效

我等待服务日志中的热加载消息并刷新 UI，同时确认编辑的是
SOLOQUEUE_WORK_DIR 指向的 settings.yaml。必要时我重启一次，以区分热加载问题
和运行时问题。
