# Reference Manual

English | [简体中文](zh/reference.md)

This reference manual documents configuration parameters (`settings.yaml`), CLI command line usage, database storage, backup procedures, and security policies.

---

## 1. Configuration Reference (`settings.yaml`)

Configuration settings are loaded from `settings.yaml` located in the active work directory (`~/.soloqueue/` by default; overridden by `SOLOQUEUE_WORK_DIR`). Supported settings are hot-reloaded when modified.

### Minimal Configuration Example

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

### Key Configuration Sections

| Section | Purpose |
| --- | --- |
| `providers` | OpenAI-compatible API endpoint definitions and retries |
| `models` | Model definitions, context windows, generation, and thinking parameters |
| `model_routes` | Task type routing mappings (`general`, `engineering`, `research`, `classifier`, `fallback`) |
| `tools` | File paths, shell execute regex filters, HTTP host allowlists, output limits |
| `agent` | Internal agent tool and MCP server settings |
| `qqbots` / `wechat_bots` | Credentials, bindings, and user whitelists for QQ/WeChat channels |
| `lspmcp` | Language server binary paths, arguments, and language/file extension bindings |
| `embedding` | Vector embedding provider and model settings (optional) |

---

## 2. CLI Command Reference

The primary binary is `soloqueue`. Run `soloqueue --help` for complete subcommand options.

### `soloqueue serve`
Starts the HTTP REST, WebSocket, and agent runtime server on `127.0.0.1`.
- `--port, -p`: Listening port (default: `57647`; `0` for random port).
- `--verbose, -v`: Enables verbose stderr logging.

### `soloqueue start`
Starts the backend runtime, Web Console at `/`, and Status UI at `/status/` on one `127.0.0.1` listener. It accepts the same port and verbose flags as `serve`.

### `soloqueue web`
Starts only the standalone Web Console. Use `--backend` to set the backend URL; the default is `http://127.0.0.1:57647`.

### `soloqueue version`
Prints the application version string.

### `soloqueue skills report`
Generates a JSON governance report on installed skills, showing invocation frequency and prompt overhead metrics:
```bash
soloqueue skills report --days 30
```

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

### Safe Backup Procedure

1. Stop the `soloqueue` server process.
2. Copy the entire work directory (`~/.soloqueue/`) to a secure backup location.
3. Restart the server process.

> **Note**: Always stop the server before copying to ensure SQLite WAL checkpoints complete and timeline JSONL logs are fully flushed.
