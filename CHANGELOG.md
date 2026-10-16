# Changelog

Notable changes are recorded here for each release in English and Simplified Chinese.

## [Unreleased]

## [0.2.2] - 2026-10-16

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
