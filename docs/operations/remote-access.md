# Remote access

The server is local-first. By default it listens on 127.0.0.1:57647 and
localhost requests bypass authentication.

## Bind deliberately

To listen on another interface:

~~~bash
./soloqueue serve --host 0.0.0.0 --port 57647
~~~

Do this only on a trusted network or behind a reverse proxy that provides TLS,
network policy, and an additional access boundary.

## Configure authentication

Set credentials in settings.yaml:

~~~yaml
auth:
  user: soloqueue
  password: replace-with-a-long-random-password
~~~

Or set both environment variables before starting the server:

~~~bash
export SOLOQUEUE_AUTH_USER=soloqueue
export SOLOQUEUE_AUTH_PASSWORD="replace-with-a-long-random-password"
~~~

For non-loopback requests, the server requires Basic Authentication when
credentials are configured. If no credentials are configured, remote requests
are denied. The health endpoint is intentionally unauthenticated for
readiness checks and must not be treated as proof that the application is
secured.

WebSocket clients use a short-lived one-time token obtained through the
authenticated API. Do not place credentials or tokens in shared screenshots,
shell history, or public URLs.

## Desktop connection

Use Settings → Connection to point the desktop client at the backend address.
Keep the base URL on a trusted network and verify the authentication behavior
from the backend logs before using a remote project.

## Recommended deployment boundary

For a personal machine, prefer an SSH tunnel or a private VPN over exposing
the listener directly. Do not put a development instance on the public
Internet. SoloQueue is not a multi-user access-control system and has not been
security audited as an enterprise service.
