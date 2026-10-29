# Changelog

Notable changes are recorded here for each release in English and Simplified Chinese.

## [Unreleased]

## [0.3.0] - 2026-10-29

### Added

- Added native Windows support, including PowerShell command execution, process cancellation, language servers, and speech transcription.
- Added an optional local Laya ONNX task classifier with setup controls and classification statistics.
- Added execution traces and delegation history, along with storage retention controls.
- Added WeCom bot management and settings for search tools and image models.

### Improved

- Integrated local inference into the main executable and kept local workers warm briefly when disabled.
- Improved chat layout, streaming navigation, delegation details, modal accessibility, and per-session composer drafts.
- Refreshed WeChat notification routes while active conversations continue.

### Compatibility notes

- Local inference downloads its model and ONNX Runtime when needed. Platform and hardware acceleration availability depend on the runtime and device.

### 新增

- 新增 Windows 原生支持，包括 PowerShell 命令执行、进程取消、语言服务器和语音转写。
- 新增可选的本地 Laya ONNX 任务分类器、设置控制和分类统计。
- 新增执行轨迹、委派历史和存储保留控制。
- 新增企业微信机器人管理，以及搜索工具和图像模型设置。

### 改进

- 将本地推理集成到主程序，并在停用后短暂保留本地工作进程以便复用。
- 改进聊天布局、流式导航、委派详情、弹窗无障碍支持和按会话保存的输入草稿。
- 活跃会话期间刷新微信通知路由。

### 兼容性说明

- 本地推理在需要时下载模型和 ONNX Runtime；平台与硬件加速可用性取决于运行库和设备。

## [0.2.4] - 2026-10-21

### Added

- Added lazy language-server startup and Vue 3, React JSX, and React TSX language support, including diagnostics, hover, navigation, references, and rename operations.

### Fixed

- Fixed L2 team groups and leader names staying stale after teams are installed or reloaded while the server is running.
- Fixed language-server clients remaining unavailable after an unexpected process exit; clients now restart on the next request.

### Improved

- Stop idle language-server clients after ten minutes and reuse active clients across requests.

### 新增

- 新增语言服务器懒启动，并支持 Vue 3、React JSX 和 React TSX，包括诊断、悬停提示、代码跳转、引用查找和重命名。

### 修复

- 修复服务运行期间安装或重载团队后，L2 团队分组和负责人名称仍显示旧数据的问题。
- 修复语言服务器进程异常退出后客户端无法继续使用的问题；下次请求会重新启动客户端。

### 改进

- 语言服务器空闲十分钟后自动退出，并在活跃期间复用客户端。

## [0.2.3] - 2026-10-21

### Added

- Added local ONNX embeddings using the pinned official `multilingual-e5-small` model. First activation downloads the model and ONNX Runtime with SHA-256 verification; cached artifacts support offline restarts. Available GPU acceleration is preferred automatically, with CPU fallback.
- Added isolated, resumable memory indexing and settings-page preparation/indexing status, retry, cancellation, and index rebuilding. Raw memories are preserved; changing remote model profiles isolates their vectors without automatically reindexing through a paid API.
- Added four mutually exclusive long-term-memory modes: OFF (default), BM25 + knowledge graph, BM25 + knowledge graph + local ONNX, and BM25 + knowledge graph + remote API.
- Added native worker packaging for macOS ARM64, Linux x86_64, and Windows x86_64, plus a three-platform CI gate for real CPU/reference parity, exact tokenization and truncation, automatic-provider fallback, managed indexing, and offline restart.

### Fixed

- Split chat replies at queued-input boundaries and made Stop cancel all message sources in the current session.
- Restored WeChat proactive notifications using saved credentials and established recipient routes after restart. A valid account binding and route are still required; acceptance by WeChat does not guarantee delivery.
- Fixed embedding model/provider activation on hot reload, kept healthy workers running after queued-query cancellation or rejected input, and corrected runtime-library discovery and backend state after failed CPU fallback.
- Preserved executable permissions in cross-platform release archives.

### Improved

- Persisted context-compaction status across history reloads, serialized compaction runs, and added a 16K-token output limit, bounded retries, and a five-minute timeout. Completion notices dismiss after five seconds.
- Replaced forced JSON clarification responses with concise natural-language questions for unresolved blocking information.
- Made OFF drain admitted memory operations before returning, so already-bound L1/L2 tools cannot continue reading or writing long-term memory after OFF completes.

### Compatibility notes

- `embedding.enabled=false` now disables the entire durable long-term-memory engine, including BM25, knowledge graph, and vectors. Previously, remote embeddings could remain active with this flag false. Select an enabled mode to resume; stored memories, indexes, provider credentials, and model settings are preserved. Short-term conversation memory and event timeline logging are unchanged.
- The macOS Intel application remains available, but its local ONNX mode is unsupported by the pinned Runtime package. Linux ARM64 Docker packaging uses CPU inference and is outside the three-runner native gate.
- CUDA and DirectML acceleration depend on compatible hardware and runtime dependencies. Provider-capable packages and CPU fallback are tested; hosted CI does not establish CUDA or DirectML GPU hardware execution.

