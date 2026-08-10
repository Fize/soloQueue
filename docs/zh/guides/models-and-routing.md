# 模型与路由

[English: Models and routing](../../guides/models-and-routing.md)

我按照任务性质而不是难度阶梯来路由请求。当前任务类型是：

| 任务类型 | 常见请求 |
| --- | --- |
| general | 对话、写作、翻译、摘要 |
| engineering | 代码、仓库、调试、测试、部署 |
| research | 搜索、时效信息和来源核验 |

## 配置 Provider 和 Model

我打开 Settings → Models，添加 OpenAI-compatible Provider，注册 Model 并
启用需要的条目。Provider 凭据可以填写在设置中，也可以通过环境变量提供。

我为每个 Model 配置本地 ID、Provider ID、可选的 API Model 名称、上下文窗口、
生成参数和思考参数，并使用 provider:model 作为路由引用格式。

## 配置任务路由

settings.yaml 中的 model_routes 把任务类型映射到模型：

~~~yaml
model_routes:
  general: deepseek:deepseek-v4-flash-thinking
  engineering: deepseek:deepseek-v4-flash-thinking-max
  research: deepseek:deepseek-v4-flash-thinking-max
  classifier: deepseek:deepseek-v4-flash
  fallback: deepseek:deepseek-v4-flash
~~~

我会先使用本地快速分类规则识别代码、命令、Traceback 和研究信号。输入不明确
时，可以使用配置的 Classifier Model。如果路由不可用，就使用 fallback，并在
界面记录实际解析结果。

## 检查解析结果

我用 Chat 的活动请求指示器查看可用的任务类型和模型，用 Stats 查看使用和路由
历史。如果结果异常，我会检查同一会话的前一个请求，因为后续请求会保留任务上下文。

## 我的模型检查清单

- Provider 已启用且服务端能读取 Key。
- Model 的 provider_id 对应已启用 Provider。
- 每个路由都指向启用的 provider:model。
- 上下文窗口足够支撑目标 Workflow。
- Provider 支持所选择的 thinking 字段。
