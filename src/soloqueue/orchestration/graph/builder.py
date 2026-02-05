from typing import Literal, Dict, Any

from langgraph.graph import StateGraph, END
from langgraph.prebuilt import ToolNode
from langchain_core.messages import AIMessage, ToolMessage

from soloqueue.core.loaders import AgentLoader
from soloqueue.core.logger import logger
from soloqueue.orchestration.state import AgentState
from soloqueue.orchestration.tools import resolve_tools_for_agent
from soloqueue.orchestration.graph.node import create_agent_runner

def get_router(agent_name: str):
    """Factory to create a router specific to an agent's tool node name."""
    tool_node_name = f"{agent_name}_tools"
    
    def router(state: AgentState) -> Literal[tool_node_name, "delegate_node", "__end__"]:
        messages = state["messages"]
        last_message = messages[-1]
        
        if not isinstance(last_message, AIMessage):
            return "__end__"
            
        tool_calls = last_message.tool_calls
        
        if tool_calls:
            # Check for Delegation
            for tool_call in tool_calls:
                if tool_call["name"] == "delegate_to":
                    return "delegate_node"
            return tool_node_name
            
        # No tools? Check stack to see if we need to return
        call_stack = state.get("call_stack", [])
        if call_stack:
            return "pop_node"
            
        return "__end__"
        
    return router

def delegate_node(state: AgentState) -> Dict[str, Any]:
    """
    Handles delegation signal.
    """
    messages = state["messages"]
    last_msg = messages[-1]
    
    # Extract target from the tool call
    # Assumption: Only one delegation call per turn
    tool_call = last_msg.tool_calls[0]
    target = tool_call["args"]["target"]
    instruction = tool_call["args"]["instruction"]
    
    logger.info(f"Delegating to {target}: {instruction}")
    
    # We construct a ToolMessage to close the tool call loop for the LLM history
    # This prevents the LLM from thinking the tool call is still pending
    tool_msg = ToolMessage(
        tool_call_id=tool_call["id"],
        content=f"Instruction: {instruction}",
        name="delegate_to"
    )
    
    # Manage Call Stack
    # We push the current active agent onto the stack
    active_agent = state.get("active_agent", "leader") # Default to leader if unknown
    current_stack = state.get("call_stack", [])
    new_stack = current_stack + [active_agent]
    
    return {
        "messages": [tool_msg],
        "next_recipient": target,
        "call_stack": new_stack
    }

def pop_node(state: AgentState) -> Dict[str, Any]:
    """
    Pops the call stack to return to the parent agent.
    """
    stack = state.get("call_stack", [])
    if not stack:
        # Should not happen if routed correctly, but safe fallback
        return {"next_recipient": "__end__"}
        
    target = stack[-1]
    new_stack = stack[:-1]
    
    logger.info(f"Returning control to {target}")
    
    # Optional: Add a system message to inform the parent agent of the return?
    # For now, we assume the parent sees the tool output and resumes.
    
    return {
        "next_recipient": target,
        "call_stack": new_stack
    }

def global_router(state: AgentState) -> str:
    """
    The Central Switchboard.
    Reads 'next_recipient' from state and routes to that node.
    """
    target = state.get("next_recipient")
    if target:
        # Reset next_recipient? Maybe not, it acts as a pointer.
        return target
    return "__end__"


def build_dynamic_graph():
    builder = StateGraph(AgentState)
    loader = AgentLoader()
    agents = loader.load_all()
    
    # 1. Register Delegate Node (Hub)
    builder.add_node("delegate_node", delegate_node)
    
    # 1.5 Register Pop Node (Return Hub)
    builder.add_node("pop_node", pop_node)
    
    # 2. Register Agent + Tool Nodes
    agent_names = list(agents.keys())
    
    for name, config in agents.items():
        # A. Create Tools
        tools = resolve_tools_for_agent(config.tools, config.sub_agents)
        
        # B. Agent Node
        agent_node = create_agent_runner(config, tools)
        builder.add_node(name, agent_node)
        
        # C. Tool Node (Dedicated)
        tool_node_name = f"{name}_tools"
        tool_node = ToolNode(tools)
        builder.add_node(tool_node_name, tool_node)
        
        # D. Edges
        # Agent -> Router -> [MyTools, Delegate, Pop, End]
        builder.add_conditional_edges(
            name,
            get_router(name),
            {
                tool_node_name: tool_node_name,
                "delegate_node": "delegate_node",
                "pop_node": "pop_node",
                "__end__": END
            }
        )
        
        # Tools -> Back to Agent
        builder.add_edge(tool_node_name, name)
        
    # 3. Delegate Node -> Global Router -> Any Agent
    # The DelegateNode sets 'next_recipient', and this edge routes it.
    builder.add_conditional_edges(
        "delegate_node",
        global_router,
        # Map ALL possible agent names
        {name: name for name in agent_names}
    )
    
    # 4. Pop Node -> Global Router -> Any Agent
    builder.add_conditional_edges(
        "pop_node",
        global_router,
        {name: name for name in agent_names}
    )
    
    # 4. Entry Point
    builder.set_entry_point("leader")  # Investment Team Leader
    
    return builder.compile()
