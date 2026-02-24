
from langchain_core.tools import tool, BaseTool
from soloqueue.core.memory.manager import MemoryManager

def create_artifact_tools(memory: MemoryManager) -> list[BaseTool]:
    """
    Create tools bound to a specific MemoryManager instance.
    """
    
    @tool("save_artifact")
    def save_artifact(content: str, title: str, 
                      tags: str = "", artifact_type: str = "text") -> str:

        """
        Save content as a permanent artifact. 
        Args:
            content: The text/code content to save.
            title: A short descriptive title.
            tags: Comma-separated tags (e.g. "code,python,utils").
            artifact_type: Type of artifact (text, report, code, etc.).
        Returns: 
            The Artifact ID.
        """
        tag_list = [t.strip() for t in tags.split(",") if t.strip()]
        
        art_id = memory.save_artifact(
            content=content,
            title=title,
            tags=tag_list,
            artifact_type=artifact_type,
            author="agent" # we could pass actual agent name if we bind it later, but 'agent' is fine
        )
        return f"Artifact saved successfully. ID: {art_id}"

    @tool("read_artifact")
    def read_artifact(artifact_id: str) -> str:
        """
        Read the content of an artifact by ID.
        """
        art = memory.get_artifact(artifact_id)
        if not art:
            return f"Error: Artifact {artifact_id} not found."
        
        metadata = art.get("metadata", {})
        content = art.get("content", "")
        return f"Title: {metadata.get('title')}\nType: {metadata.get('type')}\nContent:\n{content}"

    @tool("list_artifacts")
    def list_artifacts(tag: str = "") -> str:
        """
        List all artifacts available in this group, optionally filtered by a tag.
        """
        tag_filter = tag if tag else None
        artifacts = memory.list_artifacts(tag_filter)
        
        if not artifacts:
            return "No artifacts found."
            
        result = "Available Artifacts:\n"
        for art in artifacts:
            tags_str = ", ".join(art.get('tags', []))
            result += f"- [{art.get('id')}] {art.get('title')} (Type: {art.get('artifact_type', 'text')}, Tags: {tags_str})\n"
        return result

    @tool("delete_artifact")
    def delete_artifact(artifact_id: str) -> str:
        """
        Delete an artifact by its metadata ID.
        Note: This only removes the entry from the index. Orphaned blob files 
        will be cleaned up by the Garbage Collector later.
        """
        success = memory.delete_artifact(artifact_id)
        if success:
            return f"Artifact {artifact_id} deleted successfully."
        else:
            return f"Error: Artifact {artifact_id} not found or could not be deleted."

    return [save_artifact, read_artifact, list_artifacts, delete_artifact]

