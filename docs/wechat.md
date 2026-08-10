# WeChat iLink

SoloQueue connects to personal WeChat accounts through Tencent's official
iLink Bot API. The integration is an optional channel bridge, not a separate
agent runtime.

## Login

Stop another SoloQueue process using the same account, then run:

~~~bash
soloqueue wechat login --id personal --name "Personal WeChat"
~~~

The command prints a QR-code URL, polls the login status, asks for a
verification code when required, and writes the confirmed credential to
settings.yaml. The same flow is available under Settings → Channels.

To bind directly to an L2 agent:

~~~bash
soloqueue wechat login --id personal --bind-type l2 --bind-agent agent-id
~~~

## Configuration

~~~yaml
wechat_bots:
  - id: personal
    name: Personal WeChat
    enabled: true
    bot_token: issued-by-qr-login
    bot_id: issued-by-qr-login
    base_url: https://ilinkai.weixin.qq.com
    bot_agent: SoloQueue/0.1.0
    bind_type: l1
    whitelist_enabled: false
    whitelist: []
~~~

The settings API exposes sanitized account views; keep the source
settings.yaml private because it contains credentials.

## Supported scope

- QR authorization, redirects, pairing and verification states.
- Multiple configured accounts.
- Long-poll text intake and text replies.
- Voice messages when the upstream includes a server-side transcript.
- L1 or dedicated L2 binding and optional allowlists.
- Typing activity while a response is running.

The current integration does not provide encrypted CDN media transfer,
proactive cron delivery, or a bundled speech-to-text engine for media-only
voice.

## Operational limits

iLink can be rate-limited, interrupted, changed, or terminated upstream. The
current cursor is in process, so a restart can affect update continuity. Cron
notifications and long-running replies are best-effort; use the Web UI for
definitive results.

See [Channels](guides/channels.md) for the common channel contract.
