# Troubleshooting

> 中文：[故障排查](../zh/operations/troubleshooting.md)

I start with the server terminal and the most recent files under
~/.soloqueue/logs/. I reproduce a problem with the smallest request possible
before changing several settings at once.

## The portal is blank

I build my Go binary with portal assets from internal/server/dist/. I run:

~~~bash
make build-web
make build
~~~

I then restart the binary. For desktop development, I run the backend and the
desktop Vite server separately as described in [Installation](../getting-started/installation.md).

## The server cannot start

- I check whether the requested port is already in use.
- I run with --verbose and inspect the initialization error.
- I confirm that settings.yaml is valid YAML and that provider/model IDs resolve.
- If a previous process is still running, I stop it before starting another one.

## The model does not respond

- I confirm the provider is enabled and its API key is available to the server
  process.
- I confirm the selected model belongs to that provider.
- I inspect model_routes and the fallback route.
- I check the provider base URL, timeout, and network access.

## A tool action is blocked

I read the confirmation card and the server log. I distinguish a blocked
confirmation from a tool execution failure, then check the project path, shell
policy, HTTP host policy, and MCP policy before changing global settings.

## Remote requests return 403 or 401

I expect remote access to be denied when no credentials are configured. I
configure both auth fields or both SOLOQUEUE_AUTH_USER and
SOLOQUEUE_AUTH_PASSWORD, then retry with Basic Auth. I test the actual remote
address because localhost requests intentionally follow a different path.

## Cron ran but no message arrived

I use the Web UI cron history as the source of truth. I verify the agent's
notify_channel, recent QQ/WeChat activity, channel connection state, and
server logs. Channel delivery is best-effort and can be dropped after a
restart or upstream rate limit.

## Config changes appear ignored

I wait for the hot-reload message in the server log, then refresh the UI. I
check that I edited the active work directory, not another settings.yaml
selected by SOLOQUEUE_WORK_DIR. I restart once to separate a reload problem
from a runtime problem.

## Where to report a reproducible issue

I include the SoloQueue revision, OS, command line, sanitized configuration
sections, and relevant log categories. I remove API keys, channel tokens,
prompts, file contents, and private paths before sharing.
