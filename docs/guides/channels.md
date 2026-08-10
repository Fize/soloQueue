# Channels

> 中文：[渠道](../zh/guides/channels.md)

I connect my agent runtime to QQ Bot and WeChat iLink through channels. I treat
a channel as an input/output surface for the same sessions; I do not use it to
create a separate memory or model system.

## QQ Bot

I use Tencent's Bot Gateway for QQ and can receive private, group, and guild
messages according to the configured intents. I configure app credentials in
Settings → Channels or settings.yaml, then enable the account.

I keep gateway details and message limits in the maintainer protocol notes in
[qqbot.md](../qqbot.md).

## WeChat iLink

I authorize an account with the QR flow:

~~~bash
soloqueue wechat login --id personal --name "Personal WeChat"
~~~

I can use the same flow in Settings → Channels. I let the backend write
credentials, keep settings.yaml private, and never commit it.

I document supported message types and known iLink limitations in
[wechat.md](../wechat.md).

## Binding and allowlists

I can bind an account to the primary session (L1) or a named L2 agent. I use
optional user allowlists for both channels. I configure the binding and
allowlist before enabling a channel on a shared machine.

## Delivery expectations

I expect channel APIs to rate-limit, disconnect, or change behavior. I queue and
rate-limit QQ follow-up messages. I use an active context and typing keepalive
for WeChat while a response is running. I treat Cron notification as
best-effort and check the Web UI for the definitive run result.
