"""
Registry: Central configuration management using MVP loaders.
"""
from typing import Dict, List, Optional

from soloqueue.core.loaders import AgentLoader, AgentConfig, GroupLoader, GroupConfig
from soloqueue.core.logger import logger


class Registry:
    """
    Singleton registry for agent and group configurations.
    Uses MVP-style Markdown frontmatter loaders.
    """
    _instance: Optional["Registry"] = None
    
    def __init__(self):
        self.agents: Dict[str, AgentConfig] = {}
        self.groups: Dict[str, GroupConfig] = {}
        self.agents_by_node: Dict[str, AgentConfig] = {}
        self._initialized = False
    
    @classmethod
    def get_instance(cls) -> "Registry":
        if cls._instance is None:
            cls._instance = cls()
        return cls._instance
    
    def initialize(self):
        """Load all configurations from disk."""
        if self._initialized:
            return
            
        # Load agents using MVP loader
        agent_loader = AgentLoader()
        self.agents = agent_loader.load_all()
        
        # Build node_id -> config mapping
        for name, config in self.agents.items():
            self.agents_by_node[config.node_id] = config
            logger.info(f"Registered Agent: {config.node_id} (model={config.model})")
        
        # Load groups using MVP loader
        group_loader = GroupLoader()
        self.groups = group_loader.load_all()
        
        for name, config in self.groups.items():
            logger.info(f"Registered Group: {name}")
        
        # Validate: Each group should have exactly one leader
        self._validate_leaders()
        
        self._initialized = True
    
    def _validate_leaders(self):
        """Ensure each group has exactly one leader."""
        group_leaders: Dict[str, List[str]] = {}
        
        for name, agent in self.agents.items():
            if agent.group and agent.is_leader:
                if agent.group not in group_leaders:
                    group_leaders[agent.group] = []
                group_leaders[agent.group].append(name)
        
        for group, leaders in group_leaders.items():
            if len(leaders) > 1:
                logger.warning(f"Group '{group}' has multiple leaders: {leaders}. Using first: {leaders[0]}")
    
    def get_agent_by_name(self, name: str) -> Optional[AgentConfig]:
        """
        Get agent by name. Supports:
        - Short name: 'leader' (searches all agents)
        - Full name: 'investment.leader' (group.name format)
        """
        if "." in name:
            group, short_name = name.split(".", 1)
            for agent in self.agents.values():
                if agent.group == group and agent.name == short_name:
                    return agent
        else:
            for agent in self.agents.values():
                if agent.name == name:
                    return agent
        return None
    
    def get_agents_by_group(self, group: str) -> List[AgentConfig]:
        """Get all agents in a group."""
        return [a for a in self.agents.values() if a.group == group]
