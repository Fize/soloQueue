import frontmatter
import shutil
from typing import Dict
import pathlib

from soloqueue.core.loaders.schema import SkillSchema
from soloqueue.core.workspace import workspace
from soloqueue.core.logger import logger

class SkillLoader:
    """
    Loads Skill configurations from SKILL.md files.
    Structure: skills/<skill_name>/SKILL.md
    """
    
    def __init__(self):
        # Scan paths: Global (~/.claude/skills) and Project (.claude/skills)
        self.scan_paths = []
        
        # 1. Project-level: config/skills
        # Consistent with config/agents and config/groups
        self.scan_paths.append(workspace.root / "config/skills")
        
        # 2. Global-level: ~/.soloqueue/skills
        # Use our own dotfile directory
        self.scan_paths.append(pathlib.Path.home() / ".soloqueue/skills")

    def load(self, name: str) -> SkillSchema:
        """
        Load a skill by name (searching project first, then global).
        Expects: .../skills/<name>/SKILL.md
        """
        for base_path in self.scan_paths:
            skill_dir = base_path / name
            file_path = skill_dir / "SKILL.md"
            
            if file_path.exists():
                return self._load_file(name, file_path)
                
        raise FileNotFoundError(f"Skill '{name}' not found in {self.scan_paths}")

    def _load_file(self, name: str, file_path: pathlib.Path) -> SkillSchema:
        try:
            post = frontmatter.load(str(file_path))
            
            data = post.metadata
            data['name'] = data.get('name', name)
            
            skill = SkillSchema(**data)
            skill.content = post.content
            skill.path = str(file_path.parent) # The directory containing SKILL.md
            
            logger.debug(f"Loaded skill: {skill.name} from {file_path}")
            return skill
            
        except Exception as e:
            logger.error(f"Failed to load skill {name} from {file_path}: {e}")
            raise e

    def load_all(self) -> Dict[str, SkillSchema]:
        """
        Load all skills from all scan paths. 
        Project overrides Global.
        """
        skills = {}
        
        # Iterate in reverse order (Global first, then Project overwrites)
        for base_path in reversed(self.scan_paths):
            if not base_path.exists():
                continue
                
            for skill_dir in base_path.iterdir():
                if skill_dir.is_dir() and (skill_dir / "SKILL.md").exists():
                    try:
                        name = skill_dir.name
                        # Skip if already loaded (unless we want project to overwrite, which we do by order)
                        # Actually, if we iterate Global -> Project, Project overwrites.
                        # Wait, list is [Project, Global]. reversed is [Global, Project]. Valid.
                        skill = self._load_file(name, skill_dir / "SKILL.md")
                        skills[name] = skill
                        skills[name] = skill
                    except Exception:
                        continue
        return skills

    def save(self, skill: SkillSchema):
        """Save a skill configuration."""
        # Determine save location (Default: project config/skills)
        # Even if loaded from global, edits should save to project overlay?
        # For now, yes.
        project_skills_dir = self.scan_paths[0] # First path is project config/skills
        
        skill_dir = project_skills_dir / skill.name
        if not skill_dir.exists():
            skill_dir.mkdir(parents=True, exist_ok=True)
            
        file_path = skill_dir / "SKILL.md"
        
        # Prepare data
        data = skill.model_dump(exclude={"content", "path"}, exclude_none=True)
        
        # Create Post
        post = frontmatter.Post(skill.content or "", **data)
        
        with open(file_path, "wb") as f:
            frontmatter.dump(post, f)
            
        logger.info(f"Saved skill {skill.name} to {file_path}")

    def delete(self, name: str) -> None:
        """Delete skill directory. Raises FileNotFoundError if not found."""
        for base_path in self.scan_paths:
            skill_dir = base_path / name
            if skill_dir.exists() and (skill_dir / "SKILL.md").exists():
                shutil.rmtree(skill_dir)
                logger.info(f"Deleted skill directory: {skill_dir}")
                return
        raise FileNotFoundError(f"Skill '{name}' not found in {self.scan_paths}")

