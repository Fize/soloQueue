# Desktop Remote Connection Authentication Secure Design

## Scope and Compatibility

This design changes only the Desktop connection implementation. The Go
backend's REST paths, request/response payloads, WebSocket envelopes, and
session concurrency rules remain unchanged.

The Desktop is an Electron-only client:

- `local` mode connects to the loopback backend and does not perform auth.
- `remote` mode connects to the configured backend and performs auth.
- Renderer assets are always bundled with the Electron application; the
  remote backend is an API/WebSocket endpoint, not the source of Desktop CSS
  or JavaScript.

## Auth Strategy

### Local mode

- Main process starts the bundled Go backend and waits for `/healthz`.
- HTTP uses `http://127.0.0.1:<port>`.
- WebSocket uses `ws://127.0.0.1:<port>/ws`.
- No credentials are loaded or decrypted.
- No `Authorization` header is created or injected.
- `/api/auth/token` is not requested.

### Remote mode

- Main process does not start the bundled Go backend.
- Remote URL is validated as an `http` or `https` URL without embedded user
  info or an ambiguous path form.
- The persisted remote credential is owned by the Main process; the Renderer
  may hold the current form value only for the active session.
- The password is persisted only through Electron `safeStorage` when the host
  supports it. Otherwise it is session-only and must be entered again after a
  restart.
- REST Authorization is injected only for the configured remote origin and
  backend paths `/api`, `/api/**`, and `/healthz`.
- Every WebSocket connect/reconnect first obtains a fresh one-time token from
  `/api/auth/token`, then connects to `/ws?token=<token>`. The token is held
  only for the active connection attempt and is never persisted or logged.
- Passwords and Authorization values are never returned to the Renderer by
  the persisted-configuration IPC API.

## Credential Storage and Migration

The Main process owns a versioned connection record under Electron's
`app.getPath('userData')`. It contains the mode, endpoint, username, and an
encrypted password value.

The first release with this design performs a one-time migration from the
current Renderer localStorage keys. The migration is accepted only through a
dedicated IPC call, is written atomically, and removes the old password key
only after persistence succeeds. After migration, Renderer localStorage may
retain non-secret UI preferences but must not retain remote credentials.

## HTTP and WebSocket Transport

All feature requests use one explicit HTTP client. It has separate API and
root path resolution so `/healthz` is not accidentally transformed into
`/api/healthz`. It preserves JSON, text/YAML, Blob, FormData, and 204 response
semantics.

The HTTP client does not implement a second auth mechanism. In local mode it
sends no auth. In remote mode the Electron session policy supplies the Basic
header only for the configured backend request scope.

The WebSocket client owns connection lifecycle only. It requests a new remote
token for each connection attempt and does not put credentials in the URL.
Protocol routing and request state remain separate from the transport.

## Input Validation

- Remote URLs accept only `http` and `https` schemes.
- Remote URLs containing username/password components are rejected.
- Remote endpoint values are normalized before origin comparison.
- IPC path operations accept only normalized paths and reject traversal or
  unsupported path forms.
- External navigation accepts only `http` and `https` URLs.
- REST error bodies are parsed as untrusted data and normalized into typed
  errors without exposing raw sensitive content in UI messages.
- WebSocket messages are parsed defensively; unknown event types are ignored
  with a redacted debug record rather than terminating the connection.

## Output Encoding and Rendering

- Renderer UI continues to use React escaping.
- Markdown remains inside the existing Streamdown/Markdown renderer boundary.
- Raw HTML support is reviewed and constrained to the existing behavior; no
  new `innerHTML` or script execution path is introduced.
- Remote file/image content is loaded through the authenticated transport
  where a request header is required; arbitrary remote URLs are not treated as
  local file paths.
- Error UI shows stable user-facing messages, not response headers, tokens,
  credentials, or stack traces.

## IPC Contract

The preload bridge exposes typed, narrow operations for connection config,
backend lifecycle, window controls, dialogs, and safe shell operations.

- Renderer cannot access `ipcRenderer` directly.
- Main validates every IPC argument.
- `openExternal` allows only `http` and `https`.
- `openPath` uses a normalized path and delegates to Electron's safe shell
  operation; it does not execute a shell command.
- Event subscriptions return an unsubscribe function and are removed on
  component teardown.

## Error Handling and Logging

- Authentication failure is reported as a generic connection error with the
  HTTP status available for diagnostics but without credentials or raw body.
- Local backend startup failure reports the backend health/startup state.
- Remote connection switching stops old transport activity before creating a
  new one.
- Logs may include mode, request path, status, and connection state, but never
  include passwords, Authorization headers, WebSocket tokens, or full URLs
  containing credentials.
- Debug logging is disabled or redacted in packaged production behavior.

## Dependencies

No new runtime dependency is required for the first implementation. The
existing Electron `safeStorage`, `session.webRequest`, fetch, WebSocket,
Vitest, Testing Library, and Go `testing` infrastructure are sufficient.

If a future dependency is proposed, it must be checked with the repository's
package manager audit tooling before being added.

## Verification Plan

- Unit tests for URL normalization and auth request scope.
- Unit tests proving local mode performs no auth work.
- Unit tests proving remote reconnect obtains a fresh WS token.
- IPC tests for invalid URL/path rejection.
- Migration tests proving the old password key is removed only after a
  successful Main-process write.
- REST contract tests for `/healthz`, `/api/auth/token`, JSON, text, Blob,
  FormData, and 204 responses.
- Electron packaged smoke test for local startup and remote connection.
- Static scan for plaintext password persistence and sensitive logging.
