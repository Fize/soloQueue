
import json
import logging
import datetime
from typing import Dict, Any
from pathlib import Path

logger = logging.getLogger(__name__)

class SessionLogger:
    """
    Handles episodic memory logging (L2) for SoloQueue.
    Writes session data to both a structured JSONL file and a readable Markdown file.
    
    Directory Structure:
        .soloqueue/groups/{group}/sessions/{session_id}/
            ├── log.jsonl       # Machine-readable stream
            └── detailed.md     # Human-readable log
    """
    
    def __init__(self, workspace_root: str, group: str, session_id: str):
        self.workspace_root = Path(workspace_root)
        self.group = group
        self.session_id = session_id
        
        # Define paths
        self.session_dir = self.workspace_root / ".soloqueue" / "groups" / self.group / "sessions" / self.session_id
        self.jsonl_path = self.session_dir / "log.jsonl"
        self.md_path = self.session_dir / "detailed.md"
        
        # Ensure directory exists
        self.session_dir.mkdir(parents=True, exist_ok=True)
        
        # Initialize logs if new
        if not self.jsonl_path.exists():
            self._init_logs()
            
    def _init_logs(self):
        """Initialize log files with metadata header."""
        metadata = {
            "session_id": self.session_id,
            "group": self.group,
            "start_time": datetime.datetime.now().isoformat(),
            "type": "session_init"
        }
        
        # Write JSONL header
        with open(self.jsonl_path, "w") as f:
            f.write(json.dumps(metadata) + "\n")
            
        # Write Markdown header
        with open(self.md_path, "w") as f:
            f.write(f"# Session Log: {self.session_id}\n")
            f.write(f"**Group:** {self.group}\n")
            f.write(f"**Start Time:** {metadata['start_time']}\n\n")
            f.write("---\n\n")

    def log_step(self, step_data: Dict[str, Any]):
        """
        Log a single execution step.
        
        Args:
            step_data: Dictionary containing step details (input, output, tools, etc.)
        """
        timestamp = datetime.datetime.now().isoformat()
        step_data["timestamp"] = timestamp
        
        # 1. Append to JSONL
        try:
            with open(self.jsonl_path, "a") as f:
                f.write(json.dumps(step_data) + "\n")
        except Exception as e:
            logger.error(f"Failed to write to JSONL log: {e}")

        # 2. Append to Markdown
        try:
            self._append_to_markdown(step_data)
        except Exception as e:
            logger.error(f"Failed to write to Markdown log: {e}")

    def _append_to_markdown(self, data: Dict[str, Any]):
        """Format and append step data to Markdown log."""
        
        entry_type = data.get("type", "unknown")
        content = ""
        
        if entry_type == "user_input":
            content = f"## User Input\n\n> {data.get('content', '')}\n\n"
            
        elif entry_type == "agent_interaction":
            agent = data.get("agent", "Unknown Agent")
            content = f"### Agent: {agent}\n\n"
            
            # Thoughts/Reasoning
            if "thoughts" in data and data["thoughts"]:
                content += f"**Thinking:**\n> {data['thoughts']}\n\n"
            
            # Tool Calls
            tools = data.get("tool_calls", [])
            if tools:
                content += "**Tool Calls:**\n"
                for tool in tools:
                    content += f"- `{tool['name']}`: {json.dumps(tool['args'])}\n"
                content += "\n"
            
            # Final Response
            if "response" in data and data["response"]:
                content += f"**Response:**\n{data['response']}\n\n"

        elif entry_type == "tool_output":
            tool_name = data.get("tool", "unknown_tool")
            output = data.get("output", "")
            # Truncate long outputs in MD for readability
            if len(output) > 500:
                output = output[:500] + "... (truncated)"
            
            content = f"#### Tool Output ({tool_name})\n```\n{output}\n```\n\n"
            
        elif entry_type == "error":
             content = f"❌ **Error:** {data.get('error', 'Unknown error')}\n\n"

        if content:
            with open(self.md_path, "a") as f:
                f.write(content)
    
    def get_log_path(self) -> str:
        """Get the path to the JSONL log file."""
        return str(self.jsonl_path)

    def log_artifact(self, artifact_name: str, path: str):
        """Log an artifact creation event."""
        # JSONL
        event = {
            "type": "artifact_created",
            "name": artifact_name,
            "path": path,
            "timestamp": datetime.datetime.now().isoformat()
        }
        self.log_step(event)
        
        # Markdown specific addition
        with open(self.md_path, "a") as f:
            f.write(f"📦 **Artifact Created:** [{artifact_name}]({path})\n\n")
    
    def get_events(self, event_type: str | None = None) -> list[dict[str, Any]]:
        """
        Read all logged events from this session.
        
        This method enables session replay and analysis - a lightweight 
        alternative to complex checkpointing systems.
        
        Args:
            event_type: Optional filter by event type (e.g., "agent_interaction", 
                        "tool_output", "error"). If None, returns all events.
        
        Returns:
            List of event dictionaries in chronological order.
        
        Example:
            logger = SessionLogger(workspace, group, session_id)
            
            # Get all events
            events = logger.get_events()
            
            # Get only errors
            errors = logger.get_events(event_type="error")
            
            # Get agent interactions for replay
            interactions = logger.get_events(event_type="agent_interaction")
        """
        events: list[dict[str, Any]] = []
        
        if not self.jsonl_path.exists():
            return events
        
        try:
            with open(self.jsonl_path, 'r') as f:
                for line in f:
                    line = line.strip()
                    if not line:
                        continue
                    try:
                        event = json.loads(line)
                        if event_type is None or event.get("type") == event_type:
                            events.append(event)
                    except json.JSONDecodeError:
                        logger.warning(f"Skipping malformed JSON line in {self.jsonl_path}")
                        continue
        except Exception as e:
            logger.error(f"Failed to read session log: {e}")
        
        return events
    
    def get_session_metadata(self) -> dict[str, Any] | None:
        """
        Get session initialization metadata.
        
        Returns:
            Session metadata dict or None if not found.
        """
        events = self.get_events(event_type="session_init")
        return events[0] if events else None

