# 流式输出验证方案 (Streaming Output Verification)

## 1. 目标
验证 SoloQueue CLI 能够以**流式 (Streaming)** 方式逐字逐句地展示 AI 的回复，而不是等待生成完全部内容后一次性显示。这能显著提升用户体验，减少感知延迟。

## 2. 现状分析
当前的 `cli.py` 使用 `graph.astream(input_state)`。
*   **机制**：监听 Graph 的状态更新事件（Node 完成执行）。
*   **结果**：用户看到的输出是“块状”的（Block Output）。当 Agent 思考了 10 秒钟生成了一大段回复后，这段回复会瞬间出现在屏幕上。
*   **结论**：不符合"打字机效果"的要求。

## 3. 技术方案
我们需要从监听“节点状态”转向监听“LLM 生成事件”。
LangGraph 提供了 `astream_events` API 来实现这一点。

### 3.1 代码变更 (`src/soloqueue/cli.py`)
将 `graph.astream` 替换为 `graph.astream_events`，并处理思考过程。

**核心逻辑**：
*   **状态机**：维护一个 `is_thinking` 状态。
*   **检测标签**：当检测到 `<think>` 时，切换输出颜色为**灰色/斜体**。
*   **检测结束**：当检测到 `</think>` 时，恢复正常颜色。

```python
async for event in graph.astream_events(input_state, version="v1"):
    kind = event["event"]
    
    # 1. 监听 LLM 的流式 Token
    if kind == "on_chat_model_stream":
        chunk = event["data"]["chunk"]
        content = chunk.content
        if content:
            # 简单的流式状态机 (伪代码)
            if "<think>" in content:
                print("\n\033[90m[Thinking] ", end="", flush=True) # Grey
                is_thinking = True
                content = content.replace("<think>", "")
            
            if "</think>" in content:
                print(content.replace("</think>", ""), end="", flush=True)
                print("\033[0m\n", end="", flush=True) # Reset
                is_thinking = False
                continue
                
            print(content, end="", flush=True)
            
    # 2. 监听工具调用 (Thinking in Action)
    elif kind == "on_tool_start":
        print(f"\n\033[93m[Tool Action] {event['name']}...\033[0m", end="")
```

## 4. 验证步骤
1.  **修改代码**：更新 `cli.py`。
2.  **运行 Demo**：
    *   Command: `uv run python -m soloqueue.cli`
    *   Input: "9.11 和 9.8 哪个大？" (触发逻辑推理)
3.  **观察点**：
    *   **预期**：
        *   先看到灰色文字（Thinking Process）：*“首先比较整数部分，都是9... 然后比较小数部分...”*
        *   然后看到正常文字：*“9.11 小于 9.8...”*
    *   **失败**：如果不显示思考过程，直接给出答案；或者思考过程和答案混在一起无法区分。

## 5. 待办任务
*   [ ] Refactor `cli.py` to use `astream_events`.
*   [ ] Verify with a long generation task.
