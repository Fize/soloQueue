# 任务路由

[English: Task routing](../routing.md)

我按照工作性质分类请求：

| 任务类型 | 含义 |
| --- | --- |
| general | 对话、写作、翻译、摘要 |
| engineering | 代码、仓库、调试、测试、部署 |
| research | 搜索、时效信息和来源核验 |

## 解析流程

~~~text
用户 Prompt
    │
    ▼
本地快速规则
    │ 明确匹配 ─────────────┐
    │ 不明确                │
    ▼                      ▼
Classifier Model        任务类型
    │                      │
    └──────────┬───────────┘
               ▼
       provider:model + fallback
~~~

我先使用本地规则识别代码、命令、Traceback、路径和研究信号。不明确时，使用
我使用配置的 Classifier，并让后续请求保留前一轮任务类型，从而保持会话上下文。

model_routes 位于 settings.yaml，值使用 provider:model。如果目标不可用，我
我使用 fallback，并在 UI 和 Stats 中记录实际解析状态。

我不把这个分类法当作难度阶梯：简单的代码问题仍是 engineering，复杂的对话仍是 general。