### 新增

- 新增基于固定版本官方 `multilingual-e5-small` 模型的本地 ONNX 嵌入。首次启用时下载模型与 ONNX Runtime，并校验 SHA-256；缓存完成后支持离线重启。默认自动优先使用可用的 GPU 加速，不可用时回退 CPU。
- 新增隔离、可恢复的记忆索引，以及设置页中的准备和索引状态、重试、取消与重建索引操作。保留原始记忆；切换远程模型配置时隔离向量，不自动调用付费 API 重建索引。
- 新增四项互斥的长期记忆模式：关闭（默认）、BM25 + 知识图谱、BM25 + 知识图谱 + 本地 ONNX、BM25 + 知识图谱 + 远程 API。
- 新增 macOS ARM64、Linux x86_64 和 Windows x86_64 原生 worker 打包，以及覆盖真实 CPU/参考模型一致性、精确分词与截断、自动执行后端回退、托管索引和离线重启的三平台 CI 门禁。

### 修复

- 按排队输入边界拆分聊天回复，并使“停止”取消当前会话中所有消息来源的任务。
- 恢复微信主动通知：重启后复用已保存的凭证与已建立的接收方路由。仍需有效的账号绑定和路由；微信接口受理不代表消息必定送达。
- 修复嵌入模型和提供商启用状态的热重载；排队查询取消或输入被拒绝时保留健康 worker；修正 Runtime 库定位及 CPU 回退失败后的执行后端状态。
- 保留跨平台发布归档中的可执行权限。

### 改进

- 持久化上下文压缩状态并在加载历史时恢复，串行执行压缩，增加 16K token 输出上限、有界重试和五分钟超时；完成提示在五秒后自动消失。
- 不再强制使用 JSON 请求澄清；对无法自行确定的阻塞信息，改用简洁的自然语言提问。
- 关闭长期记忆时等待已获准操作完成，保证关闭返回后，既有 L1/L2 工具不再继续读取或写入长期记忆。

### 兼容性说明

- `embedding.enabled=false` 现在关闭整个长期记忆引擎，包括 BM25、知识图谱和向量检索。此前该标志为 false 时，远程嵌入仍可能运行。选择任一启用模式即可恢复；已存记忆、索引、提供商凭证和模型设置均保留。短期会话记忆和事件时间线日志不受影响。
- macOS Intel 应用仍可使用，但固定版本的 Runtime 包不支持其本地 ONNX 模式。Linux ARM64 Docker 打包使用 CPU 推理，不在三平台原生门禁范围内。
- CUDA 和 DirectML 加速取决于兼容硬件及运行依赖。测试覆盖支持相应执行后端的 Runtime 包与 CPU 回退，但托管 CI 不证明 CUDA 或 DirectML GPU 硬件上的执行。

## [0.2.2] - 2026-10-16

### Fixed

- Prevented config reloads from publishing partially written settings files.

### Added

- Added persisted session inspector state, session change tracking, and token/cache usage statistics.
- Added conditional file listing and caching in the session file panel.
- Added provider names to the agent working indicator.

### Improved

- Improved chat streaming reconciliation across refreshes, reconnects, and cancelled requests.
- Improved file tree loading so cached directories refresh silently and closed branches load only when opened.
- Improved ordering and ownership of queued, stopped, and in-flight chat responses.

### 新增

- 新增会话检查器状态持久化、会话变更追踪，以及令牌和缓存用量统计。
- 为会话文件面板增加条件请求和文件列表缓存。
- 在 Agent 工作状态指示器中展示模型提供方名称。

### 修复

- 防止配置热重载在文件写入完成前发布不完整的设置。

### 改进

- 改进聊天流在刷新、重连和取消请求后的状态同步。
- 改进文件树加载：缓存目录静默刷新，关闭的目录仅在打开时加载。
- 改进排队、停止及进行中的聊天响应顺序和归属处理。

## [0.2.1] - 2026-10-13

### Fixed

- Fixed chat streaming state reconciliation after refresh and reconnect.
- Fixed cancelled requests leaving stale in-flight state or orphaned processes.
- Fixed plan panel refresh and restoration in the Web Console.
- Hid automatic context-compaction summaries from chat output.
- Grouped adjacent delegation segments consistently in chat rendering.

### 修复

- 修复聊天流在刷新和重连后的状态同步问题。
- 修复取消请求后残留进行中状态或孤儿进程的问题。
- 修复 Web Console 中计划面板刷新和恢复问题。
- 隐藏自动上下文压缩摘要，避免其显示在聊天内容中。
- 修复聊天渲染中相邻委派片段的分组问题。

## [0.2.0] - 2026-10-12

### Added

- Added ChatGPT subscription sign-in as a model provider.

### 新增

- 新增通过 ChatGPT 订阅账号登录，并将其作为模型提供方使用。
