import frontmatter
from typing import Dict

from soloqueue.core.loaders.schema import SkillSchema
from soloqueue.core.workspace import workspace
from soloqueue.core.logger import logger

class SkillLoader:
    """
    Loads Skill configurations from SKILL.md files.
    Structure: skills/<skill_name>/SKILL.md
    """
    
    def __init__(self, skills_dir: str = "skills"):
        try:
            self.skills_root = workspace.resolve_path(skills_dir)
        except Exception:
            self.skills_root = workspace.root / skills_dir
            
    def load(self, name: str) -> SkillSchema:
        """
        Load a specific skill by directory name.
        """
        skill_dir = self.skills_root / name
        file_path = skill_dir / "SKILL.md"
        
        if not file_path.exists():
            raise FileNotFoundError(f"Skill config not found: {file_path}")
            
        try:
            post = frontmatter.load(str(file_path))
            
            data = post.metadata
            data['name'] = data.get('name', name)
            
            skill = SkillSchema(**data)
            skill.instructions = post.content
            skill.scripts_dir = str(skill_dir / "scripts")
            
            logger.debug(f"Loaded skill: {skill.name}")
            return skill
            
        except Exception as e:
            logger.error(f"Failed to load skill {name}: {e}")
            raise e

    def load_all(self) -> Dict[str, SkillSchema]:
        skills = {}
        if not self.skills_root.exists():
            return skills
            
        for skill_dir in self.skills_root.iterdir():
            if skill_dir.is_dir() and (skill_dir / "SKILL.md").exists():
                try:
                    name = skill_dir.name
                    skills[name] = self.load(name)
                except Exception:
                    continue
        return skills
