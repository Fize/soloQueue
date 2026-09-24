# Reference Manual

English | [简体中文](zh/reference.md)

This reference manual documents configuration parameters (`settings.yaml`), CLI command line usage, database storage, backup procedures, and security policies.

---

## 1. Configuration Reference (`settings.yaml`)

Configuration settings are loaded from `settings.yaml` located in the active work directory (`~/.soloqueue/` by default; overridden by `SOLOQUEUE_WORK_DIR`). Supported settings are hot-reloaded when modified.

### Configuration Example

```yaml
providers:
  - id: deepseek
    name: DeepSeek
    base_url: https://api.deepseek.com/v1
    api_key_env: DEEPSEEK_API_KEY
    enabled: true
    is_default: true

models:
  - id: deepseek-v4-flash-thinking
    provider_id: deepseek
    name: DeepSeek V4 Flash (Thinking)
    context_window: 1048576
    enabled: true
    generation:
      temperature: 0
      max_tokens: 16384
    thinking:
      enabled: true
      reasoning_effort: high

model_routes:
  general: deepseek:deepseek-v4-flash-thinking
  engineering: deepseek:deepseek-v4-flash-thinking
  research: deepseek:deepseek-v4-flash-thinking
  classifier: deepseek:deepseek-v4-flash-thinking
  fallback: deepseek:deepseek-v4-flash-thinking
```

### Configuration Sections

| Section | Purpose |
| --- | --- |
| `providers` | OpenAI-compatible API endpoint definitions and retries |
| `models` | Model definitions, context windows, generation, thinking parameters, and vision support |
| `model_routes` | Task routes (`general`, `engineering`, `research`) and auxiliary `classifier`, `vision`, and `fallback` routes |
| `tools` | File paths, shell filters, HTTP host allowlists, output limits, and image-generation models (`image_models`) |
| `agent` | Internal agent tool and MCP server settings |
| `qqbots` / `wechat_bots` / `telegram_bots` | Credentials and session bindings for QQ, WeChat, and Telegram channels |
| `lspmcp` | Language server binary paths, arguments, and language/file extension bindings |
| `embedding` | Vector embedding provider and model settings (optional) |
| `speech` | Optional local speech-to-text settings using whisper.cpp |

### Field-by-field reference

The keys below use their YAML names. Omitted fields retain their built-in defaults. Provider API keys can be supplied directly or through an environment variable; environment variables are preferred to avoid storing secrets in `settings.yaml`.

#### Providers and models

| YAML key | Purpose |
| --- | --- |
| `providers[].id`, `providers[].name` | Stable provider ID and display name; `id` is referenced by models and routes. |
| `providers[].base_url` | OpenAI-compatible API base URL. |
| `providers[].api_key`, `providers[].api_key_env` | API key or environment variable containing the key. A directly supplied key takes precedence. |
| `providers[].enabled`, `providers[].is_default` | Enable the provider and mark it as the default provider. |
| `providers[].timeout_ms` | Provider request timeout in milliseconds. |
| `providers[].headers` | Additional HTTP headers sent to the provider. |
| `providers[].retry.max_retries` | Maximum number of retries (default: `3`). |
| `providers[].retry.initial_delay_ms`, `providers[].retry.max_delay_ms` | Initial and maximum retry delays (defaults: `1000` and `30000` ms). |
| `providers[].retry.backoff_multiplier` | Retry delay multiplier (default: `2`). |
| `models[].id`, `models[].provider_id`, `models[].name` | Model ID, owning provider ID, and display name. |
| `models[].api_model` | Optional upstream model name; defaults to the model ID. |
| `models[].context_window` | Model context-window size in tokens. |
| `models[].enabled`, `models[].vision` | Enable the model; set `vision: true` when it accepts image content. |
| `models[].generation.temperature`, `models[].generation.max_tokens` | Sampling temperature and maximum generated tokens. |
| `models[].thinking.enabled` | Enable the provider's reasoning/thinking mode. |
| `models[].thinking.reasoning_effort` | Provider-specific reasoning effort, such as `high` or `max`. |
| `models[].thinking.thinking_type` | Value sent as the provider's thinking type, for APIs that require a non-default value. |

