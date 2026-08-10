# Configuration reference

> 中文：[配置参考](../zh/reference/configuration.md)

I load settings from settings.yaml in the active work directory. My default
work directory is ~/.soloqueue/; SOLOQUEUE_WORK_DIR overrides it. I load
compiled defaults first and let settings.yaml override them. The settings
service watches the file and hot-reloads supported changes.

## Minimal provider setup

~~~yaml
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
~~~

I use api_key_env instead of api_key when possible. I use provider:model as a
route value, and both IDs must refer to enabled entries.

## Top-level sections

| Section | Purpose |
| --- | --- |
| auth | Basic Auth for non-loopback requests |
| session | Timeline file limits |
| log | Console and file logging |
| tools | File, shell, HTTP, search, and image tool policies |
| providers | OpenAI-compatible LLM endpoints and retries |
| models | Model catalog, generation, thinking, context, and vision |
| model_routes | general, engineering, research, classifier, vision, fallback |
| embedding | Optional long-term vector search provider/model |
| agent | Built-in and external MCP allowlists |
| qqbots | Tencent QQ Bot credentials, intents, binding, and allowlists |
| wechat_bots | WeChat iLink credentials, binding, and allowlists |
| lspmcp | LSP-backed MCP server definitions |
| simulation | Generative-agent simulation defaults |
| speech | Optional speech model settings |

## Authentication

~~~yaml
auth:
  user: soloqueue
  password: replace-with-a-long-random-password
~~~

I can use the equivalent environment variables SOLOQUEUE_AUTH_USER and
SOLOQUEUE_AUTH_PASSWORD. I document them in [Remote access](../operations/remote-access.md).

## Tool policy

I use the tools section for read and write limits, HTTP allowed hosts and
private-network blocking, shell block and confirmation regexes, shell output
limits, web-search timeout, and image model credentials. Zero or empty values
may mean “use the compiled default”; I check the current schema before relying
on a boundary for a new deployment.

## MCP file

MCP server definitions live separately in ~/.soloqueue/mcp.json. The standard
shape is a mcpServers map with command, args, transport, env, and enabled
fields. See [Skills and MCP](../guides/skills-and-mcp.md).

## Editing safely

I prefer the Settings screens for supported fields. When I edit YAML directly,
I keep a backup, preserve provider/model IDs, and watch the server log for
reload or validation errors. I treat settings.yaml as a secret-bearing file.
