from typing import Dict, Any, List
import sqlite3
import os

from langgraph.graph import StateGraph, END
from langgraph.prebuilt import ToolNode

from soloqueue.orchestration.state import AgentState
from soloqueue.orchestration.tools import resolve_tools_for_agent
from soloqueue.orchestration.graph.node import create_agent_runner
from soloqueue.core.registry import Registry
from soloqueue.orchestration.graph.router import (
    get_node_router,
    delegate_node,
    pop_node,
    global_router
)

def build_dynamic_graph(checkpointer=None):
    builder = StateGraph(AgentState)
    
    # 1. Initialize Registry
    registry = Registry.get_instance()
    # Ensure it's initialized (safe to call multiple times if idempotent, 
    # but load_all might re-read files. Registry singleton handles it? 
    # Registry implementation re-loads every time initialize is called.
    # Ideally should check if loaded.)
    # For now, just call it.
    registry.initialize() 
    
    # 2. Register Hub Nodes
    builder.add_node("delegate_node", delegate_node)
    builder.add_node("pop_node", pop_node)
    
    # 3. Register Agents
    all_node_ids = []
    
    for node_id, config in registry.agents_by_node.items():
        all_node_ids.append(node_id)
        
        # A. Resolve Tools
        tools = resolve_tools_for_agent(config)
        
        # B. Create Agent Runner Node
        agent_runner = create_agent_runner(config, tools)
        builder.add_node(node_id, agent_runner)
        
        # C. Create Tool Node
        tool_node_name = f"{node_id}_tools"
        tool_node = ToolNode(tools)
        builder.add_node(tool_node_name, tool_node)
        
        # D. Edges (Agent -> Router)
        builder.add_conditional_edges(
            node_id,
            get_node_router(node_id),
            {
                tool_node_name: tool_node_name,
                "delegate_node": "delegate_node",
                "pop_node": "pop_node",
                "__end__": END
            }
        )
        
        # E. Edges (Tool -> Back to Agent)
        builder.add_edge(tool_node_name, node_id)

    # 4. Hub Edges (The Switchboard)
    routing_map = {nid: nid for nid in all_node_ids}
    
    builder.add_conditional_edges(
        "delegate_node",
        global_router,
        routing_map
    )
    
    builder.add_conditional_edges(
        "pop_node",
        global_router,
        routing_map
    )
    
    # 5. Entry Point
    entry_agent = registry.get_agent_by_name("investment.leader")
    if entry_agent:
        builder.set_entry_point(entry_agent.node_id)
    else:
        leaders = [a for a in registry.agents.values() if a.is_leader]
        if leaders:
            builder.set_entry_point(leaders[0].node_id)
        else:
            raise ValueError("No Leader Agent found in configuration!")

    # 6. Persistence (External)
    if checkpointer:
        return builder.compile(checkpointer=checkpointer)
    else:
        return builder.compile()
