from dataclasses import dataclass, field
from typing import List, Optional

@dataclass
class GroupConfig:
    """Configuration for an Agent Group."""
    name: str  # Unique ID (e.g., 'investment')
    description: str
    shared_context: str = ""

@dataclass
class AgentConfig:
    """Configuration for a single Agent."""
    name: str  # Unique within the group (e.g., 'leader')
    group: str # Must match a GroupConfig.name
    system_prompt: str
    model: Optional[str] = None # Fallback to settings.DEFAULT_MODEL if None
    is_leader: bool = False
    skills: List[str] = field(default_factory=list)
    description: str = ""

    @property
    def full_name(self) -> str:
        """Returns globally unique identifier: {group}.{name}"""
        return f"{self.group}.{self.name}"

    @property
    def node_id(self) -> str:
        """Returns LangGraph node identifier: {group}__{name}"""
        return f"{self.group}__{self.name}"
