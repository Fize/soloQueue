from typing import List

from langchain_core.tools import BaseTool, StructuredTool

from soloqueue.core.primitives.bash import bash
from soloqueue.core.primitives.file_io import read_file, write_file
from soloqueue.core.primitives.search import grep, glob
from soloqueue.core.primitives.web import web_fetch

def get_all_primitives() -> List[BaseTool]:
    """
    Get all Layer 1 primitives packaged as LangChain Tools.
    This serves as the single source of truth for base capabilities.
    """
    return [
        StructuredTool.from_function(
            func=bash,
            name="bash",
            description="Execute safe shell commands. Use this for file operations not covered by other tools."
        ),
        StructuredTool.from_function(
            func=read_file,
            name="read_file",
            description="Read content of a file from the workspace."
        ),
        StructuredTool.from_function(
            func=write_file,
            name="write_file",
            description="Write content to a file in the workspace. Requires approval."
        ),
        StructuredTool.from_function(
            func=grep,
            name="grep",
            description="Search for a regex pattern in files."
        ),
        StructuredTool.from_function(
            func=glob,
            name="glob",
            description="Find files matching a glob pattern."
        ),
        StructuredTool.from_function(
            func=web_fetch,
            name="web_fetch",
            description="Fetch a URL and return its content as Markdown."
        ),
    ]
