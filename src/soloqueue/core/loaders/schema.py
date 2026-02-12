from typing import List, Optional, Any, Dict
from pydantic import BaseModel, Field, model_validator

class AgentSchema(BaseModel):
    """
    Schema for Agent Definition files (AGENT.md frontmatter).
    Compatible with Claude Code / Gemini format.
    """
    name: str
    description: str
    model: Optional[str] = None
    reasoning: bool = Field(False, description="Enable reasoning/thinking mode for this agent")
    
    # Group Membership
    group: Optional[str] = Field(None, description="Group this agent belongs to")
    is_leader: bool = Field(False, description="Whether this agent is the group leader")
    
    # Capability Definition
    tools: List[str] = Field(default_factory=list, description="List of Skill/Tool names this agent can use")
    skills: List[str] = Field(default_factory=list, description="Alias for tools (for better readability)")
    sub_agents: List[str] = Field(default_factory=list, description="List of Sub-Agent names this agent can delegate to")
    
    # Memory / Context
    memory: Optional[str] = Field(None, description="Path to context file (relative to context/)")
    
    # Runtime (not in yaml)
    system_prompt: Optional[str] = None
    
    @model_validator(mode='after')
    def merge_skills(self):
        if self.skills:
            # Avoid duplicates
            existing = set(self.tools)
            for s in self.skills:
                if s not in existing:
                    self.tools.append(s)
        return self
    
    @property
    def node_id(self) -> str:
        """Returns LangGraph node identifier: {group}__{name} or just {name}"""
        if self.group:
            return f"{self.group}__{self.name}"
        return self.name 


class SkillSchema(BaseModel):
    """
    Schema for Skill Definition files (SKILL.md frontmatter).
    Matches Claude Code specification.
    """
    name: str  # Slash command (e.g., git-commit)
    description: str
    
    # Claude Code Spec Fields
    allowed_tools: List[str] = Field(default_factory=list, description="Tools this skill is allowed to use")
    disable_model_invocation: bool = Field(False, description="If true, can only be triggered manually")
    subagent: Optional[str] = Field(None, description="e.g. 'fork' to run in isolated context")
    
    # Arguments hint for autocomplete (optional)
    arguments: Optional[str] = None
    
    # Runtime (not in yaml)
    content: Optional[str] = None   # The Markdown prompt template
    path: Optional[str] = None      # Absolute path to skill directory


class GroupSchema(BaseModel):
    """
    Schema for Group Definition files (GROUP.md frontmatter).
    """
    name: str
    description: str
    shared_context: Optional[str] = Field(None, description="Shared context for all agents in this group")
