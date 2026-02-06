from soloqueue.core.loaders.schema import AgentSchema, SkillSchema, GroupSchema
from soloqueue.core.loaders.agent_loader import AgentLoader as AgentLoader
from soloqueue.core.loaders.skill_loader import SkillLoader as SkillLoader
from soloqueue.core.loaders.group_loader import GroupLoader as GroupLoader

# Convenience exports
AgentConfig = AgentSchema 
SkillConfig = SkillSchema
GroupConfig = GroupSchema
