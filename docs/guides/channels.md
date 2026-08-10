# Channels

SoloQueue can connect the agent runtime to QQ Bot and WeChat iLink. Channels
are an input/output surface for the same sessions; they do not create a
separate memory or model system.

## QQ Bot

QQ uses Tencent's Bot Gateway and can receive private, group, and guild
messages according to the configured intents. Configure the app credentials in
Settings → Channels or settings.yaml, then enable the account.

See the maintainer protocol notes in [qqbot.md](../qqbot.md) for gateway
details and message limits.

## WeChat iLink

Authorize an account with the QR flow:

~~~bash
soloqueue wechat login --id personal --name "Personal WeChat"
~~~

The same flow is available in Settings → Channels. Credentials are written by
the backend; keep settings.yaml private and never commit it.

See [wechat.md](../wechat.md) for supported message types and known iLink
limitations.

## Binding and allowlists

An account can bind to the primary session (L1) or a named L2 agent. Both
channels support optional user allowlists. Configure the binding and allowlist
before enabling a channel on a shared machine.

## Delivery expectations

Channel APIs can rate-limit, disconnect, or change behavior. QQ follow-up
messages are queued and rate-limited. WeChat requires an active context and
uses a typing keepalive while a response is running. Cron notification is
best-effort; check the Web UI for the definitive run result.
