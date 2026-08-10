# 记忆与统计

[English: Memory and stats](../../guides/memory-and-stats.md)

我把 SoloQueue 的短期对话记忆、长期记忆、时间线事件和使用统计分开使用。
我让它们回答不同问题，不会把它们当作一份没有区别的历史。

## Memory

- 上下文压缩需要时，我会生成对话摘要。
- 长期记忆使用 SQLite 中的 BM25 搜索和知识图谱。
- 我可以选择启用 Embedding Provider 进行向量搜索；默认不需要远程 Embedding API。
- Timeline JSONL 保存追加式会话事件，用于回放和诊断。

我打开 Settings → Memory 查看 Embedding 和保留相关设置。Memory 和日志可能包含
Prompt、文件路径、工具参数和响应，我会把它们当作私有项目数据。

## Stats

我打开 Stats 查看选定时间范围内的 Token 使用、请求历史和路由分类。它用于运行
反馈，不等同于外部 Provider 的计费事实。

只读 Memory Audit：

~~~bash
soloqueue memory audit
~~~

Legacy Cleanup 会先生成计划，再应用：

~~~bash
soloqueue memory cleanup --project-root /absolute/path/to/project
soloqueue memory cleanup --project-root /absolute/path/to/project --apply
~~~

我应用 Cleanup 时会创建数据库备份并写入 Manifest。我会保留它们，直到 Audit 和抽样检查完成。
