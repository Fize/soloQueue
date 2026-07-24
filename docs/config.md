# Configuration System

**Location**: `internal/config/`

SoloQueue uses a hybrid configuration model. Runtime settings are split between local TOML files and the shared SQLite database, balancing environment-specific parameters with dynamic UI-driven configuration updates.

---

## 1. Hybrid Storage Architecture

```
                  Bootstrap & Process Settings
                              │
                              ▼
                   ┌──────────────────────┐
                   │  settings.yaml /     │  --> Contains Auth, Log, Agent blocks
                   │  settings.local.toml │
                   └──────────┬───────────┘
                              │
                    Dynamic / API Settings
                              │
                              ▼
                   ┌──────────────────────┐
                   │  SQLite DB (SQLite)  │  --> Contains Providers, Models, Tools,
                   │  (soloqueue.db)      │      QQBot, LSPMCP, Session, Embedding
                   └──────────────────────┘
```

### TOML Files (`settings.yaml` & `settings.local.toml`)

- **Location**: `~/.soloqueue/settings.yaml` (shared user config) and `~/.soloqueue/settings.local.toml` (optional machine-specific override).
- **Scope**: Used for process bootstrap parameters, authentication, logging options, and core agent server settings.
- **Serialization Rules (`MarshalTOML`)**: When settings are edited in the UI and saved back to the file system, SoloQueue customizes serialization:
  - Only `Auth`, `Log`, and `Agent` blocks are written back to `settings.yaml`.
  - Migrated blocks are **excluded** from TOML serialization to prevent conflicts with database values.

### SQLite Database (`soloqueue.db`)

- **Scope**: Shared database for all runtime state — LLM providers, models, default model mappings, tool execution limits, QQ Bot gateway settings, LSP server entries, embedding parameters, memory engine (BM25 + KG + vectors), cron tasks, team/agent definitions, and telemetry.
- **Tables**: Managed transactionally in SQLite:
  - `llm_providers`: API endpoints and keys.
  - `llm_models`: Context window sizes and parameters.
  - `llm_default_models`: Default model roles.
  - `system_settings`: Key-value JSON storage for remaining configurations (like tools, session, embedding, and QQ Bot integration).
- **Benefit**: Permits dynamic settings changes from client APIs without editing files on disk.

---

## 2. Loader & Layered Initialization

At startup, the `config.Loader` initializes settings by merging inputs in ascending priority:

1. **Compiled Defaults** (`defaults.go`): Hardcoded fallback values.
2. **Global File** (`settings.yaml`): Shared parameters.
3. **Local File** (`settings.local.toml`): Local process overrides.

### Merge Rules

- **Objects/Maps**: Merged recursively. Keys in higher layers override lower layers.
- **Arrays**: Replaced wholesale. No index-by-index merging.
- **Omits**: Missing keys in overrides preserve default/lower-layer values.

### Hot Reload

- The loader watches TOML files using `fsnotify`.
- File writes or rename events trigger reloading.
- Debounced by **200ms** to prevent partial reads during multi-pass editor save operations.

---

## 3. Configuration Reference

Below is the complete structure of all config blocks.

### `[auth]` — HTTP API Authentication

- `user` (string): Username. If empty, authentication is disabled.
- `password` (string): Password. Excluded from JSON payloads for security.

### `[log]` — Logger Output Settings

- `level` (string): Logging level (`debug`, `info`, `warn`, `error`).
- `console` (bool): If true, logs are output to stderr.
- `file` (bool): If true, logs are written to rotating files under `~/.soloqueue/logs/`.

### `[agent]` — Core Agent Subsystems

- `builtin_mcp_servers` (array of strings): Whitelist of built-in MCP servers to load. If omitted, all built-ins are loaded.
- `external_mcp_servers` (array of strings): Whitelist of external MCP servers to load.

### `[session]` — Session Context History

- `timeline_max_file_mb` (int): Maximum size of an event timeline log before rotation (default: 50MB).

### `[tools]` — Built-in Tool Constraints

Controls sandbox resource usage and safety rules:

- **Read/Write Limits**:
  - `max_file_size` (int): Max bytes allowed when reading a file.
  - `max_matches` (int): Ripgrep matches cap.
  - `max_write_size` (int): Max bytes written in one file write operation.
  - `max_multi_write_bytes` (int): Total bytes written in multi-replace operations.
  - `max_multi_write_files` (int): Max files modified in one edit call.
- **HTTP Fetching**:
  - `http_allowed_hosts` (array): Host names the agent is allowed to connect to.
  - `http_block_private` (bool): If true, blocks private subnet fetches (RFC 1918) to prevent SSRF.
