# Troubleshooting

Start with the server terminal and the most recent files under
~/.soloqueue/logs/. Reproduce a problem with the smallest request possible
before changing several settings at once.

## The portal is blank

The Go binary embeds portal assets from internal/server/dist/. Run:

~~~bash
make build-web
make build
~~~

Then restart the binary. For desktop development, run the backend and the
desktop Vite server separately as described in [Installation](../getting-started/installation.md).

## The server cannot start

- Check whether the requested port is already in use.
- Run with --verbose and inspect the initialization error.
- Confirm that settings.yaml is valid YAML and that provider/model IDs resolve.
- If a previous process is still running, stop it before starting another one.

## The model does not respond

- Confirm the provider is enabled and its API key is available to the server
  process.
- Confirm the selected model belongs to that provider.
- Inspect model_routes and the fallback route.
- Check the provider base URL, timeout, and network access.

## A tool action is blocked

Read the confirmation card and the server log. A blocked confirmation is
different from a tool execution failure. Check the project path, shell policy,
HTTP host policy, and MCP policy before changing global settings.

## Remote requests return 403 or 401

Remote access is denied when no credentials are configured. Configure both
auth fields or both SOLOQUEUE_AUTH_USER and SOLOQUEUE_AUTH_PASSWORD, then
retry with Basic Auth. Localhost requests intentionally follow a different
path; test the actual remote address.

## Cron ran but no message arrived

Use the Web UI cron history as the source of truth. Verify the agent's
notify_channel, recent QQ/WeChat activity, channel connection state, and
server logs. Channel delivery is best-effort and can be dropped after a
restart or upstream rate limit.

## Config changes appear ignored

Wait for the hot-reload message in the server log, then refresh the UI. Check
that you edited the active work directory, not another settings.yaml selected
by SOLOQUEUE_WORK_DIR. Restart once to separate a reload problem from a
runtime problem.

## Where to report a reproducible issue

Include the SoloQueue revision, OS, command line, sanitized configuration
sections, and relevant log categories. Remove API keys, channel tokens,
prompts, file contents, and private paths before sharing.