#### Model routes

Route values use `provider-id:model-id` and must refer to enabled entries above.

| YAML key | Purpose |
| --- | --- |
| `model_routes.general` | Model for conversation, writing, translation, and other general tasks. |
| `model_routes.engineering` | Model for coding, debugging, and other engineering tasks. |
| `model_routes.research` | Model for research and information lookup. |
| `model_routes.classifier` | Optional model for task classification. |
| `model_routes.vision` | Optional image-capable model; its model entry must set `vision: true`. |
| `model_routes.fallback` | Fallback model used when a task route cannot be resolved. |

#### Session, logging, and tools

| YAML key | Purpose and default |
| --- | --- |
| `session.timeline_max_file_mb` | Maximum timeline file size in MiB (default: `50`). |
| `log.level` | Log level (default: `info`). |
| `log.console`, `log.file` | Enable console and file logging (defaults: `false`, `true`). |
| `tools.max_file_size`, `tools.max_write_size` | Maximum bytes read from or written to one file (default: `1048576` each). |
| `tools.max_matches`, `tools.max_line_len`, `tools.max_glob_items` | Limits for search matches, returned line length, and glob results (defaults: `100`, `500`, `1000`). |
| `tools.max_multi_write_bytes`, `tools.max_multi_write_files`, `tools.max_replace_edits` | Limits for multi-file writes and replacement edits (defaults: `10485760`, `50`, `50`). |
| `tools.http_allowed_hosts` | Optional WebFetch host allowlist; an empty list allows any host subject to other protections. |
| `tools.http_max_body`, `tools.http_timeout_ms` | WebFetch response-body limit and timeout (defaults: `5242880` bytes and `600000` ms). |
| `tools.http_block_private` | Block private, loopback, and link-local addresses (default: `true`). |
| `tools.shell_block_regexes` | Regular-expression command blocklist. Empty by default, so no commands are blocked by this list. |
| `tools.shell_max_output` | Maximum shell output in bytes (default: `262144`). |
| `tools.web_search_timeout_ms` | Web search timeout (default: `600000` ms). |
| `tools.tavily_api_key`, `tools.tavily_api_key_env` | Tavily key or environment variable; without a key, search uses DuckDuckGo. |
| `tools.image_models[]` | Image-generation model entries; each includes `id`, `name`, `provider`, `enabled`, and `is_default`, plus provider-specific credentials/settings: `secret_id`, `secret_id_env`, `secret_key`, `secret_key_env`, `api_key`, `api_key_env`, `api_base_host`, and `region`. |

#### Agent and integrations

| YAML key | Purpose |
| --- | --- |
| `agent.builtin_mcp_servers`, `external_mcp_servers` | Optional allowlists for built-in and external MCP servers. Omitted means load all; `[]` means load none. |
| `qqbots[]` | QQ account fields: `id`, `name`, `enabled`, `app_id`, `app_secret`, `intents`, `sandbox`, `bind_type`, `bind_agent`, `whitelist_enabled`, and `whitelist`. |
| `wechat_bots[]` | WeChat iLink account fields: `id`, `name`, `enabled`, `bot_token`, `bot_id`, `base_url`, `bot_agent`, `bind_type`, `bind_agent`, `whitelist_enabled`, and `whitelist`. |
| `telegram_bots[]` | Telegram account fields: `id`, `name`, `enabled`, `bot_token`, `bot_id`, `username`, `bind_type`, `bind_agent`, `whitelist_enabled`, and `whitelist`. |
| `embedding.enabled`, `embedding.min_similarity`, `embedding.provider`, `embedding.model_name` | Enable vector embeddings, set the similarity threshold (default: `0.65`), provider type (`none` or `openai`), and model name. Embedding is disabled by default. |
| `embedding.providers[]` | Embedding provider fields: `id`, `name`, `base_url`, `api_key`, `api_key_env`, and `enabled`. |
| `embedding.models[]` | Embedding model fields: `id`, `provider_id`, `name`, `dimension`, `batch_size`, `normalize`, `enabled`, and `is_default`. |
| `lspmcp.servers[]` | Overrides built-in LSP servers by `id`; entries have `command`, `args`, `languages`, `extensions`, and `disabled`. An empty list uses all built-in servers. |
| `speech.enabled`, `speech.model`, `speech.model_dir` | Enable local speech-to-text, select `tiny`, `base`, `small`, or `medium`, and set the model directory (default: `<work-directory>/models`, usually `~/.soloqueue/models`). Disabled by default. |

