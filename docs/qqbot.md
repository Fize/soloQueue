# QQ Bot

> 中文：[QQ Bot](zh/qqbot.md)

My QQ channel lives under internal/channel/qq and connects to Tencent's Bot
Gateway.

## User setup

I create or select a Tencent QQ Bot application, copy its App ID and App Secret
into Settings → Channels, choose the required gateway intents, and enable the
account. I bind the account to the primary session or a named L2 agent when
needed, and use a whitelist for a shared or test account.

## Runtime behavior

I normalize incoming private, group, and guild messages into the shared
channel contract and forward them to a session. I send the first response as
the passive reply when the API allows it; follow-up chunks use an active,
rate-limited queue. I may split Markdown and long responses to fit QQ
constraints.

I support local message commands including /help, /cancel, /clear, and
/version, and handle them before I create an LLM request.

## Limits

Tencent controls QQ gateway availability, intent permissions, API limits, and
media upload rules. I do not treat a connected bot as proof that every message
or cron notification will be delivered. I check the Web UI and server logs for
the authoritative run result.

For the cross-channel setup and security boundary, see
[Channels](guides/channels.md) and [Security and permissions](operations/security-and-permissions.md).
