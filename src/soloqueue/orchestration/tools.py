from typing import List

from langchain_core.tools import BaseTool, tool
from pydantic import BaseModel, Field

from soloqueue.core.primitives import get_all_primitives

class DelegateInput(BaseModel):
    """Input for delegating a task to another agent."""
    target: str = Field(description="The name of the agent to delegate to (e.g., 'cto', 'developer')")
    instruction: str = Field(description="Detailed instruction of what the sub-agent needs to do.")

def create_delegate_tool(allowed_targets: List[str]) -> BaseTool:
    """
    Creates a 'delegate_to' tool with dynamic enum validation.
    This tool is NOT executed by Python; it's a signal for the Router.
    """
    # Create dynamic Enums or just use description to guide LLM? 
    # Proper Enum in Pydantic is safer.
    
    # Dynamic class creation for strict validation
    description = f"Delegate a task to one of: {', '.join(allowed_targets)}"
    
    @tool(args_schema=DelegateInput)
    def delegate_to(target: str, instruction: str) -> str:
        """Delegate a task to a subordinate agent."""
        # This function body might never be called if Router intercepts it first.
        # But if it is called, we return a special signal string.
        return f"__DELEGATE_TO__: {target} | {instruction}"
    
    delegate_to.description = description
    return delegate_to

from soloqueue.core.schema import AgentConfig

def resolve_tools_for_agent(config: AgentConfig) -> List[BaseTool]:
    """
    Combine built-in primitives and delegation tools for an agent.
    """
    all_primitives = {t.name: t for t in get_all_primitives()}
    
    final_tools = []
    
    # 1. Add Configured Skills (Primitives)
    for name in config.skills:
        if name in all_primitives:
            final_tools.append(all_primitives[name])
        else:
            # Check custom skills later?
            pass
            
    # 2. Add Delegation Tool if applicable (Leader only)
    if config.is_leader:
        # Leader can delegate to anyone. 
        # We don't restrict targets in the tool definition (enum) anymore to allow dynamic graph scaling.
        # We use a generic list or description.
        # Ideally, we should inject the list of available targets into the Prompt, not the Tool Schema, 
        # to avoid schema bloat.
        final_tools.append(create_delegate_tool(allowed_targets=["ANY_AGENT"]))
        
    return final_tools
