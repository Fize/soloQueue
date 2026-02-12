# Advanced Memory Features Design (Phase 3)

**Version**: 1.0  
**Status**: Design Review  
**Author**: SoloQueue Architecture Team  
**Last Updated**: 2026-02-08

---

## 1. Executive Summary

This document specifies the design for **Phase 3: Advanced Intelligence** features in the SoloQueue memory architecture:

1. **L3 Semantic Memory**: Vector database for long-term knowledge retrieval
2. **Automated Session Summarization**: LLM-driven conversation compression

These features transform SoloQueue from a stateless execution engine into a **learning system** that accumulates and leverages knowledge over time.

---

## 2. L3 Semantic Memory (Vector Database)

### 2.1 Goals

**Primary Objective**: Enable agents to **learn from past experiences** and retrieve relevant solutions from historical sessions.

**Use Cases**:
- **Problem-Solving**: "We solved a similar bug 2 weeks ago, let me check..."
- **Code Reuse**: Find past implementations of similar features
- **Knowledge Accumulation**: Build organizational knowledge over time
- **Context Augmentation**: Inject relevant past context into current tasks

### 2.2 Technology Selection

#### Option A: **ChromaDB** (Recommended)

**Pros**:
- ✅ Pure Python, embeddable (no external service)
- ✅ Persistent local storage (SQLite-based)
- ✅ Built-in embedding support (sentence-transformers)
- ✅ Simple API, minimal dependencies
- ✅ Production-ready, actively maintained

**Cons**:
- ⚠️ Not suitable for massive scale (millions of vectors)

**Decision**: **Use ChromaDB** for initial implementation due to simplicity and local-first philosophy.

#### Alternative: FAISS, Pinecone, Weaviate (Future)

### 2.3 Architecture Design

```
┌─────────────────────────────────────────────────────┐
│                  Agent Execution                    │
└─────────────────┬───────────────────────────────────┘
                  │
                  ▼
         ┌────────────────┐
         │ MemoryManager  │
         └────────┬───────┘
                  │
    ┌─────────────┼─────────────┐
    │             │             │
    ▼             ▼             ▼
┌────────┐  ┌──────────┐  ┌──────────────┐
│Session │  │Artifact  │  │SemanticStore │ ← NEW
│Logger  │  │  Store   │  │ (ChromaDB)   │
└────────┘  └──────────┘  └──────────────┘
                                  │
                    ┌─────────────┼─────────────┐
                    ▼             ▼             ▼
              ┌──────────┐  ┌─────────┐  ┌─────────┐
              │Embedding │  │Vector   │  │Metadata │
              │  Model   │  │  Index  │  │  Store  │
              └──────────┘  └─────────┘  └─────────┘
```

### 2.4 Data Model

#### **Semantic Memory Entry**

Each entry represents a **knowledge fragment** extracted from sessions:

```python
class MemoryEntry(TypedDict):
    id: str                    # UUID
    content: str               # Text content (what to embed)
    embedding: list[float]     # Vector (384-dim for all-MiniLM)
    metadata: dict[str, Any]   # Structured metadata
```

#### **Metadata Schema**

```python
{
    "type": "solution" | "error" | "pattern" | "decision",
    "session_id": str,
    "agent_name": str,
    "timestamp": str,
    "tags": list[str],
    "context": {
        "task_description": str,
        "outcome": "success" | "failure",
        "tool_calls": list[str],
        "artifact_ids": list[str]
    }
}
```

### 2.5 Embedding Strategy

#### **What to Embed?**

Extract **meaningful knowledge fragments** from sessions:

1. **Successful Solutions** (type: `solution`)
   - Task description + final output
   - Example: "How I implemented user authentication with JWT"

2. **Error Patterns** (type: `error`)
   - Error message + resolution
   - Example: "MemoryError when processing large files → solution: streaming"

3. **Design Decisions** (type: `decision`)
   - Context + rationale + outcome
   - Example: "Why we chose SQLite over PostgreSQL for artifacts"

4. **Code Patterns** (type: `pattern`)
   - Reusable implementations
   - Example: "How to implement retry logic with exponential backoff"

#### **When to Embed?**

**Trigger 1: Session End** (Automatic)
- Extract key insights from completed session
- Embed summaries (see Section 3)

**Trigger 2: Manual Tagging** (User-Driven)
- User marks specific interactions as "important"
- Immediate embedding

**Trigger 3: Batch Processing** (Scheduled)
- Nightly job processes unindexed sessions
- Find high-value knowledge (e.g., long debugging sessions that succeeded)

### 2.6 Retrieval API

#### **Primary Method: Semantic Search**

