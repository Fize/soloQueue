# 项目与会话

[English: Projects and sessions](../../guides/projects-and-sessions.md)

我把 Agent 运行时放在一个本地工作目录中，同时让每个项目指向仓库或其他
绝对文件系统路径。这样我可以在多个项目之间复用同一套 Team 和 Model。

## 项目

1. 我打开 Settings → Projects。
2. 我使用名称和绝对路径添加项目。
3. 我在创建 Chat 或 Workflow Run 时选择项目。

我把项目路径当作执行范围，而不只是一个标签。在允许写入、Shell、网络或委派操作前，
我会重新检查这个路径。

## Chat 会话

- Chat 用于创建普通的项目会话。
- Assistant 暴露我长期使用的主助手会话。
- Session Tree 让我返回已有对话。
- 当前请求可以展示解析出的任务类型和模型。

我依赖服务端按会话串行处理请求，并通过 WebSocket 向桌面端推送进度。服务重启后，
我仍能保留持久化历史和本地元数据，但需要重新建立内存中的认证和活动连接。

## 工作目录与项目目录

我默认使用 ~/.soloqueue/ 作为工作目录，其中包含设置、Agent/Team 文件、SQLite、日志、
Memory、Plan 和生成的 Artifact。项目路径独立存在，通常应当是我明确允许
Agent 检查或修改的已有目录。

对于委派或 Workflow 任务，我会使用明确的项目路径，不依赖模型生成的路径，
也不会把本意是仓库的路径放进 ~/.soloqueue/。

## 我的会话习惯

- 先用只读请求确认项目。
- 为修改任务写明目标和验收标准。
- 把无关仓库分成不同项目。
- 自己检查最终 diff 和生成的 Artifact。
