from typing import List, Optional, Any, Dict
from pydantic import BaseModel, Field

class AgentSchema(BaseModel):
    """
    Schema for Agent Definition files (AGENT.md frontmatter).
    Compatible with Claude Code / Gemini format.
    """
    name: str
    description: str
    model: Optional[str] = None
    
    # Group Membership
    group: Optional[str] = Field(None, description="Group this agent belongs to")
    is_leader: bool = Field(False, description="Whether this agent is the group leader")
    
    # Capability Definition
    tools: List[str] = Field(default_factory=list, description="List of Skill names this agent can use")
    sub_agents: List[str] = Field(default_factory=list, description="List of Sub-Agent names this agent can delegate to")
    
    # Memory / Context
    memory: Optional[str] = Field(None, description="Path to context file (relative to context/)")
    
    # Runtime (not in yaml)
    system_prompt: Optional[str] = None
    
    @property
    def node_id(self) -> str:
        """Returns LangGraph node identifier: {group}__{name} or just {name}"""
        if self.group:
            return f"{self.group}__{self.name}"
        return self.name 


class SkillSchema(BaseModel):
    """
    Schema for Skill Definition files (SKILL.md frontmatter).
    """
    name: str
    description: str
    input_schema: Dict[str, Any] = Field(default_factory=dict, description="JSON Schema for arguments")
    
    # Runtime (not in yaml)
    instructions: Optional[str] = None
    scripts_dir: Optional[str] = None


class GroupSchema(BaseModel):
    """
    Schema for Group Definition files (GROUP.md frontmatter).
    """
    name: str
    description: str
    shared_context: Optional[str] = Field(None, description="Shared context for all agents in this group")
