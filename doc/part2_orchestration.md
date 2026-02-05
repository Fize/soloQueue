# Part 2: Orchestration Design Document

**Version:** 1.1.0 (Detailed Tech Spec)
**Status:** Approved
**Date:** 2026-02-06
**Dependencies:** Part 1 (Infrastructure)

---

## 1. Overview

Part 2 负责构建 SoloQueue 的动态运行系统 (The Runtime)。

**Core Philosophy:**
- **Dynamic Graph Construction:** 系统启动时扫描 `config/agents/*.md`，为每个配置文件动态注册一个 LangGraph 节点。
- **Recursive Delegation:** 使用 **Call Stack (调用栈)** 管理层级任务委派。
- **Shared State:** 所有节点共享统一的状态结构。

---

## 2. Technical Specification

### 2.1 The State (`src/soloqueue/orchestration/state.py`)

AgentState 是图在节点间流转的唯一数据包。

```python
from typing import TypedDict, Annotated, List, Literal, Sequence, Dict, Any
from langchain_core.messages import BaseMessage
import operator

class AgentState(TypedDict):
    """
    Control flow and memory state for the Agent Graph.
    """
    # --- Messaging ---
    # Append-only log of messages.
    # Agents read this to understand history.
    messages: Annotated[List[BaseMessage], operator.add]
    
    # --- Control Flow ---
    # The name of the node that should execute NEXT.
    # - "ceo", "developer", etc. (valid node names)
    # - "__end__" (termination)
    next_recipient: str
    
    # --- Recursion Stack ---
    # Acts like a function call stack for delegation.
    # When Boss delegates to Worker: call_stack.append("Boss")
    # When Worker finishes: next_recipient = call_stack.pop()
    call_stack: Annotated[List[str], lambda existing, new: new] # Always overwrite logic for stack? 
    # Actually, we need to be careful with stack updates in LangGraph. 
    # Safest is to treat it as immutable replacement or specific reducer.
    # For now, we will simply pass the modified list.
    
    # --- Structured Context ---
    # A persistent blackboard for structured data exchange (not just chat).
    # e.g., {"financial_report_path": "data/reports/2023.txt"}
    artifacts: Annotated[Dict[str, Any], lambda x, y: {**x, **y}]
```

### 2.2 Tool Binding & Execution (`src/soloqueue/orchestration/tools.py`)

我们需要一个转换层，将我们定义的 `SkillSchema` 和 Built-in Primitives 转换为 LangChain 能理解的 `BaseTool` 或 Function Definition。

**Tool Conversion Logic:**

1.  **Built-in Primitives:**
    *   直接封装为 `StructuredTool`。
    *   例如：`read_file` -> `BaseTool(name="read_file", func=core.primitives.file_io.read_file)`

2.  **Custom Skills (`SKILL.md`):**
    *   读取 `input_schema` (JSON Schema)。
    *   创建一个 `DynamicTool`。
    *   **Execution Logic:** 当 LLM 调用此 Tool 时，实际上我们在 Python 侧并不是执行一个函数，而是：
        1.  (Optional) 如果是简单的 Script Skill，执行 `python skills/xyz/script.py`。
        2.  (More likely) Skill 仅仅是一段 Prompt 指引。
        *Wait, `SKILL.md` in Claude Code usually implies executing a script OR providing instructions.*
        *Design Decision:* MVP 阶段，Skill 主要作为 **Built-in Tools 的组合逻辑 prompt** 注入给 Agent，或者包含一个 `scripts/` 目录供 `bash` 工具执行。
        *Refined Decision:* 为了简单，Skill 被视为一组 **System Prompt 注入** + **可选的可执行脚本**。如果包含脚本，Agent 可以用 `bash` 去跑。

### 2.3 The Agent Node (`src/soloqueue/orchestration/graph/node.py`)

这是通用的 Agent 运行时逻辑。

```python
from langchain_openai import ChatOpenAI
from langchain_core.prompts import ChatPromptTemplate, MessagesPlaceholder
from soloqueue.core.loaders import AgentConfig
from soloqueue.core.llm import LLMFactory  # [NEW] Import LLM Factory

def create_agent_runner(config: AgentConfig, tools: List[BaseTool]):
    """
    Closure that returns the node function.
    """
    # 1. Initialize LLM (once per node creation)
    # 使用 Phase 1.5 实现的 Factory 获取实例
    llm = LLMFactory.get_llm(config.model).bind_tools(tools)
    
    async def agent_node(state: AgentState):
        messages = state["messages"]
        # ... logic ...
        sender = state.get("active_agent", "user") # Who sent the last message?
        
        # 2. Construct Prompt
        # Inject System Prompt from Agent Config
        # Inject Context from Memory artifacts
        
        # 3. Invoke LLM
        response = await llm.ainvoke(messages)
        
        # 4. Handle Tool Calls vs Delegation
        # Need custom logic to differentiate:
        # - Real Tool Call (e.g. read_file) -> Calculate result locally
        # - Delegation (magic function "delegate_to") -> Update state.next_recipient
        
        return {
            "messages": [response],
            # ... update other state ...
        }
        
    return agent_node
```

