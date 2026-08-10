# QQ Bot

SoloQueue's QQ channel lives under internal/channel/qq and connects to
Tencent's Bot Gateway.

## User setup

Create or select a Tencent QQ Bot application, copy its App ID and App Secret
into Settings → Channels, choose the required gateway intents, and enable the
account. Bind the account to the primary session or a named L2 agent when
needed. Use a whitelist for a shared or test account.

## Runtime behavior

Incoming private, group, and guild messages are normalized into the shared
channel contract and forwarded to a session. The first response is sent as the
passive reply when the API allows it; follow-up chunks use an active,
rate-limited queue. Markdown and long responses may be split to fit QQ
constraints.

Local message commands include /help, /cancel, /clear, and /version. They are
handled before an LLM request is created.

## Limits

QQ gateway availability, intent permissions, API limits, and media upload
rules are controlled by Tencent. A connected bot is not proof that every
message or cron notification will be delivered. Check the Web UI and server
logs for the authoritative run result.

For the cross-channel setup and security boundary, see
[Channels](guides/channels.md) and [Security and permissions](operations/security-and-permissions.md).
