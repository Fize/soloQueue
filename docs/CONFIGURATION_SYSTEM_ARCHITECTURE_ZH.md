# Configuration System Architecture

## 概览

配置系统是 SoloQueue 的运行时控制面，负责：

- 定义全局配置结构 `Settings`
- 提供硬编码默认值
- 从 `settings.toml` / `settings.local.toml` 分层加载
- 提供热加载与回调
- 把用户配置转换成运行时可直接消费的结构

它的角色不是单纯“读文件”，而是为整个系统提供一个稳定的 typed configuration service。

## 代码设计

这个包使用泛型 `Loader[T]`，让 load/save/watch 的语义实现一次，同时保持类型安全。上层再通过 `GlobalService` 嵌入 `Loader[Settings]`，提供面向业务的查询接口。

配置合并规则被明确写死：对象递归合并、数组整体替换、缺失字段保留旧值。这让多层配置的行为是可预测的，而不是隐含的。

包内还负责“文件格式”和“运行时结构”之间的转换。例如 `ToolsConfig.ToToolsConfig(...)` 会把毫秒制 timeout 转成 `time.Duration`，并补齐 runtime 默认值。

## 配置结构

完整 schema 在 [internal/config/schema.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/schema.go#L10) 中定义为 `Settings`。

顶层包含：

- `Session`
- `Log`
- `Tools`
- `Providers`
- `Models`
- `Embedding`
- `DefaultModels`

其中：

- `Providers` 管理 provider 级别配置，例如 API key env、base URL、retry
- `Models` 管理模型级别配置，例如 context window、generation、thinking
- `DefaultModels` 负责把 `expert`、`fast` 等角色映射到具体模型

## 默认值层

[internal/config/defaults.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/defaults.go) 中的 `DefaultSettings()` 提供系统默认配置，包括：

- session / log 默认值
- tools 限制默认值
- 默认 provider 与 model catalog
- 默认角色模型映射

这个设计保证系统在没有用户配置文件时也能启动。

## Loader 机制

核心加载器在 [internal/config/loader.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/loader.go#L17)。

它提供：

- 类型化快照
- 并发安全读写
- 原子保存
- 变更回调
- 文件监听

### 分层加载顺序

加载优先级是：

`defaults -> paths[0] -> paths[1] -> ...`

对全局配置服务而言，这些 path 是：

- `settings.toml`
- `settings.local.toml`

因此本地覆盖可以自然叠加在主配置之上。

### Snapshot 语义

`Get()` 返回当前配置快照。调用方拿到的是值语义，不会直接持有 loader 的内部共享状态。

### Save / Set

`Set(fn)` 会：

1. 基于当前值生成新配置
2. 持久化到 primary path
3. 持久化失败时回滚内存值

底层 `saveTo(...)` 采用 `.tmp + rename` 的原子写模式。

## 合并语义

[internal/config/merge_toml.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/merge_toml.go) 中的 `MergeTOML(...)` 定义了分层配置最核心的规则：

- 对象字段递归合并
- 数组整体替换
- 缺失字段保持旧值
- 标量直接覆盖

这意味着：

- 嵌套对象可以局部覆盖
- provider/model 数组如果想改，通常需要写完整替换集

## 热加载

`Watch()` 在 [internal/config/loader.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/loader.go#L190) 中实现。它监听的是父目录而不是单个文件，这样可以兼容：

- 文件写入
- 文件首次创建
- 编辑器 rename-save 模式

变更会经过 `scheduleReload()` 做 200ms debounce，避免一次保存触发多次 reload。

## GlobalService

[internal/config/service.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/service.go#L9) 中的 `GlobalService` 是面向应用的 facade，在 `Loader[Settings]` 基础上增加了业务查询：

- `DefaultProvider()`
- `ProviderByID()`
- `ModelByID()`
- `ModelByProviderID()`
- `DefaultModelByRole()`

这样 runtime 不需要自己遍历 `Settings` 结构。

## Role 模型解析

`DefaultModelByRole(...)` 在 [internal/config/service.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/service.go#L92) 中实现，解析顺序是：

1. 角色专属配置
2. `Fallback`
3. 硬编码默认值

再把 `provider:model` 解析成实际的 `LLMModel`。

这是配置系统与任务路由、agent factory 之间的重要桥梁。

## 到运行时配置的转换

[internal/config/tools_convert.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/tools_convert.go#L9) 中的 `ToolsConfig.ToToolsConfig(...)` 负责把用户配置转换为 runtime 使用的 `tools.Config`。

处理内容包括：

- 毫秒到 `time.Duration`
- 0 值回退到默认值
- 将 allowed dirs 合并进 runtime 注入的目录集合

这样 `tools` 包不需要理解 TOML 文件层面的表示方式。

## 与启动流程的关系

应用通过 `config.Init(workDir)` 初始化配置服务，见 [internal/config/service.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/service.go#L169)。

之后 `cmd/soloqueue/main.go` 会消费这些配置：

- provider 配置 -> DeepSeek client
- tools 配置 -> `tools.Config`
- model 配置 -> default model 与 factory resolver

所以配置系统实际上是 runtime wiring 的源头之一。

## 关键文件

- [internal/config/schema.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/schema.go)
- [internal/config/defaults.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/defaults.go)
- [internal/config/loader.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/loader.go)
- [internal/config/merge_toml.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/merge_toml.go)
- [internal/config/service.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/service.go)
- [internal/config/tools_convert.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/tools_convert.go)
