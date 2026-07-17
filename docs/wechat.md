# WeChat iLink Integration

SoloQueue can connect to personal WeChat accounts through Tencent's official iLink Bot API. The implementation follows the current `Tencent/openclaw-weixin` protocol rather than unofficial client hooks.

## Supported scope

- QR-code authorization, including IDC redirect and pairing-code states
- Multiple configured accounts
- Long-poll message intake with persisted in-process cursor progression
- Text messages and voice messages that include a server-side transcript
- Text replies with the inbound `context_token` and WeChat-compatible Markdown filtering
- Long-running reply keepalive through `getconfig` and a 5-second `sendtyping` heartbeat
- L1 or dedicated L2 agent binding
- User allowlists and configuration hot reload
- `/help`, `/cancel`, `/clear`, `/compact`, `/version`, and `/myid`

Not yet implemented:

- Encrypted CDN download/upload for images, files, video, and raw voice
- WeChat proactive cron delivery (the current cron target schema is QQ-specific)
- A bundled speech-to-text engine for media-only voice

## Login

Stop the running server before changing the same account from another process, then run:

```bash
soloqueue wechat login --id personal --name "Personal WeChat"
```

The same flow is available in Desktop → Settings → Channels. Credentials are written by the backend to `~/.soloqueue/settings.yaml` and are never returned to the renderer. The legacy `soloqueue weixin login` command remains a hidden compatibility alias for one release.

To bind the account directly to an L2 agent:

```bash
soloqueue wechat login \
  --id personal \
  --bind-type l2 \
  --bind-agent <agent-template-id>
```

The token is equivalent to a password. Keep `settings.yaml` private and do not commit it.

## Configuration

```yaml
wechat_bots:
  - id: personal
    name: Personal WeChat
    enabled: true
    bot_token: <issued by QR login>
    bot_id: <issued by QR login>
    base_url: https://ilinkai.weixin.qq.com
    bot_agent: SoloQueue/0.1.0
    bind_type: l1
    whitelist_enabled: false
    whitelist: []
```

Sanitized account views are available through `GET/PUT /api/config/wechat-bots/`; the legacy `/api/config/weixin-bots/` route is a temporary alias. Tokens are never returned by either route.

## Channel architecture

`internal/channel` owns transport-neutral messages, attachments, reply tokens, and session contracts. `internal/channel/qq` and `internal/channel/wechat` own protocol-specific behavior. New channels normalize inbound data into `channel.Message` and retain reply correlation in `ReplyToken`.

The reply token is intentionally opaque. For WeChat it carries `context_token`; for another channel it may be a message ID or thread token. This prevents future transports from leaking protocol types into the session package.

Channels may optionally implement `ResponseActivityStarter`. The shared text bridge starts this activity before asking the session and stops it before the final or error reply. WeChat uses the lifecycle to obtain a `typing_ticket`, send an immediate typing indicator, and refresh it every five seconds. A typing failure is logged and degrades to the normal reply path; it does not fail the agent request.

## Operational considerations

- iLink is controlled by Tencent and may be rate-limited, changed, interrupted, or terminated.
- There is no history API; continuity depends on the `get_updates_buf` cursor during a running process.
- The current cursor is not written to disk. After restart, the server starts with an empty cursor; upstream behavior should be monitored for duplicate delivery.
- Voice messages with `voice_item.text` use the transcript immediately. Media-only voice is represented as an audio attachment and receives an explicit unsupported response until CDN decryption and ASR are configured.
- iLink sends replies as TEXT items. The WeChat sender filters unsupported Markdown syntax; Markdown images must be sent as media items in a future outbound-media phase.
- Each text reply includes a unique `client_id`. Logs record message age, response size, request duration, and return codes without recording the bot token, context token, typing ticket, or full user ID.

Protocol references: [Tencent/openclaw-weixin](https://github.com/Tencent/openclaw-weixin), [official Chinese protocol README](https://github.com/Tencent/openclaw-weixin/blob/main/README.zh_CN.md), and the [original evaluation document](https://github.com/hao-ji-xing/openclaw-weixin/blob/main/weixin-bot-api.md).
