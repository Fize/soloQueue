
from soloqueue.core.primitives import get_all_primitives
from langchain_core.tools import StructuredTool

def test_registry_returns_tools():
    tools = get_all_primitives()
    assert len(tools) == 6
    assert isinstance(tools[0], StructuredTool)
    
    names = {t.name for t in tools}
    assert "bash" in names
    assert "read_file" in names
    assert "web_fetch" in names
