# 微信 iLink

[English: WeChat iLink](../wechat.md)

我通过腾讯官方 iLink Bot API 把个人微信账号连接到 SoloQueue。它是可选的渠道
桥接，不是独立的 Agent Runtime。

## 登录

我先停止使用同一账号的其他 SoloQueue 进程，再运行：

~~~bash
soloqueue wechat login --id personal --name "Personal WeChat"
~~~

我用这个命令打印二维码 URL、轮询登录状态、按需读取验证码，并把确认后的凭据写入
settings.yaml。我也可以在 Settings → Channels 中完成相同流程。

绑定到 L2 Agent：

~~~bash
soloqueue wechat login --id personal --bind-type l2 --bind-agent agent-id
~~~

## 支持范围

- QR 授权、重定向、配对和验证状态。
- 多账号配置。
- 长轮询文本接收和文本回复。
- 上游提供服务端 Transcript 时的语音消息。
- L1 或指定 L2 绑定，以及可选白名单。
- 响应运行期间的 typing activity。

我目前不提供加密 CDN 媒体传输、主动 Cron 投递，或媒体语音的内置语音转文字。
我预期上游可能限流、终止或改变行为；重启也可能影响 Cursor 连续性。我把 Web UI
作为最终结果来源。
