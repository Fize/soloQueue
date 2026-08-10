# 配置参考

[English: Configuration reference](../../reference/configuration.md)

我从当前工作目录的 settings.yaml 读取配置。默认工作目录是 ~/.soloqueue/，
SOLOQUEUE_WORK_DIR 可以覆盖它。我先加载编译时默认值，再由 settings.yaml 覆盖，
并监听支持热加载的变化。

## 最小 Provider 配置

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

我尽量使用 api_key_env 而不是直接填写 api_key。路由值是 provider:model，
两者都必须对应启用条目。

## 顶层区段

| 区段 | 用途 |
| --- | --- |
| auth | 非回环请求的 Basic Auth |
| session | Timeline 文件限制 |
| log | 控制台和文件日志 |
| tools | 文件、Shell、HTTP、搜索和图片工具策略 |
| providers | OpenAI-compatible LLM Endpoint 和重试 |
| models | Model Catalog、生成、思考、上下文和视觉 |
| model_routes | general、engineering、research、classifier、vision、fallback |
| embedding | 可选的长期向量搜索 Provider/Model |
| agent | 内置和外部 MCP 白名单 |
| qqbots | QQ Bot 凭据、Intent、绑定和白名单 |
| wechat_bots | 微信 iLink 凭据、绑定和白名单 |
| lspmcp | LSP 工具定义 |
| simulation | Generative Agent Simulation 默认值 |
| speech | 可选语音模型设置 |

## 编辑安全

我优先使用 Settings 页面修改支持的字段。直接编辑 YAML 时，我保留备份、
不改变 Provider/Model ID，并关注服务日志中的重载或校验错误。settings.yaml
是包含 Secret 的文件，我会保护它。
