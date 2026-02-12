import frontmatter
from typing import Dict

from soloqueue.core.loaders.schema import GroupSchema
from soloqueue.core.workspace import workspace
from soloqueue.core.logger import logger

class GroupLoader:
    """
    Loads Group configurations from Markdown files.
    """
    
    def __init__(self, config_dir: str = "config/groups"):
        try:
            self.config_root = workspace.resolve_path(config_dir)
        except Exception:
            self.config_root = workspace.root / config_dir
            
    def load(self, name: str) -> GroupSchema:
        """
        Load a specific group by name (filename without .md).
        """
        file_path = self.config_root / f"{name}.md"
        
        if not file_path.exists():
            raise FileNotFoundError(f"Group config not found: {file_path}")
            
        try:
            post = frontmatter.load(str(file_path))
            
            data = post.metadata
            data['name'] = data.get('name', name)
            
            group = GroupSchema(**data)
            group.shared_context = post.content  # The markdown body
            
            logger.debug(f"Loaded group: {group.name}")
            return group
            
        except Exception as e:
            logger.error(f"Failed to load group {name}: {e}")
            raise e

    def load_all(self) -> Dict[str, GroupSchema]:
        """
        Load all groups in the config directory.
        """
        groups = {}
        if not self.config_root.exists():
            return groups
            
        for file_path in self.config_root.glob("*.md"):
            try:
                name = file_path.stem
                groups[name] = self.load(name)
            except Exception:
                continue
                
        return groups

    def save(self, group: GroupSchema):
        """
        Save GroupSchema back to Markdown file.
        """
        # Convert schema to dict (exclude shared_context which is content)
        data = group.model_dump(exclude={"shared_context"}, exclude_none=True)
        
        # Create Post
        content = group.shared_context or ""
        post = frontmatter.Post(content, **data)
        
        # Write file
        file_path = self.config_root / f"{group.name}.md"
        
        # Ensure directory exists
        file_path.parent.mkdir(parents=True, exist_ok=True)
        
        with open(file_path, "wb") as f:
            frontmatter.dump(post, f)
            
        logger.info(f"Saved group config: {file_path}")
