from typing import TypedDict, Annotated, List, Dict, Any
import operator

from langchain_core.messages import AnyMessage

def merge_artifacts(existing: Dict[str, Any], new: Dict[str, Any]) -> Dict[str, Any]:
    """Merge new artifacts into existing dictionary."""
    if not existing:
        existing = {}
    return {**existing, **new}

class AgentState(TypedDict):
    """
    Control flow and memory state for the Agent Graph.
    """
    # --- Messaging ---
    # Append-only log of messages.
    # Agents read this to understand history.
    messages: Annotated[List[AnyMessage], operator.add]
    
    # --- Control Flow ---
    # The name of the node that should execute NEXT.
    # - "ceo", "developer", etc. (valid node names)
    # - "__end__" (termination)
    next_recipient: str
    
    # Track which agent is currently active (to push to stack)
    active_agent: str
    
    # --- Recursion Stack ---
    # Acts like a function call stack for delegation.
    # When Boss delegates to Worker: call_stack.append("Boss")
    # When Worker finishes: next_recipient = call_stack.pop()
    # Using a custom reducer or just replacing the list works for LangGraph
    # if we manage it carefully in nodes. Replacing is simpler for MVP.
    call_stack: List[str]
    
    # --- Structured Context ---
    # A persistent blackboard for structured data exchange (not just chat).
    # e.g., {"financial_report_path": "data/reports/2023.txt"}
    artifacts: Annotated[Dict[str, Any], merge_artifacts]
