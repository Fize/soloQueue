# Part 2: Orchestration Design Document

**Version:** 2.0.0 (Custom Orchestration)
**Status:** Implemented
**Date:** 2026-02-07

---

## 1. Overview

Part 2 describes the **Orchestration Layer** of SoloQueue. Unlike Phase 1 which relied on LangGraph, the current implementation uses a lightweight, custom Python-based orchestration engine designed specifically for recursive agent delegation and state isolation.

**Core Philosophy:**
- **Stack-Based Execution:** Models the agent call stack explicitly (like a function call stack).
- **State Isolation:** Each agent runs in its own "Stack Frame" with isolated memory and context.
- **Explicit Control Flow:** Agents communicate intent via standardized `ControlSignal`s (e.g., DELEGATE, RETURN).

---

## 2. Core Components

### 2.1 The Orchestrator (`src/soloqueue/orchestration/orchestrator.py`)

The `Orchestrator` is the central event loop of the system. It replaces the complex graph traversal of LangGraph with a predictable stack management loop.

**Responsibilities:**
- Manages the **Function Call Stack** (`stack: List[TaskFrame]`).
- Loops until the stack is empty or max iterations reached.
- Dispatches execution to the `AgentRunner`.                           
- Handles `ControlSignal`s returned by the runner to modify the stack.

**Logic Loop:**
1.  **Peek** the top frame from the stack.
2.  **Run** one step of the active agent (`AgentRunner.step`).
3.  **Handle Signal**:
    *   `CONTINUE`: Do nothing, loop again.
    *   `DELEGATE`: **Push** a new `TaskFrame` for the target agent onto the stack.
    *   `RETURN`: **Pop** the current frame, pass the result to the *new* top frame (the caller).
    *   `ERROR`: Handle or terminate.

### 2.2 TaskFrame (`src/soloqueue/orchestration/frame.py`)

A `TaskFrame` represents a single unit of work (a function call) in the stack.

**Structure:**
```python
class TaskFrame:
    agent_name: str          # Who is running? (e.g., "investment.leader")
    memory: List[BaseMessage] # Local context for this task
    input_instruction: str   # The prompt that started this frame
    output_buffer: List[str] # Buffer for final response
    loop_count: int          # To prevent infinite loops
```

**State Isolation:**
- Every time an agent receives a task (via Delegation), a *fresh* `TaskFrame` is created.
- The sub-agent sees *only* the `input_instruction` and its own subsequent thoughts/actions.
- It does *not* see the entire history of the parent agent, preventing context pollution and reducing token usage.

### 2.3 AgentRunner (`src/soloqueue/orchestration/runner.py`)

The `AgentRunner` is a stateless executor that performs a single "Thinking Step" for an agent.

**Responsibilities:**
- **LLM Invocation:** Calls the underlying LLM (e.g., `ReasoningChatOpenAI`) handling streaming and token counting.
- **Prompt Construction:** Combines System Prompt + Frame Memory.
- **Tool Execution:** Executes "physical" tools (read_file, etc.) and appends `ToolMessage` to memory.
- **Intent Recognition:** Detects special "logical" tools like `delegate_to` and converts them into `ControlSignal`s.

**Streaming Support:**
- Uses `llm.stream()` to provide real-time feedback (Thinking process and Response) to the user via CLI.

### 2.4 ControlSignal (`src/soloqueue/orchestration/types.py`)

Standardized return object from `AgentRunner` to `Orchestrator`.

```python
class ControlSignal:
    type: SignalType  # CONTINUE, DELEGATE, RETURN, ERROR
    target_agent: str # For DELEGATE
    instruction: str  # For DELEGATE
    payload: Any      # For RETURN
```

---

## 3. The Delegation Protocol

Delegation is treated as a "logical tool call" but executed as a stack operation.

**The Flow:**
1.  **Leader** decides to delegate:
    *   LLM calls `delegate_to(target="trader", instruction="Buy Apple")`.
    *   `AgentRunner` detects this tool call.
    *   `AgentRunner` returns `ControlSignal(type=DELEGATE, target="trader", ...)` to Orchestrator.
    *   *Note*: The runner pauses physical execution here.

2.  **Orchestrator** switches context:
    *   Pushes `TaskFrame(agent="trader", instruction="Buy Apple")` to stack.
    *   Active agent is now **Trader**.

3.  **Trader** executes:
    *   Runs loop: Think -> Act -> Think -> ...
    *   Eventually determines task is done.
    *   LLM produces text response (no tool calls).
    *   `AgentRunner` detects completion, returns `ControlSignal(type=RETURN, payload="Bought Apple at $150")`.

4.  **Orchestrator** returns:
    *   Pops Trader's frame.
    *   Active agent is back to **Leader**.
    *   **Result Injection**: The payload ("Bought Apple...") is injected into Leader's memory as a `ToolMessage` corresponding to the original `delegate_to` call ID.
    *   Leader resumes execution, seeing the delegation as a completed tool call.

---

## 4. Key Improvements over LangGraph

1.  **Simplicity**: Removed heavy dependency on LangGraph's complex state graph and edge conditional logic.
2.  **Predictability**: The stack model is easier to debug and reason about than a cyclic graph.
3.  **Strict Isolation**: Enforcing fresh frames for sub-tasks guarantees that sub-agents don't hallucinate based on parent context.
4.  **Serialization**: Explicit handling of tool call serialization prevents "pending tool call" errors common in parallel execution environments.
