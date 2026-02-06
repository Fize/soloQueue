from typing import Literal, Dict, Any, Optional

from langchain_core.messages import AIMessage, ToolMessage
from soloqueue.core.logger import logger
from soloqueue.orchestration.state import AgentState
from soloqueue.core.registry import Registry

def get_node_router(agent_node_id: str):
    """Factory to create a router specific to an agent's tool node."""
    tool_node_name = f"{agent_node_id}_tools"
    
    def router(state: AgentState) -> Literal[tool_node_name, "delegate_node", "__end__", "pop_node"]:
        messages = state["messages"]
        if not messages:
            return "__end__"
            
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
    Handles delegation signal with Permission Checking.
    """
    messages = state["messages"]
    last_msg = messages[-1]
    
    tool_call = last_msg.tool_calls[0]
    target_name = tool_call["args"]["target"]
    instruction = tool_call["args"]["instruction"]
    
    active_agent_id = state.get("active_agent", "unknown")
    registry = Registry.get_instance()
    
    # Resolve Source
    source_config = registry.get_agent_by_node_id(active_agent_id)
    if not source_config:
        logger.error(f"Unknown source agent: {active_agent_id}")
        return _return_error(tool_call, f"Internal Error: Unknown source agent {active_agent_id}", active_agent_id)

    # Resolve Target
    # We pass source's group to allow short-name resolution (intra-group)
    target_config = registry.get_agent_by_name(target_name, current_group=source_config.group)
    
    if not target_config:
        logger.error(f"Delegation failed: Target '{target_name}' not found.")
        return _return_error(tool_call, f"Target '{target_name}' not found.", active_agent_id)

    # Permission Check
    allowed = False
    if source_config.group == target_config.group:
        # Intra-group: Allow
        allowed = True
    else:
        # Inter-group: Leader Only
        if source_config.is_leader and target_config.is_leader:
            allowed = True
        else:
            reason = "Inter-group delegation requires both agents to be Leaders."
            logger.warning(f"Delegation denied: {active_agent_id} -> {target_config.node_id}. {reason}")
            return _return_error(tool_call, f"Permission Denied: {reason}", active_agent_id)
            
    if not allowed:
         return _return_error(tool_call, "Permission Denied: Unknown reason.", active_agent_id)

    # Stack Management
    logger.info(f"Delegating: {active_agent_id} -> {target_config.node_id} | Inst: {instruction}")
    
    tool_msg = ToolMessage(
        tool_call_id=tool_call["id"],
        content=f"Instruction: {instruction}",
        name="delegate_to"
    )
    
    current_stack = state.get("call_stack", [])
    new_stack = current_stack + [active_agent_id]
    
    return {
        "messages": [tool_msg],
        "next_recipient": target_config.node_id,
        "call_stack": new_stack
    }

def pop_node(state: AgentState) -> Dict[str, Any]:
    """Pops the call stack to return to the parent agent."""
    stack = state.get("call_stack", [])
    if not stack:
        return {"next_recipient": "__end__"}
        
    target_node_id = stack[-1]
    new_stack = stack[:-1]
    
    logger.info(f"Returning control to {target_node_id}")
    
    return {
        "next_recipient": target_node_id,
        "call_stack": new_stack
    }

def global_router(state: AgentState) -> str:
    """Read next_recipient from state."""
    target = state.get("next_recipient")
    if target:
        return target
    return "__end__"

def _return_error(tool_call, error_msg, active_agent_id):
    """Helper to return a ToolMessage error and route back to sender."""
    return {
        "messages": [ToolMessage(
            tool_call_id=tool_call["id"],
            content=f"Error: {error_msg}",
            name="delegate_to"
        )],
        "next_recipient": active_agent_id
    }
