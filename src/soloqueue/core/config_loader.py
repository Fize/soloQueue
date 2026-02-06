import os
import yaml
import logging
from pathlib import Path
from typing import Dict, List, Optional
from soloqueue.core.schema import GroupConfig, AgentConfig

logger = logging.getLogger(__name__)

class ConfigLoader:
    def __init__(self, config_root: str = "soloqueue/config"):
        # Resolve to absolute path relative to project root if needed
        # Assuming run from project root or config_root is absolute
        self.config_root = Path(config_root)
        self.groups: Dict[str, GroupConfig] = {}
        self.agents: List[AgentConfig] = []

    def load_all(self):
        """Loads all groups and agents, then validates relationships."""
        self._load_groups()
        self._load_agents()
        self._validate()

    def _load_groups(self):
        group_dir = self.config_root / "groups"
        if not group_dir.exists():
            logger.warning(f"Group config directory not found: {group_dir}")
            return

        for config_file in group_dir.glob("*.yaml"):
            try:
                with open(config_file, "r", encoding="utf-8") as f:
                    data = yaml.safe_load(f)
                    # Validate required fields
                    if "name" not in data:
                        logger.error(f"Group config {config_file} missing 'name' field.")
                        continue
                    
                    group = GroupConfig(**data)
                    self.groups[group.name] = group
                    logger.info(f"Loaded Group: {group.name}")
            except Exception as e:
                logger.error(f"Failed to load group {config_file}: {e}")

    def _load_agents(self):
        agent_dir = self.config_root / "agents"
        if not agent_dir.exists():
            logger.warning(f"Agent config directory not found: {agent_dir}")
            return

        for config_file in agent_dir.glob("*.yaml"):
            try:
                with open(config_file, "r", encoding="utf-8") as f:
                    data = yaml.safe_load(f)
                    if "name" not in data or "group" not in data:
                        logger.error(f"Agent config {config_file} missing 'name' or 'group'.")
                        continue
                    
                    # Handle optional fields with defaults via dataclass or dict.get
                    agent = AgentConfig(
                        name=data["name"],
                        group=data["group"],
                        system_prompt=data.get("system_prompt", ""),
                        is_leader=data.get("is_leader", False),
                        skills=data.get("skills", []),
                        description=data.get("description", "")
                    )
                    self.agents.append(agent)
                    logger.info(f"Loaded Agent: {agent.name} (Group: {agent.group})")
            except Exception as e:
                logger.error(f"Failed to load agent {config_file}: {e}")

    def _validate(self):
        """Validates configuration rules."""
        # 1. Check Orphan Agents (Group must exist)
        valid_agents = []
        for agent in self.agents:
            if agent.group not in self.groups:
                logger.error(f"Agent '{agent.name}' belongs to non-existent group '{agent.group}'. Ignoring.")
                continue
            valid_agents.append(agent)
        self.agents = valid_agents

        # 2. Check Multiple Leaders (One Leader per Group)
        leaders_by_group = {}
        for agent in self.agents:
            if agent.is_leader:
                if agent.group in leaders_by_group:
                    existing_leader = leaders_by_group[agent.group]
                    logger.error(
                        f"Multiple leaders detected in group '{agent.group}': "
                        f"'{existing_leader.name}' and '{agent.name}'. "
                        f"Ignoring is_leader flag for '{agent.name}'."
                    )
                    # Force downgrade
                    agent.is_leader = False
                else:
                    leaders_by_group[agent.group] = agent

    def get_group(self, group_name: str) -> Optional[GroupConfig]:
        return self.groups.get(group_name)

    def get_agents_by_group(self, group_name: str) -> List[AgentConfig]:
        return [a for a in self.agents if a.group == group_name]