```python
class SemanticStore:
    def search(
        self,
        query: str,
        top_k: int = 5,
        filter_metadata: dict[str, Any] | None = None
    ) -> list[MemoryEntry]:
        """
        Semantic search over knowledge base.
        
        Args:
            query: Natural language query
            top_k: Number of results
            filter_metadata: Filter by metadata (e.g., agent_name, tags)
        
        Returns:
            Top-k most relevant memory entries
        """
        ...
```

**Example Usage**:

```python
# Agent is stuck on a task
results = semantic_store.search(
    query="How to handle database connection pooling in Python",
    top_k=3,
    filter_metadata={"type": "solution", "outcome": "success"}
)

# Inject results into context
for entry in results:
    context += f"\n[Past Solution]\n{entry['content']}\n"
```

#### **Secondary Method: Hybrid Search**

Combine vector similarity + metadata filtering + keyword matching for best results.

### 2.7 Storage Layout

```
.soloqueue/
├── semantic/
│   ├── chroma.db              # ChromaDB persistent storage
│   ├── embeddings.cache       # LRU cache for frequently accessed embeddings
│   └── index/                 # Vector index files
└── state.db                   # Link semantic IDs to session IDs
```

### 2.8 Performance Considerations

| Metric                | Target                | Strategy                            |
| --------------------- | --------------------- | ----------------------------------- |
| **Search Latency**    | < 100ms               | In-memory index, quantized vectors  |
| **Embedding Latency** | < 50ms/item           | Local model (sentence-transformers) |
| **Index Size**        | < 1GB for 10K entries | Use compact embeddings (384-dim)    |
| **Memory Usage**      | < 500MB               | Lazy loading, LRU cache             |

---

## 3. Automated Session Summarization

### 3.1 Goals

**Primary Objective**: Compress long session logs into **concise, searchable summaries** for L3 indexing.

**Benefits**:
1. **Reduce embedding cost**: Summarize 1000-line session → 200-word summary
2. **Improve search quality**: Well-written summaries are more semantically rich
3. **Human readability**: Quickly understand what happened in past sessions
4. **Knowledge extraction**: Identify key decisions, errors, and solutions

### 3.2 Summarization Pipeline

```
┌────────────────┐
│ Session Ends   │
└───────┬────────┘
        │
        ▼
┌──────────────────────┐
│ Trigger Summarizer   │ (Async job)
└──────────┬───────────┘
           │
           ▼
┌──────────────────────────────┐
│ Load Session JSONL           │
│ - Agent interactions         │
│ - Tool calls/outputs         │
│ - Errors                     │
└──────────┬───────────────────┘
           │
           ▼
┌──────────────────────────────┐
│ Extract Key Information      │
│ - Task description           │
│ - Major decisions            │
│ - Errors + resolutions       │
│ - Final outcome              │
└──────────┬───────────────────┘
           │
           ▼
┌──────────────────────────────┐
│ LLM Summarization            │
│ Model: gpt-4o-mini           │
│ Prompt: Structured template  │
└──────────┬───────────────────┘
           │
           ▼
┌──────────────────────────────┐
│ Generate Summary             │
│ - 200-500 words              │
│ - Markdown formatted         │
│ - Sections: Goal, Actions,   │
│   Outcome, Key Learnings     │
└──────────┬───────────────────┘
           │
           ▼
┌──────────────────────────────┐
│ Save Summary                 │
│ 1. sessions/{id}/summary.md  │
│ 2. Embed → SemanticStore     │
└──────────────────────────────┘
```

### 3.3 Summarization Prompt Template

```markdown
# System Prompt for Session Summarizer

You are a technical documentation specialist. Your task is to summarize a software development session based on the interaction logs provided.

## Input Format
- Agent: Who performed the task
- Task: What the user requested
- Interactions: Chronological log of agent actions, tool calls, and outputs
- Outcome: Final result (success/failure)

## Output Format
Produce a structured summary in the following format:

### Session Summary: {task_title}

**Agent**: {agent_name}  
**Duration**: {start_time} - {end_time}  
**Outcome**: ✅ Success / ❌ Failure

#### 🎯 Goal
A one-sentence description of what the user wanted to achieve.

#### 🔧 Actions Taken
- Bullet points of major steps (max 5-7 items)
- Focus on decisions, not every single tool call

#### 📊 Outcome
What was the final result? What artifacts were produced?

#### 💡 Key Learnings
- Errors encountered and how they were resolved
- Design decisions and rationale
- Reusable patterns or solutions

**Tags**: `{tag1}`, `{tag2}`, `{tag3}`
```