QQ, WeChat, and Telegram credentials and channel bindings can also be managed from the Web Console. `mcp.json` is a separate file for external MCP server definitions; it is not a `settings.yaml` section.

---

## 2. CLI Command Reference

The primary binary is `soloqueue`. Run `soloqueue --help` to list registered subcommands and options.

### `soloqueue serve`
Starts the HTTP REST, WebSocket, and agent runtime server on `127.0.0.1`.
- `--host`: Listening host (default: `127.0.0.1`).
- `--port, -p`: Listening port (default: `57689`; `0` for random port).
- `--verbose, -v`: Enables verbose stderr logging.

### `soloqueue start`
Starts the backend runtime, Web Console at `/`, and Status UI at `/status/` on one listener. It accepts the same host, port, and verbose flags as `serve`.

### `soloqueue web`
Starts only the standalone Web Console. Use `--backend` to set the backend URL; the default is `http://127.0.0.1:57689`.

### `soloqueue version`
Prints the application version string.

### `soloqueue skills report`
Generates a JSON governance report on installed skills, showing invocation frequency and prompt overhead metrics:
```bash
soloqueue skills report --days 30
```

### ClawHub skill lifecycle

Skill packages are not embedded in SoloQueue and are not managed by its HTTP API. Use the standalone [ClawHub](https://github.com/openclaw/clawhub) CLI against `${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}`. Set `SOLOQUEUE_WORK_DIR` when using a non-default work directory:

```bash
SOLOQUEUE_HOME="${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}"
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills list
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills search "calendar"
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills inspect @owner/slug
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills install @owner/slug
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills update @owner/slug
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills update --all
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills uninstall slug
```

SoloQueue discovers global `SKILL.md` packages from `${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}/skills/` and compatible project packages from `<project>/.claude/skills/`. Discovery supports grouped layouts such as `skills/@user/skill/SKILL.md`, traverses up to six directory levels, and stops below a directory once it contains a recognized entrypoint. The `@user` path is organizational; the skill ID comes from frontmatter `name` or the skill directory name. Within one root, shallower paths win same-ID collisions, then lexical path order. Global `SKILL.md` definitions reload when a skill directory or recognized entrypoint changes; project packages are loaded when an Agent is created. Changes to supporting files do not rebuild the global Skill registry. The Web Console exposes only read-only inspection. Do not use `openclaw` or SoloQueue management endpoints for this lifecycle; use standalone `clawhub` commands instead.

### `soloqueue memory`
Inspects or cleans long-term memory:
```bash
soloqueue memory audit [--db path]
soloqueue memory cleanup --project-root /path/to/project [--apply]
```

### `soloqueue wechat login`
Runs the WeChat iLink QR authentication workflow:
```bash
soloqueue wechat login --id personal --name "Personal WeChat" [--bind-type l1|l2]
```

---

## 3. Data Directory & Backup

The HTTP service has no application authentication or public listener. Put
external authentication, TLS, CORS, and access policy in a user-managed
reverse proxy.

Application data resides in `~/.soloqueue/` (or directory specified by `SOLOQUEUE_WORK_DIR`):

| Path | Purpose |
| --- | --- |
| `settings.yaml` | Application configuration and active settings |
| `mcp.json` | External MCP server definitions |
| `soloqueue.db` | Shared SQLite database (teams, cron, memory) |
| `logs/` | HTTP, application, timeline JSONL, and scheduled task logs |
| `agents/` / `groups/` | User agent templates and team definitions |
| `skills/` | Installed custom skills |

### Consistent Backup Procedure

1. Stop the `soloqueue` server process.
2. Copy the entire work directory (`~/.soloqueue/`) to a backup location.
3. Restart the server process.

> **Note**: Stopping the server before copying allows SQLite WAL checkpoints and timeline JSONL writes to finish before the files are copied.
