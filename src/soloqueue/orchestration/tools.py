from typing import List, Set

from langchain_core.tools import BaseTool, tool
from pydantic import BaseModel, Field

from soloqueue.core.logger import logger
from soloqueue.core.primitives import get_all_primitives
from soloqueue.core.loaders.skill_loader import SkillLoader

# Global set to track skill tool names
_skill_tool_names: Set[str] = set()

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

from soloqueue.core.loaders import AgentConfig


def resolve_tools_for_agent(config: AgentConfig) -> List[BaseTool]:
    """
    Combine built-in primitives and delegation tools for an agent.
    Primitives are auto-included for ALL agents.
    """
    # 0. Start with ALL Primitives (Built-in skills)
    # The user specification states that all agents have these by default.
    final_tools = get_all_primitives()
    
    # Track names to avoid duplicates if user still lists them in YAML
    existing_tool_names = {t.name for t in final_tools}
    
    # 1. Add Configured Skills
    skill_loader = SkillLoader()
    
    # Dynamic Proxy Tool Factory
    def create_skill_proxy(skill_name: str, description: str):
        """Creates a tool that just signals the desire to use a skill."""
        
        class SkillInput(BaseModel):
            arguments: str = Field(description="Arguments for the skill (e.g. CLI flags or natural language input)")
            
        @tool(args_schema=SkillInput)
        def proxy_tool(arguments: str) -> str:
            """Executes the skill."""
            # Signal string for Runner to intercept
            return f"__USE_SKILL__: {skill_name} | {arguments}"
            
        proxy_tool.name = skill_name
        proxy_tool.description = description
        _skill_tool_names.add(skill_name)  # Track skill tool name
        return proxy_tool

    for name in config.tools:
        if name in existing_tool_names:
            # Already added as a primitive, skip to avoid duplication
            continue
        else:
            # Try loading as a Skill
            try:
                skill_schema = skill_loader.load(name)
                # Success! active the skill as a Tool
                proxy = create_skill_proxy(skill_schema.name, skill_schema.description)
                final_tools.append(proxy)
                existing_tool_names.add(skill_schema.name)
                logger.debug(f"Attached Skill Proxy: {skill_schema.name} for {config.node_id}")
            except Exception as e:
                # It might be an invalid tool name, or a primitive not in get_all_primitives?
                logger.warning(f"Skill '{name}' not found in Skill Registry. Error: {e}")
                
    # 2. Add Delegation Tool if applicable (Leader only)
    if config.is_leader:
        # Leader can delegate.
        # If sub_agents are defined, we restrict to those (Whitelist).
        # If empty but is_leader is True, we might allow ANY_AGENT (Wildcard) 
        # or just fail. For now, strict whitelist if list is present.
        
        targets = config.sub_agents
        if not targets:
            # Fallback to ANY_AGENT if no sub-agents defined but is_leader=True
            # This maintains backward compatibility or "Super Leader" mode
            targets = ["ANY_AGENT"]
            
        final_tools.append(create_delegate_tool(allowed_targets=targets))
        
    return final_tools


def is_skill_tool(tool_name: str) -> bool:
    """Check if a tool name is a skill proxy."""
    return tool_name in _skill_tool_names
