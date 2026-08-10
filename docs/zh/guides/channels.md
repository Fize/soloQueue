# 渠道

[English: Channels](../../guides/channels.md)

我可以把 Agent 运行时连接到 QQ Bot 和微信 iLink。渠道复用同一套会话，不会
创建独立的 Memory 或 Model 系统。

## QQ Bot

我使用腾讯 Bot Gateway 接收 QQ 私聊、群聊和 Guild 消息，具体范围取决于我配置的 Intent。
我在 Settings → Channels 或 settings.yaml 配置 App 凭据，然后启用账号。

网关细节和消息限制见 [QQ Bot 维护者说明](../../qqbot.md)。

## 微信 iLink

我使用二维码流程授权账号：

~~~bash
soloqueue wechat login --id personal --name "Personal WeChat"
~~~

我也可以在 Settings → Channels 中完成同样的流程。后端会把凭据写入 settings.yaml，
我不会把它提交到 Git。

账号可以绑定主会话（L1）或指定的 L2 Agent，也可以配置用户白名单。共享机器
上启用渠道前，我会先设置绑定和白名单。

我让 QQ 后续消息排队并限流，让微信长响应使用 typing keepalive。Cron 通知是尽力而为，
我以 Web UI 中的最终结果为准。
