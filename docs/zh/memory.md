# 记忆子系统

[English: Memory subsystem](../memory.md)

我把对话摘要、长期记忆和 Timeline 事件分开处理。

## 短期对话记忆

我使用 internal/memory/conversation 在上下文压缩需要时生成 LLM 驱动的摘要，并把摘要保存
在工作目录中，用来恢复长对话，而不必重新回放每条消息。

## 长期记忆

我使用 internal/memory/engine 作为 SQLite 长期 Memory 的入口，让它组合 BM25 全文搜索和
知识图谱，也可以在启用 Embedding Provider 时融合向量搜索。我的默认配置不需要远程
Embedding API。

## Timeline

我使用 internal/memory/timeline 写入追加式 JSONL 事件，用于回放和诊断，并确保 System Prompt
不会进入用户 Timeline。

我会先运行只读 Audit，再应用可逆 Cleanup，并在此之前备份 SQLite 和日志。