- **Shell Executions (`Bash`)**:
  - `shell_block_regexes` (array): Command patterns that are blocked in the shell tool.
  - `shell_confirm_regexes` (array): Command patterns requiring explicit user confirmation before running.
  - `shell_max_output` (int): Max stdout/stderr size in bytes kept in context.

### `[[providers]]` — LLM Connection Endpoints

- `id` (string): Unique identifier (e.g. `deepseek`).
- `name` (string): User-friendly label.
- `base_url` (string): API endpoint URL.
- `api_key` / `api_key_env` (string): Cleartext API key or the environment variable key holding it.
- `timeout_ms` (int): Request timeout.
- `[providers.retry]`:
  - `max_retries` (int): Max retry count.
  - `initial_delay_ms` / `max_delay_ms` (int): Backoff parameters.
  - `backoff_multiplier` (float): Multiplier for exponential backoff (e.g. 2.0).

### `[[models]]` — LLM Model catalog

- `id` (string): Unique model identifier.
- `provider_id` (string): The hosting provider's ID.
- `context_window` (int): Context window token count (hard waterline).
- `[models.generation]`:
  - `temperature` (float): Sampling temperature.
  - `max_tokens` (int): Max generation length.
- `[models.thinking]`:
  - `enabled` (bool): Enable reasoning/thinking mode (e.g. DeepSeek-R1).
  - `reasoning_effort` (string): Effort setting (`low`, `medium`, `high`, `max`).

### `[default_models]` — Role Mappings

Maps generic model roles used by agents to concrete provider model IDs (format: `provider_id:model_id`):

- `expert`: Used for complex code generation and system design.
- `superior`: Used for medium tasks.
- `universal`: Used for simple tasks.
- `fast`: Used for quick chat classification and summarizations.
- `fallback`: Fallback model if the selected model fails.

### `[embedding]` — Memory Engine Embedding Provider

Controls the vector search pipeline in the memory engine:

- `provider` (string): Embedding provider. `"none"` (default, dual-hybrid BM25+KG), `"openai"` (remote API).

Sub-table `[embedding.openai]`:

- `base_url` (string): OpenAI-compatible API endpoint.
- `api_key` (string): API key.
- `model` (string): Model name (e.g., `text-embedding-3-small`).
- `dimension` (int): Embedding dimension.

When `provider = "none"`, the memory engine uses only BM25 and KG — no embedding model or API key needed.

### `[qqbot]` — Tencent QQ Bot Integration

- `enabled` (bool): Activate the gateway connection.
- `app_id` / `app_secret` (string): Tencent Developer App ID and Secret.
- `intents` (int): Integer bitmask representing gateway intents.
- `sandbox` (bool): Use Tencent sandbox environment for testing.

### `wechat_bots` — WeChat iLink Integration

Each list entry configures one official iLink bot account:

- `enabled` (bool): Start long polling for the account.
- `bot_token` / `bot_id` (string): Credentials issued by Desktop QR login or `soloqueue wechat login`.
- `base_url` (string): Tencent API base URL returned during login.
- `bot_agent` (string): Observability identifier sent in `base_info`.
- `bind_type` (string): `l1` or `l2`; `bind_agent` is required for `l2`.
- `whitelist_enabled` / `whitelist`: Optional allowed WeChat user IDs.

The legacy `weixin_bots` key is read for one compatibility release and rewritten as `wechat_bots` on save. See [wechat.md](wechat.md) for login, limitations, and security guidance.

### `[[lspmcp.servers]]` — Built-in LSP Server Configurations

Spawns editor language servers as MCP tools:

- `id` (string): LSP server ID (e.g. `gopls`).
- `command` (string): Subprocess executable name.
- `args` (array of strings): Startup arguments.
- `languages` (array of strings): Target languages (e.g., `["go"]`).
- `extensions` (array of strings): Monitored file extensions (e.g., `[".go"]`).
- `disabled` (bool): Disable the LSP server.

---

## 4. Key APIs

- **`Loader.Get()`**: Returns an immutable, thread-safe snapshot copy of `Settings`. Callers should query options from this struct to prevent mutation races.
- **`DefaultModelByRole(role string)`**: Resolves a role mapping by checking the role key, falling back to the `fallback` configuration, and ultimately returning hardcoded defaults.
- **`ToolsConfig.ToToolsConfig()`**: Sanitizes values, translating milliseconds/seconds integers into Go `time.Duration` structures and setting up sandbox path permissions.
