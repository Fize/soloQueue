# 远程访问

[English: Remote access](../../operations/remote-access.md)

我按本地优先方式运行服务。默认监听 127.0.0.1:57647，localhost 请求会绕过
认证。

## 明确绑定地址

需要监听其他网卡时，我会显式指定：

~~~bash
./soloqueue serve --host 0.0.0.0 --port 57647
~~~

我只会在可信网络或提供 TLS、网络策略和额外访问边界的反向代理后这样做。

## 配置认证

我可以在 settings.yaml 中设置：

~~~yaml
auth:
  user: soloqueue
  password: replace-with-a-long-random-password
~~~

也可以在启动前设置：

~~~bash
export SOLOQUEUE_AUTH_USER=soloqueue
export SOLOQUEUE_AUTH_PASSWORD="replace-with-a-long-random-password"
~~~

非回环请求在配置凭据后需要 Basic Auth；未配置凭据时，远程请求会被拒绝。
/healthz 为就绪探针而保持未认证，我不会把它当作应用已安全的证明。

桌面端可以在 Settings → Connection 中配置后端地址。我不会把凭据或短期
WebSocket Token 放进截图、Shell 历史或公开 URL。

个人机器上，我更倾向使用 SSH Tunnel 或私有 VPN，而不是直接暴露监听端口。
我不把 SoloQueue 当作多用户访问控制系统或经过企业级安全审计的服务。
