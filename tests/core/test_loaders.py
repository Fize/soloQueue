
import pytest
from soloqueue.core.loaders.agent_loader import AgentLoader
from soloqueue.core.loaders.skill_loader import SkillLoader
from soloqueue.core.loaders.schema import AgentSchema, SkillSchema
from soloqueue.core.workspace import workspace

@pytest.fixture
def mock_config(tmp_path):
    workspace.root = tmp_path
    
    # Setup directories
    (tmp_path / "config" / "agents").mkdir(parents=True)
    (tmp_path / "skills").mkdir()
    
    return tmp_path

def test_load_agent(mock_config):
    agent_file = mock_config / "config" / "agents" / "tester.md"
    agent_file.write_text("""---
name: tester
description: A test agent.
tools: ["read_file"]
sub_agents: ["subby"]
---
You are a tester.
""")
    
    loader = AgentLoader("config/agents")
    agent = loader.load("tester")
    
    assert agent.name == "tester"
    assert agent.description == "A test agent."
    assert "read_file" in agent.tools
    assert "subby" in agent.sub_agents
    assert "You are a tester." in agent.system_prompt.strip()

def test_load_skill(mock_config):
    skill_dir = mock_config / "skills" / "search"
    skill_dir.mkdir()
    (skill_dir / "SKILL.md").write_text("""---
name: search
description: Search skill.
input_schema:
  type: object
  properties:
    query: {type: string}
---
Search instructions.
""")
    
    loader = SkillLoader("skills")
    skill = loader.load("search")
    
    assert skill.name == "search"
    assert skill.input_schema["properties"]["query"]["type"] == "string"
    assert "Search instructions" in skill.instructions
