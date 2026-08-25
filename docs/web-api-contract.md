# SoloQueue Web Runtime Contract

This contract is the API-first boundary for the browser-only runtime.

## Commands

- `soloqueue serve` starts the backend runtime and read-only status UI. `/` redirects to `/status/`.
- `soloqueue web` starts only the embedded Web Console static server. It does not initialize the database, agents, Cron, MCP, channels, or sessions. `--backend` is the backend URL used by the browser UI.
- `soloqueue start` starts one backend runtime, one HTTP listener, the Web Console at `/`, and Status UI at `/status/`.

All commands default to `127.0.0.1`; `serve` and `start` default to port `57647`, while `web` defaults to `57648`.

## HTTP routes

- `/api/*` and `/ws` preserve the existing REST/WebSocket contracts. Unknown `/api/*` routes return JSON `404`; they never return SPA HTML.
- `/healthz` is an unauthenticated JSON readiness response.
- `/status/*` serves the independent Status UI and falls back to its own `index.html`.
- In `start` and `web`, other GET/HEAD paths serve the Web Console and fall back to its `index.html`.
- In `serve`, `/` redirects to `/status/` and the Web Console is not mounted.
- `GET /api/runtime-config` returns `{ "backend_url": string }`. An empty value means same-origin.

## Origin policy

The application emits CORS headers only for `localhost`, `127.0.0.1`, and
`::1` origins, with the requesting loopback origin echoed in
`Access-Control-Allow-Origin`. It includes `Vary: Origin`, allows the REST
methods and `Content-Type`/`Accept` request headers used by the browser UI. It
allows browser-managed credentials for loopback requests, but the application
does not issue or validate any credentials. Non-loopback requests continue
through the application without application CORS headers so an external
reverse proxy can apply its own policy, including credential handling.

WebSocket connections accept loopback origins, same-host origins presented by
a reverse proxy, and requests without an Origin header. The application does
not provide HTTP authentication or WebSocket tokens.

## Browser project paths

Project paths are plain text values submitted to the existing project API. No native directory picker or file-system access API is part of this contract.
