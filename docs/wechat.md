# WeChat iLink

> 中文：[微信 iLink](zh/wechat.md)

I connect personal WeChat accounts through Tencent's official iLink Bot API.
I treat the integration as an optional channel bridge, not a separate agent
runtime.

## Login

I stop another SoloQueue process using the same account, then run:

~~~bash
soloqueue wechat login --id personal --name "Personal WeChat"
~~~

I use the command to print a QR-code URL, poll the login status, ask for a
verification code when required, and write the confirmed credential to
settings.yaml. The same flow is available under Settings → Channels.

I bind directly to an L2 agent with:

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

I use the settings API for sanitized account views; I keep the source
settings.yaml private because it contains credentials.

## Supported scope

- I support QR authorization, redirects, pairing and verification states.
- I support multiple configured accounts.
- I support long-poll text intake and text replies.
- I support voice messages when the upstream includes a server-side transcript.
- I support L1 or dedicated L2 binding and optional allowlists.
- I show typing activity while a response is running.

I do not currently provide encrypted CDN media transfer,
proactive cron delivery, or a bundled speech-to-text engine for media-only
voice.

## Operational limits

I expect iLink to be rate-limited, interrupted, changed, or terminated upstream.
I keep the current cursor in process, so a restart can affect update continuity.
I treat Cron notifications and long-running replies as best-effort and use the
Web UI for definitive results.

I use [Channels](guides/channels.md) for the common channel contract.