### 3.4 Summarizer Implementation

```python
class SessionSummarizer:
    def __init__(self, llm_model: str = "gpt-4o-mini"):
        self.llm = ModelAdapterFactory.create(llm_model)
    
    def summarize(self, session_id: str) -> str:
        """
        Generate summary for a session.
        
        Returns:
            Markdown-formatted summary
        """
        # 1. Load session log
        session = self._load_session(session_id)
        
        # 2. Extract key events
        events = self._extract_key_events(session)
        
        # 3. Prepare context for LLM
        context = self._build_llm_context(events)
        
        # 4. Call LLM
        summary = self.llm.invoke(context)
        
        # 5. Save summary
        self._save_summary(session_id, summary)
        
        return summary
    
    def _extract_key_events(self, session: dict) -> list[dict]:
        """
        Filter session log to key events.
        
        Keep:
        - First user message (task description)
        - Tool calls with large outputs
        - Errors
        - Final response
        
        Discard:
        - Intermediate reasoning
        - Repeated tool calls
        """
        ...
```

### 3.5 Triggering Strategy

#### **Option A: Immediate Summarization** (Default)

- Trigger on session close
- Async background job (don't block user)
- Suitable for short sessions (< 100 interactions)

#### **Option B: Deferred Summarization** (Cost-Optimized)

- Batch summarize during off-peak hours
- Prioritize sessions that:
  - Have > 50 interactions
  - Ended in success after failures
  - Were tagged by user

#### **Option C: On-Demand** (User-Controlled)

- User explicitly requests summary
- Useful for long debugging sessions

### 3.6 Cost Analysis

Assuming **gpt-4o-mini** at $0.15/1M input tokens, $0.60/1M output tokens:

| Scenario                          | Input Tokens | Output Tokens | Cost per Session |
| --------------------------------- | ------------ | ------------- | ---------------- |
| Short session (20 interactions)   | 2,000        | 300           | $0.0005          |
| Medium session (100 interactions) | 8,000        | 500           | $0.0015          |
| Long session (500 interactions)   | 30,000       | 600           | $0.0051          |

**Daily cost estimate** (10 sessions/day): ~$0.02 - $0.05

**Optimization**: Use smaller model for routine sessions, GPT-4 only for complex ones.

---

## 4. Integration with Existing Architecture

### 4.1 MemoryManager Extensions

```python
class MemoryManager:
    # Existing components
    session_logger: SessionLogger
    artifact_store: ArtifactStore
    
    # NEW: Phase 3 components
    semantic_store: SemanticStore
    summarizer: SessionSummarizer
    
    def close_session(self):
        """Enhanced session close with Phase 3 features."""
        # 1. Save session logs (existing)
        self.session_logger.close()
        
        # 2. Trigger summarization (NEW)
        if self.config.auto_summarize:
            summary = self.summarizer.summarize(self.session_id)
            
            # 3. Embed summary into semantic store (NEW)
            self.semantic_store.add_entry(
                content=summary,
                metadata={
                    "session_id": self.session_id,
                    "agent_name": self.agent_name,
                    "type": "session_summary"
                }
            )
```

### 4.2 Agent Context Augmentation

**Before (Phase 2)**:
```
Context = System Prompt + Recent History
```

**After (Phase 3)**:
```
Context = System Prompt + Relevant Past Knowledge + Recent History
                              ↑
                    From Semantic Search
```

**Implementation**:
```python
class ContextBuilder:
    def build_context_with_retrieval(
        self,
        system_prompt: str,
        history: list[BaseMessage],
        current_task: str  # NEW: Task description for retrieval
    ) -> list[BaseMessage]:
        # 1. Semantic search
        relevant_memories = self.semantic_store.search(
            query=current_task,
            top_k=3
        )
        
        # 2. Inject as "past experience" section
        if relevant_memories:
            knowledge_section = self._format_knowledge(relevant_memories)
            augmented_prompt = f"{system_prompt}\n\n{knowledge_section}"
        else:
            augmented_prompt = system_prompt
        
        # 3. Build context as before
        return self.build_context(augmented_prompt, history)
```

---

## 5. Implementation Roadmap

### Phase 3.1: Semantic Store Foundation (8-12 hours)

**Week 1: Core Infrastructure**
- [ ] ChromaDB integration
- [ ] SemanticStore class implementation
- [ ] Embedding pipeline (sentence-transformers)
- [ ] Basic search API
- [ ] Unit tests (embedding, search, persistence)

**Deliverables**:
- `src/soloqueue/core/memory/semantic_store.py`
- `tests/test_semantic_store.py`

### Phase 3.2: Session Summarization (6-10 hours)

**Week 2: Summarization System**
- [ ] SessionSummarizer class
- [ ] LLM prompt engineering
- [ ] Event extraction logic
- [ ] Async job queue (optional: use `asyncio` or `celery`)
- [ ] Integration tests

**Deliverables**:
- `src/soloqueue/core/memory/summarizer.py`
- `tests/test_summarizer.py`

### Phase 3.3: Integration & Optimization (4-6 hours)

**Week 3: Production Ready**
- [ ] MemoryManager integration
- [ ] ContextBuilder augmentation
- [ ] CLI tools (`soloqueue memory search`, `soloqueue session summarize`)
- [ ] Performance profiling
- [ ] Documentation update

**Deliverables**:
- Updated `MemoryManager`
- E2E tests showing retrieval-augmented generation

---

## 6. Configuration

### Environment Variables

```bash
# Semantic Store
SOLOQUEUE_SEMANTIC_ENABLED=true
SOLOQUEUE_SEMANTIC_MODEL=all-MiniLM-L6-v2  # sentence-transformers model
SOLOQUEUE_SEMANTIC_TOP_K=5

# Summarization
SOLOQUEUE_AUTO_SUMMARIZE=true
SOLOQUEUE_SUMMARY_MODEL=gpt-4o-mini
SOLOQUEUE_SUMMARY_MIN_EVENTS=10  # Don't summarize tiny sessions
```

### Agent Configuration (YAML)

```yaml
name: retrieval_agent
memory:
  semantic_search: true       # Enable retrieval
  auto_summarize: true        # Summarize sessions
  summary_trigger: immediate  # or "deferred" or "manual"
```

---

## 7. Example Usage

### Example 1: Debugging with Past Knowledge

```python
# User: "Fix the API timeout issue"

# Agent retrieves similar past issues
past_solutions = semantic_store.search(
    query="API timeout debugging",
    filter_metadata={"type": "solution", "outcome": "success"}
)

# Agent's enhanced context now includes:
"""
[Past Experience]
2 weeks ago, we solved a similar API timeout by:
1. Increasing connection pool size
2. Adding retry logic with exponential backoff
3. Implementing circuit breaker pattern

This might help with your current issue.
"""
```

### Example 2: Automatic Learning

```python
# Session 1: User implements feature X (succeeds after trial and error)
# → Summarizer extracts: "How to implement feature X properly"
# → Embedded into semantic store

# Session 2 (1 week later): User asks "How did we implement X?"
# → Agent retrieves summary
# → Agent: "Based on our past work, here's how we did it..."
```

---

## 8. Success Metrics

| Metric                   | Target         | Measurement                          |
| ------------------------ | -------------- | ------------------------------------ |
| **Retrieval Precision**  | > 80%          | Relevant results in top-3            |
| **Summary Quality**      | > 4/5          | Human evaluation                     |
| **Search Latency**       | < 100ms        | P95 latency                          |
| **Embedding Throughput** | > 20 items/sec | Batch processing                     |
| **Knowledge Reuse Rate** | > 30%          | % of tasks using retrieved knowledge |

---

## 9. Open Questions

1. **Privacy**: Should we allow opt-out for sensitive sessions?
2. **Multi-tenancy**: How to isolate knowledge across different user groups?
3. **Knowledge Decay**: Should old, outdated knowledge expire?
4. **Human-in-the-loop**: Should summaries be reviewed before indexing?

---

## 10. Appendix: Technology Alternatives

### Embedding Models

| Model                          | Dimension | Speed   | Quality | Size  |
| ------------------------------ | --------- | ------- | ------- | ----- |
| **all-MiniLM-L6-v2** (default) | 384       | ⚡⚡⚡     | ⭐⭐⭐     | 80MB  |
| all-mpnet-base-v2              | 768       | ⚡⚡      | ⭐⭐⭐⭐    | 420MB |
| OpenAI text-embedding-3-small  | 1536      | ⚡ (API) | ⭐⭐⭐⭐⭐   | Cloud |

**Recommendation**: Start with `all-MiniLM-L6-v2` for local, fast embedding.

### Vector Databases

| Database               | Deployment  | Scale        | Complexity |
| ---------------------- | ----------- | ------------ | ---------- |
| **ChromaDB** (default) | Local       | Small-Medium | Low        |
| FAISS                  | Local       | Large        | Medium     |
| Pinecone               | Cloud       | Massive      | Low        |
| Weaviate               | Self-hosted | Large        | High       |

**Recommendation**: ChromaDB for v1, migrate to FAISS if scaling beyond 100K entries.

---

**End of Design Document**
