"""
Session Summarizer: Automated LLM-driven session compression

This module generates concise summaries of agent sessions for:
1. Human readability (quick session review)
2. Semantic indexing (embed summaries instead of full logs)
3. Knowledge extraction (identify key learnings)

Design Principles:
1. Async by default: Don't block session close
2. Structured output: Markdown with consistent sections
3. Cost-aware: Use cheap models for routine sessions
4. Event filtering: Focus on key moments, ignore noise
"""

from typing import Any, TypedDict
from datetime import datetime
import json
from loguru import logger


class SessionSummary(TypedDict):
    """Structured session summary."""
    session_id: str
    agent_name: str
    task_description: str
    outcome: str  # "success" | "failure" | "partial"
    duration_seconds: float
    key_actions: list[str]
    errors_encountered: list[str]
    learnings: list[str]
    tags: list[str]
    full_summary_md: str  # Markdown formatted


class SessionSummarizer:
    """
    LLM-driven session summarization engine.
    
    Converts verbose session logs into concise, searchable summaries.
    
    Example:
        >>> summarizer = SessionSummarizer(model="gpt-4o-mini")
        >>> summary = summarizer.summarize(session_id="abc-123")
        >>> 
        >>> print(summary['full_summary_md'])
        ### Session Summary: Implement User Authentication
        **Outcome**: ✅ Success
        
        #### 🎯 Goal
        Implement JWT-based authentication for the API.
        
        #### 🔧 Actions Taken
        - Created User model with password hashing
        - Implemented /login and /register endpoints
        - Added JWT token generation and validation middleware
        
        #### 💡 Key Learnings
        - bcrypt is preferred over SHA256 for password hashing
        - JWT refresh tokens should have longer expiry than access tokens
    """
    
    SUMMARY_PROMPT_TEMPLATE = """You are a technical documentation specialist summarizing a software development session.

## Input Data
{session_data}

## Your Task
Generate a concise, structured summary following this format:

### Session Summary: {task_title}

**Agent**: {agent_name}  
**Duration**: {duration}  
**Outcome**: ✅ Success / ❌ Failure / ⚠️ Partial

#### 🎯 Goal
One-sentence description of what the user wanted to achieve.

#### 🔧 Actions Taken
- Maximum 5-7 bullet points
- Focus on major decisions and implementations
- Skip minor tool calls and intermediate steps

#### 📊 Outcome
What was the final result? What artifacts were created?

#### 💡 Key Learnings
- Errors encountered and their resolutions
- Design decisions and rationale
- Reusable patterns or techniques discovered

**Tags**: Comma-separated keywords (max 5)

## Guidelines
- Be specific but concise (200-500 words total)
- Use technical terminology appropriately
- Focus on actionable insights
- Highlight reusable knowledge
"""
    
    def __init__(self, model: str = "gpt-4o-mini"):
        """
        Initialize summarizer.
        
        Args:
            model: LLM model to use for summarization
        """
        self.model = model
        # Lazy import to avoid circular dependency
        from soloqueue.core.llm import ModelAdapterFactory
        self.llm = ModelAdapterFactory.create(model)
    
    def summarize(self, session_id: str, session_log_path: str) -> SessionSummary:
        """
        Generate summary for a session.
        
        Args:
            session_id: Session identifier
            session_log_path: Path to session JSONL file
        
        Returns:
            Structured session summary
        """
        # 1. Load session log
        events = self._load_session_events(session_log_path)
        
        # 2. Extract key events (filter noise)
        key_events = self._extract_key_events(events)
        
        # 3. Prepare LLM context
        session_data = self._format_for_llm(key_events)
        
        # 4. Generate summary
        prompt = self.SUMMARY_PROMPT_TEMPLATE.format(
            session_data=session_data,
            task_title=key_events.get("task_title", "Unknown Task"),
            agent_name=key_events.get("agent_name", "Unknown Agent"),
            duration=self._format_duration(key_events.get("duration", 0))
        )
        
        logger.info(f"Generating summary for session {session_id}")
        summary_md = self.llm.invoke(prompt).content
        
        # 5. Parse structured data from summary
        structured_summary = self._parse_summary(summary_md, key_events)
        structured_summary["session_id"] = session_id
        structured_summary["full_summary_md"] = summary_md
        
        return structured_summary
    
    def _load_session_events(self, log_path: str) -> list[dict[str, Any]]:
        """Load JSONL session log."""
        events = []
        with open(log_path, 'r') as f:
            for line in f:
                if line.strip():
                    events.append(json.loads(line))
        return events
    
    def _extract_key_events(self, events: list[dict[str, Any]]) -> dict[str, Any]:
        """
        Filter session log to key events only.
        
        Keep:
        - Session metadata (init event)
        - First user message (task description)
        - Agent interactions with tool calls
        - Errors
        - Final response
        
        Discard:
        - Intermediate reasoning
        - Repetitive tool outputs
        - Progress messages
        """
        key_data = {
            "agent_name": None,
            "task_title": None,
            "start_time": None,
            "end_time": None,
            "interactions": [],
            "tool_calls": [],
            "errors": []
        }
        
        for event in events:
            event_type = event.get("type")
            
            if event_type == "session_init":
                key_data["agent_name"] = event.get("agent_name", "Unknown")
                key_data["start_time"] = event.get("timestamp")
            
            elif event_type == "agent_interaction":
                # First interaction often contains task description
                if not key_data["task_title"] and event.get("input"):
                    key_data["task_title"] = event["input"][:100]  # First 100 chars
                
                # Keep interactions with tool calls
                if event.get("tool_calls"):
                    key_data["interactions"].append(event)
                    key_data["tool_calls"].extend(event["tool_calls"])
            
            elif event_type == "tool_output":
                key_data["tool_calls"].append(event)
            
            elif event_type == "error":
                key_data["errors"].append(event)
            
            # Track last timestamp
            if "timestamp" in event:
                key_data["end_time"] = event["timestamp"]
        
        # Calculate duration
        if key_data["start_time"] and key_data["end_time"]:
            start = datetime.fromisoformat(key_data["start_time"])
            end = datetime.fromisoformat(key_data["end_time"])
            key_data["duration"] = (end - start).total_seconds()
        
        return key_data
    
    def _format_for_llm(self, key_events: dict[str, Any]) -> str:
        """Format key events as readable text for LLM."""
        lines = []
        
        lines.append(f"**Task**: {key_events.get('task_title', 'N/A')}")
        lines.append(f"**Agent**: {key_events.get('agent_name', 'N/A')}")
        lines.append("")
        
        lines.append("**Key Interactions**:")
        for interaction in key_events.get("interactions", [])[:10]:  # Max 10
            lines.append(f"- {interaction.get('input', '')[:200]}")  # Truncate
            if interaction.get("tool_calls"):
                for tc in interaction["tool_calls"]:
                    lines.append(f"  → Tool: {tc.get('name', 'unknown')}")
        
        if key_events.get("errors"):
            lines.append("")
            lines.append("**Errors Encountered**:")
            for error in key_events["errors"]:
                lines.append(f"- {error.get('error', 'Unknown error')}")
        
        return "\n".join(lines)
    
    def _parse_summary(self, summary_md: str, key_events: dict[str, Any]) -> SessionSummary:
        """Extract structured data from generated summary."""
        # Basic parsing (can be enhanced with regex)
        outcome = "success"  # Default
        if "❌ Failure" in summary_md:
            outcome = "failure"
        elif "⚠️ Partial" in summary_md:
            outcome = "partial"
        
        # Extract tags (simple approach)
        tags = []
        if "**Tags**:" in summary_md:
            tags_line = summary_md.split("**Tags**:")[-1].split("\n")[0]
            tags = [t.strip() for t in tags_line.split(",")]
        
        return SessionSummary(
            session_id="",  # Filled by caller
            agent_name=key_events.get("agent_name", "Unknown"),
            task_description=key_events.get("task_title", "Unknown"),
            outcome=outcome,
            duration_seconds=key_events.get("duration", 0),
            key_actions=[],  # TODO: Parse from summary
            errors_encountered=[e.get("error", "") for e in key_events.get("errors", [])],
            learnings=[],  # TODO: Parse from summary
            tags=tags,
            full_summary_md=""  # Filled by caller
        )
    
    def _format_duration(self, seconds: float) -> str:
        """Format duration as human-readable string."""
        if seconds < 60:
            return f"{int(seconds)}s"
        elif seconds < 3600:
            return f"{int(seconds/60)}m {int(seconds%60)}s"
        else:
            hours = int(seconds / 3600)
            minutes = int((seconds % 3600) / 60)
            return f"{hours}h {minutes}m"


# Example usage
if __name__ == "__main__":
    # Demo: Create mock session log
    import tempfile
    import os
    
    with tempfile.TemporaryDirectory() as tmpdir:
        log_path = os.path.join(tmpdir, "session.jsonl")
        
        # Write mock events
        with open(log_path, 'w') as f:
            f.write(json.dumps({
                "type": "session_init",
                "agent_name": "code_agent",
                "timestamp": "2026-02-08T10:00:00"
            }) + "\n")
            
            f.write(json.dumps({
                "type": "agent_interaction",
                "input": "Implement user authentication with JWT",
                "response": "I'll create the authentication system",
                "tool_calls": [{"name": "write_file", "args": {"path": "auth.py"}}]
            }) + "\n")
            
            f.write(json.dumps({
                "type": "error",
                "error": "ImportError: No module named 'jwt'",
                "timestamp": "2026-02-08T10:05:00"
            }) + "\n")
        
        # NOTE: This requires actual LLM to run
        # summarizer = SessionSummarizer("gpt-4o-mini")
        # summary = summarizer.summarize("demo-session", log_path)
        # print(summary['full_summary_md'])
        
        print("Session summarizer demo (requires LLM to run)")
