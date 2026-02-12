import frontmatter
from typing import Dict

from soloqueue.core.loaders.schema import AgentSchema
from soloqueue.core.workspace import workspace
from soloqueue.core.logger import logger

class AgentLoader:
    """
    Loads Agent configurations from Markdown files.
    """
    
    def __init__(self, config_dir: str = "config/agents"):
        # We rely on workspace to resolve the config path, 
        # but config usually lives inside the project.
        try:
            self.config_root = workspace.resolve_path(config_dir)
        except Exception:
            # Fallback if config dir doesn't exist yet
            self.config_root = workspace.root / config_dir
            
    def load(self, name: str) -> AgentSchema:
        """
        Load a specific agent by name (filename without .md).
        """
        file_path = self.config_root / f"{name}.md"
        
        if not file_path.exists():
            raise FileNotFoundError(f"Agent config not found: {file_path}")
            
        try:
            post = frontmatter.load(str(file_path))
            
            # Validate metadata against schema
            data = post.metadata
            data['name'] = data.get('name', name) # Ensure name exists
            
            agent = AgentSchema(**data)
            agent.system_prompt = post.content # The markdown body
            
            logger.debug(f"Loaded agent: {agent.name}")
            return agent
            
        except Exception as e:
            logger.error(f"Failed to load agent {name}: {e}")
            raise e

    def load_all(self) -> Dict[str, AgentSchema]:
        """
        Load all agents in the config directory.
        """
        agents = {}
        if not self.config_root.exists():
            return agents
            
        for file_path in self.config_root.glob("*.md"):
            try:
                name = file_path.stem
                agents[name] = self.load(name)
            except Exception:
                continue
                
        return agents

    def save(self, agent: AgentSchema):
        """
        Save AgentSchema back to Markdown file.
        """
        # Convert schema to dict (exclude system_prompt which is content)
        data = agent.model_dump(exclude={"system_prompt"}, exclude_none=True)
        
        # Clean up empty lists to keep YAML clean
        if 'tools' in data and not data['tools']:
            del data['tools']
        if 'sub_agents' in data and not data['sub_agents']:
            del data['sub_agents']
        
        # Create Post
        content = agent.system_prompt or ""
        post = frontmatter.Post(content, **data)
        
        # Write file
        file_path = self.config_root / f"{agent.name}.md"
        
        # Ensure directory exists
        file_path.parent.mkdir(parents=True, exist_ok=True)
        
        with open(file_path, "wb") as f:
            frontmatter.dump(post, f)
            
        logger.info(f"Saved agent config: {file_path}")
