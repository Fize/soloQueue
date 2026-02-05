
import pytest
from soloqueue.core.primitives.search import grep, glob
from soloqueue.core.primitives.file_io import read_file, write_file
from soloqueue.core.workspace import workspace

@pytest.fixture
def clean_ws(tmp_path):
    workspace.root = tmp_path
    return tmp_path

def test_read_write_file(clean_ws):
    # Write
    res = write_file("test.txt", "Hello World", require_approval=False)
    assert res["success"] is True
    assert (clean_ws / "test.txt").read_text() == "Hello World"
    
    # Read
    res = read_file("test.txt")
    assert res["success"] is True
    assert res["output"] == "Hello World"

def test_grep_search(clean_ws):
    (clean_ws / "a.py").write_text("def foo():\n    pass")
    (clean_ws / "b.txt").write_text("foo bar")
    
    res = grep("foo", ".")
    assert res["success"] is True
    assert "a.py:1: def foo():" in res["output"]
    assert "b.txt:1: foo bar" in res["output"]

def test_glob_search(clean_ws):
    (clean_ws / "src").mkdir()
    (clean_ws / "src/main.py").touch()
    (clean_ws / "README.md").touch()
    
    res = glob("**/*.py", ".")
    assert res["success"] is True
    assert "src/main.py" in res["output"]
    assert "README.md" not in res["output"]
