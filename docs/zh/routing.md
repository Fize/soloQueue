# 任务路由

[English](../routing.md) | 简体中文

本文说明当前运行时如何分类交互请求，并将分类结果转换为模型参数。路由负责选择模型，不负责决定是否把任务委派给其他 Agent。

## 路由流程

```mermaid
flowchart TD
  Input[Session 请求 + 上一轮任务类型 + 上下文历史]
  Local[高精度本地分类器]
  LLM[本地规则无法判断时调用 LLM 分类器]
  Fallback[沿用上一轮类型，否则 general]
  Resolve[从配置中解析已启用模型]
  Override[请求级模型覆盖]
  Agent[Agent 使用选定模型执行]
  Input --> Local
  Local -->|明确且无歧义| Resolve
  Local -->|无匹配 / 有歧义| LLM
  LLM -->|有效 JSON 类型| Resolve
  LLM -->|错误或响应无效| Fallback
  Fallback --> Resolve
  Resolve --> Override --> Agent
```

交互请求从 `Session.AskStream` 进入。Session 将新请求、上一轮任务类型和当前上下文内容交给路由器。路由成功后，模型参数以请求级覆盖写入该请求的 Context；Agent 仅对当前请求使用这个覆盖，不会修改其他请求的模型配置。

## 任务类型不等于难度级别

分类器返回以下三种任务类型之一：

| 类型 | 分类意图 |
| --- | --- |
| `general` | 日常对话、解释、写作、翻译，或总结用户提供的材料。 |
| `engineering` | 仓库与代码工作、调试、测试、数据库、API、自动化、部署或技术实现。 |
| `research` | 外部搜索、最新信息、来源核查、比较分析或需要引用来源的工作。 |

分类依据是任务性质与信息来源，不是任务难度或所需推理强度。本地查代码或日志属于 engineering；查找最新外部来源属于 research。是否包含图片也不会产生单独的任务类型。

## 路由方式的演进

早期曾尝试按任务难易程度选择模型，但实际使用中，难度往往难以稳定判断，也不容易直接对应到合适的模型。相比之下，按任务类型区分模型更贴近日常使用习惯：用户会根据任务是日常交互、工程操作还是外部调研，选择更合适的模型。因此，SoloQueue 目前按任务类型分类，并为不同类型配置对应模型。

当前语义分类仍由大模型完成：本地规则能够明确判断时会直接分类，遇到模糊请求时再调用配置的分类模型。后续会关注 JEV 模型的进展，并评估其是否适合用于任务分类；目前这仍是后续考察方向。

## 分类阶段

### 本地快速分类

`internal/router/fasttrack.go` 中的规则以谨慎为原则。代码块、Stack Trace、Shell/构建命令等属于强信号。文件路径需要与工程类信号同时出现，才构成强证据；技术实体需要与动作或缺陷信号结合。本地规则也识别明确的 research 和 general 表达。

只有证据明确指向一种类型时，本地分类器才直接返回结果。信号冲突或证据较弱时，它会返回“未匹配”，交给语义分类，而不是强行猜测。分类结果还会附带来源和原因码，供运行日志观察。

### LLM 回退

启用 LLM 分类且本地规则未能判断时，`internal/router/llm_classifier.go` 会向配置的分类模型发送有界请求。输入包括当前请求、是否包含图片的元信息，以及最多六条近期 user/assistant 消息，总计最多 4,096 字节。分类请求有五秒超时，使用零温度、较小的输出上限、关闭 thinking，并要求 JSON 响应。

只有包含受支持任务类型的有效 JSON 才会被接受。Provider 错误、JSON 格式错误或类型无效时，回退到上一轮有效任务类型；如果没有，则回退为 `general`。失败详情以有限长度写入日志，不会作为面向用户的回答输出。

## 模型解析

分类与模型选择互不相关。`internal/router/router.go` 只返回任务类型，模型由配置服务按唯一默认值解析：

1. `settings.yaml` 中的 `model_routes.default`。
2. 其 Provider 与 Model 均已启用时，使用代码内置默认值（`deepseek:deepseek-v4-flash-thinking`）。

只有 Provider 和模型都启用时，配置引用才有效。对话、定时任务和辅助分类器都使用这同一个模型；分类调用强制非思考模式。图片转写优先使用当前模型的 `vision: true`，否则使用第一个已启用的视觉模型。

会话可以覆盖该默认值。选择保存在会话的 `meta.json`（`model`）中，因此重启后仍有效，并对共享该会话的所有入口生效：Web 控制台、QQ、微信、企微和 Telegram。优先级为 会话选择 → `model_routes.default` → 代码内置默认值。若所选模型的 Provider 或模型之后被禁用，会回落到默认模型并记录一条警告，而不是让本轮失败。渠道桥接提供 `/model` 用于查看或修改（`/model <provider:model> [low|high|max]`、`/model auto`）；Web 控制台通过 `PUT /api/session/model`（L1）和 `PUT /api/session/l2/{id}/model`（L2）写入同一份状态。辅助任务（压缩、摘要、人格反思、记忆抽取）和定时任务始终使用配置的默认模型。

任务类型只作为标签流转：用于后续轮次沿用、写入用量指标，并随请求事件发布，但不再决定模型。`RouteDecision` 已移除。

分类失败时，Session 记录错误并继续执行，本轮只是缺少任务标签，仍使用默认模型。启动阶段若没有任何已启用模型可解析，运行时构建直接失败，不会在无模型的情况下启动。

## 运行时装配与状态

- `internal/runtime/build_agent.go` 根据当前设置和 LLM 客户端构造默认分类器与路由器。
- `internal/session/session.go` 负责逐请求分类、请求级模型覆盖和上一轮任务类型连续性。
- 路由状态保存在活动 Session 中。持久化的 L2 Session 还会将最近任务类型合并写入 Session 元数据，使重启后仍能保留后续对话分类上下文。
- 分类器 Provider/模型更新可通过 Router 的更新方法应用，无须替换分类器抽象。

实现与回归测试位置：[`internal/router`](../../internal/router)、[`internal/tasktype`](../../internal/tasktype)、[`internal/config`](../../internal/config)、[`internal/session`](../../internal/session)。