### 2.4 Graph Builder (`src/soloqueue/orchestration/graph/builder.py`)

```python
from langgraph.graph import StateGraph, END
from soloqueue.core.loaders import AgentLoader
from soloqueue.orchestration.graph.node import create_agent_runner

def build_dynamic_graph():
    graph = StateGraph(AgentState)
    loader = AgentLoader()
    agents = loader.load_all()
    
    # 1. Register nodes
    for name, config in agents.items():
        # Resolve tools for this agent
        tools = resolve_tools(config.tools)
        # Add "delegate" tool if sub_agents exists
        if config.sub_agents:
            tools.append(create_delegate_tool(config.sub_agents))
            
        graph.add_node(name, create_agent_runner(config, tools))
        
    # 2. Logic for Delegation Edge
    # We define a special "tool_node" or execute tools inside the agent node?
    # LangGraph standard pattern: Agent -> [Tools] -> Agent.
    # Delegation is just a special "Tool" that changes the control flow.
    
    def router(state) -> Literal["call_tool", "__end__", "new_agent_name"]:
        # Inspect the last message.
        # If tool_calls has "delegate_to(target='cto')", return "cto"
        # If tool_calls has "read_file", return "call_tool"
        # Else return "__end__" (or loop back)
        ...

    # 3. Wiring
    # This is the tricky part in a dynamic graph.
    # Every agent needs to connect to the Router.
    ...
    
    return graph.compile()
```

### 2.5 The CLI Entrypoint (`src/soloqueue/cli.py`)

系统启动的入口。

```python
def main():
    # 1. Init Infrastructure (Logger, Config)
    setup_logger()
    
    # 2. Build Graph
    graph = build_dynamic_graph()
    
    # 3. Interactive Loop
    while True:
        user_input = input("User: ")
        # Stream events from the graph
        for event in graph.stream({"messages": [HumanMessage(content=user_input)]}):
            print_agent_action(event)
```

## 3. Skill & Memory Constraints

### 3.1 Skill Definition Boundary
为了系统安全与稳定性，严格定义 Layer 2 Skill 的范围：
*   **Allowed:** 
    *   Prompt Engineering (System Prompts).
    *   Command Combinations (Bash scripts in `scripts/`).
*   **Prohibited:** 
    *   Complex Python Logic (Should be a Layer 1 Primitive).
    *   Direct Hardware Access outside of Workspace/Bash.

### 3.2 Context Window Management
为了防止 State 无限膨胀：
*   **FIFO Truncation:** Agent Node 在构建 Prompt 时，保留 System Prompt，保留最近 N 轮对话，通过 `trim_messages` 压缩中间历史。

---

## 4. Delegation Protocol details

为了实现递归委派，我们引入一个特殊的虚拟工具 `delegate_to`。

**Tool Definition:**
```json
{
  "name": "delegate_to",
  "description": "Delegate a sub-task to a subordinate agent.",
  "parameters": {
    "type": "object",
    "properties": {
      "target": { "type": "string", "enum": ["cto", "cfo"] },
      "instruction": { "type": "string", "description": "What to do" }
    },
    "required": ["target", "instruction"]
  }
}
```

**Router Logic:**
1.  如果 LLM 返回 `tool_calls=[delegate_to(target="cto")]`：
    *   LangGraph **不** 执行任何物理工具。
    *   Router 捕获此信号。
    *   State 更新：`stack.append(current_agent)`, `next_recipient = "cto"`.
    *   控制流转移到 `CTO` 节点。

2.  如果 LLM 返回 `tool_calls=[read_file(...)]`：
    *   转移到 `ToolExecutor` 节点（可以是每个 Agent 专属，也可以是全局共享的 ToolNode）。
    *   执行后返回 Agent。

3.  如果 LLM 返回文本（无 tool call）：
    *   视为任务完成 / 回复。
    *   State 更新：`next_recipient = stack.pop()` (Return to boss)。
    *   如果 stack 为空，则结束对话。

---

## 4. Implementation Steps

### Step 1: Tooling Infrastructure
- [ ] `src/soloqueue/orchestration/tools.py`: Helper functions to convert primitives/skills -> LangChain Tools.
- [ ] Implement `DelegateTool` schema generator.

### Step 2: State Definition
- [ ] `src/soloqueue/orchestration/state.py`.

### Step 3: Agent Node Logic
- [ ] `src/soloqueue/orchestration/graph/node.py`.
- [ ] logic to handle `tool_calls` vs `final_response`.

### Step 4: The Graph
- [ ] `src/soloqueue/orchestration/graph/builder.py`.
- [ ] Implement the `router` conditional edge logic carefully.
