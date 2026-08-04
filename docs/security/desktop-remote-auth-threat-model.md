# Desktop Remote Connection Authentication Threat Model

## Scope

This document covers the Electron desktop client's local and remote connection
paths, including REST requests, WebSocket setup, Electron IPC, persisted
connection configuration, and the one-time migration from the current
renderer localStorage configuration.

The desktop client is an Electron application. It is not a browser product.
The local connection path talks to the loopback Go backend and does not require
authentication. The remote connection path requires Basic Auth for REST and a
single-use WebSocket token obtained from `/api/auth/token`.

## Assets to Protect

- [ ] Remote username and password.
- [ ] Derived Basic Authorization headers.
- [ ] Single-use WebSocket tokens.
- [ ] Remote endpoint configuration.
- [ ] Local backend process lifecycle and connection state.
- [ ] User files, session history, tool confirmations, and workflow data.
- [ ] IPC capabilities exposed from the Electron main process.

## Trust Boundaries

- [ ] Remote configuration and credentials enter through user-controlled
      Renderer form input.
- [ ] Renderer JavaScript is less trusted than the Electron main process and
      must not receive the persisted remote password after configuration.
- [ ] `contextBridge`/preload is the boundary between Renderer and main
      process capabilities.
- [ ] Renderer HTTP requests cross from the local `file://` origin to either
      the loopback backend or the configured remote backend.
- [ ] The remote backend is an external network service and may return
      malformed, unauthorized, or unexpected responses.
- [ ] Markdown, filenames, URLs, and WebSocket payloads may contain
      untrusted data.
- [ ] External links, opened files, dialogs, and backend lifecycle commands
      are privileged Electron operations.

## Threats Identified

- [ ] Basic credentials are sent to an unrelated HTTP/HTTPS origin because a
      request interceptor is global rather than target-scoped.
- [ ] Credentials remain readable in Renderer localStorage or are exposed to
      arbitrary Renderer code.
- [ ] A stale or wrong Authorization scheme is used by a legacy feature path.
- [ ] A WebSocket token is reused after its one-time validity window or sent
      to the wrong remote endpoint.
- [ ] Local mode accidentally enters the remote authentication flow or sends
      an Authorization header to localhost.
- [ ] A Renderer can invoke privileged IPC operations with unvalidated paths,
      URLs, or lifecycle parameters.
- [ ] Remote URL input can target an unintended host, scheme, path, or local
      service.
- [ ] Reconnection or connection switching mixes requests, credentials, or
      runtime state between old and new endpoints.
- [ ] Remote Markdown or file responses cause script, navigation, or file
      access issues when rendered or opened.
- [ ] Authentication failures expose passwords, tokens, or sensitive response
      details through logs or user-facing errors.

## Planned Mitigations

- [ ] Keep `local` and `remote` as explicit connection modes. Local mode does
      not read credentials, add Authorization, or request a WebSocket token.
- [ ] Store remote passwords only in Electron `safeStorage` when available;
      never persist them in Renderer localStorage.
- [ ] Scope remote Authorization injection to the configured remote origin and
      the backend paths `/api/**` and `/healthz`.
- [ ] Remove feature-level manual Authorization construction and route all
      REST calls through one HTTP client.
- [ ] Request a fresh `/api/auth/token` token on every remote WebSocket
      connection and reconnect; never log or cache it beyond the connection.
- [ ] Validate remote URLs as `http`/`https` endpoints and reject unsupported
      schemes, credentials embedded in URLs, and ambiguous endpoint forms.
- [ ] Make connection switching stop old transport activity before starting
      the new endpoint and clear request-scoped state by `requestId`.
- [ ] Define and validate a typed preload IPC contract. Validate filesystem
      paths and allow only `http`/`https` external URLs.
- [ ] Keep Markdown rendering within the existing safe renderer boundary and
      avoid raw HTML execution or unvalidated external navigation.
- [ ] Redact credentials, Authorization values, WebSocket tokens, and remote
      response secrets from logs and UI errors.
- [ ] Add unit, integration, and Electron boundary tests for each mitigation.

## Residual Risks

- [ ] A compromised Renderer process can still initiate allowed requests while
      the application is connected; the design limits credential exposure but
      does not turn the Renderer into a full security sandbox.
- [ ] `safeStorage` availability and security properties depend on the host OS
      keychain. If unavailable, the password must remain session-only.
- [ ] TLS certificate and remote server trust are delegated to Chromium's
      standard validation; custom certificate bypasses are out of scope.
- [ ] Remote backend authorization and rate limiting remain backend concerns.

## Security Verification

- [ ] Local mode test proves no Authorization header and no `/api/auth/token`
      request.
- [ ] Remote mode test proves Authorization is sent only to the configured
      backend paths.
- [ ] Cross-origin request test proves credentials are not injected.
- [ ] WebSocket reconnect test proves a fresh token is requested each time.
- [ ] Legacy localStorage migration test proves the old password is removed
      after successful migration.
- [ ] IPC tests reject invalid URLs and unsafe paths.
- [ ] Static scan confirms no password/token logging and no plaintext password
      persistence in the Desktop source.
