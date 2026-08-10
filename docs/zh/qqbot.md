# QQ Bot

[English: QQ Bot](../qqbot.md)

我把 SoloQueue 的 QQ 渠道放在 internal/channel/qq，并连接腾讯 Bot Gateway。

## 我的设置流程

我创建或选择腾讯 QQ Bot 应用，把 App ID 和 App Secret 填入 Settings → Channels，
选择所需 Gateway Intent，并启用账号。需要时，我把账号绑定到主会话或命名的
L2 Agent；共享或测试账号会配置白名单。

## 运行行为

我把收到的私聊、群聊和 Guild 消息转换为共享渠道契约，再转发到 Session。API 允许
时，我把第一段响应作为被动回复发送；后续内容通过限流队列主动发送。我可能拆分
Markdown 和长响应。

本地命令包括 /help、/cancel、/clear 和 /version。它们在创建 LLM 请求之前处理。

## 限制

网关可用性、Intent 权限、API 限流和媒体上传规则由腾讯控制。连接成功不代表每条
消息或 Cron 通知都一定送达。我会以 Web UI 和服务日志作为最终运行证据。
