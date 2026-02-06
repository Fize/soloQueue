from typing import Dict, List, Optional
from soloqueue.core.schema import GroupConfig, AgentConfig
from soloqueue.core.config_loader import ConfigLoader

class Registry:
    _instance = None
    
    def __init__(self):
        self.groups: Dict[str, GroupConfig] = {}
        self.agents: Dict[str, AgentConfig] = {} # Key: full_name ("group.agent")
        self.agents_by_node: Dict[str, AgentConfig] = {} # Key: node_id ("group__agent")

    @classmethod
    def get_instance(cls):
        if cls._instance is None:
            cls._instance = Registry()
        return cls._instance

    def initialize(self, config_root: str = "soloqueue/config"):
        loader = ConfigLoader(config_root)
        loader.load_all()
        
        self.groups = loader.groups
        for agent in loader.agents:
            self.agents[agent.full_name] = agent
            self.agents_by_node[agent.node_id] = agent

    def get_agent_by_name(self, name: str, current_group: Optional[str] = None) -> Optional[AgentConfig]:
        """
        Resolves agent name to config.
        Supports short name (intra-group) and full name (inter-group).
        """
        # 1. Try absolute name
        if name in self.agents:
            return self.agents[name]
            
        # 2. Try relative name
        if current_group:
            full_name = f"{current_group}.{name}"
            if full_name in self.agents:
                return self.agents[full_name]
                
        return None

    def get_agent_by_node_id(self, node_id: str) -> Optional[AgentConfig]:
        return self.agents_by_node.get(node_id)
