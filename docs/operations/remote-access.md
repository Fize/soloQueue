# Remote access

> 中文：[远程访问](../zh/operations/remote-access.md)

I run the server local-first. By default it listens on 127.0.0.1:57647 and
localhost requests bypass authentication.

## Bind deliberately

When I need to listen on another interface, I run:

~~~bash
./soloqueue serve --host 0.0.0.0 --port 57647
~~~

I do this only on a trusted network or behind a reverse proxy that provides
TLS, network policy, and an additional access boundary.

## Configure authentication

I set credentials in settings.yaml:

~~~yaml
auth:
  user: soloqueue
  password: replace-with-a-long-random-password
~~~

Or I set both environment variables before starting the server:

~~~bash
export SOLOQUEUE_AUTH_USER=soloqueue
export SOLOQUEUE_AUTH_PASSWORD="replace-with-a-long-random-password"
~~~

For non-loopback requests, my server requires Basic Authentication when
credentials are configured. If I configure no credentials, it denies remote
requests. The health endpoint is intentionally unauthenticated for readiness
checks, so I do not treat it as proof that the application is secured.

I use a short-lived one-time token obtained through the authenticated API for
WebSocket clients. I do not place credentials or tokens in shared screenshots,
shell history, or public URLs.

## Desktop connection

I use Settings → Connection to point the desktop client at the backend address.
I keep the base URL on a trusted network and verify authentication behavior
from the backend logs before using a remote project.

## Recommended deployment boundary

On my personal machine, I prefer an SSH tunnel or private VPN over exposing the
listener directly. I do not put a development instance on the public Internet.
I do not treat SoloQueue as a multi-user access-control system or an enterprise
security-audited service.
