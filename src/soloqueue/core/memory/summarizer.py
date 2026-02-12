"""
Session Summarizer - Automatic session log summarization using LLM

Converts lengthy JSONL session logs into concise, structured summaries with:
- Human-readable markdown summary
- Extracted key learnings
- Structured metadata (objective, duration, outcome)
"""

from typing import Optional
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
import json

from loguru import logger


@dataclass
class SessionSummary:
    """Structured session summary."""
    session_id: str
    objective: str
    duration_seconds: int
    outcome: str  # "success", "failure", "partial"
    difficulty: int  # 1-10
    key_learnings: list[str]
    markdown: str  # Full markdown summary
    timestamp: str


@dataclass
class SessionEvent:
    """A single event from session log."""
    timestamp: str
    event_type: str  # "llm_call", "tool_use", "error", "success"
    content: str
    metadata: dict


class SessionSummarizer:
    """
    LLM-driven session summarizer.
    
    Analyzes session logs and generates structured summaries with key learnings.
    
    Example:
        summarizer = SessionSummarizer(model="gpt-4o-mini")
        
        summary = summarizer.summarize(
            session_id="abc123",
            session_log_path="/path/to/session.jsonl"
        )
        
        print(summary.markdown)
        for learning in summary.key_learnings:
            print(f"- {learning}")
    """
    
    def __init__(
        self,
        model: str = "gpt-4o-mini",
        api_key: Optional[str] = None,
        max_events: int = 100
    ):
        """
        Initialize session summarizer.
        
        Args:
            model: LLM model to use for summarization
            api_key: Optional API key (falls back to env var)
            max_events: Maximum number of events to include in summary
        """
        self.model = model
        self.max_events = max_events
        
        # Initialize LLM client
        try:
            from openai import OpenAI
            import os
            
            self.client = OpenAI(api_key=api_key or os.getenv("OPENAI_API_KEY"))
            logger.info(f"Initialized SessionSummarizer with model: {model}")
        except ImportError:
            raise RuntimeError(
                "openai package required for summarization. "
                "Install with: pip install openai"
            )
    
    def summarize(
        self,
        session_id: str,
        session_log_path: str
    ) -> SessionSummary:
        """
        Generate summary from session log.
        
        Args:
            session_id: Session identifier
            session_log_path: Path to JSONL session log
        
        Returns:
            SessionSummary with structured data
        """
        logger.info(f"Summarizing session: {session_id}")
        
        # Load and parse events
        events = self._load_events(session_log_path)
        
        if not events:
            logger.warning(f"No events found in {session_log_path}")
            return self._create_empty_summary(session_id)
        
        # Extract key events
        key_events = self._extract_key_events(events)
        
        # Calculate duration
        duration = self._calculate_duration(events)
        
        # Generate summary using LLM
        llm_response = self._call_llm(session_id, key_events, duration)
        
        # Parse and structure response
        summary = self._parse_llm_response(session_id, llm_response, duration)
        
        logger.info(
            f"Summary generated: {summary.outcome}, "
            f"{len(summary.key_learnings)} learnings"
        )
        
        return summary
    
    def _load_events(self, log_path: str) -> list[SessionEvent]:
        """Load events from JSONL log file."""
        events = []
        path = Path(log_path)
        
        if not path.exists():
            logger.warning(f"Log file not found: {log_path}")
            return events
        
        try:
            with open(path, 'r', encoding='utf-8') as f:
                for line in f:
                    if line.strip():
                        data = json.loads(line)
                        events.append(SessionEvent(
                            timestamp=data.get('timestamp', ''),
                            event_type=data.get('event_type', 'unknown'),
                            content=data.get('content', ''),
                            metadata=data.get('metadata', {})
                        ))
        except Exception as e:
            logger.error(f"Failed to load events from {log_path}: {e}")
        
        return events
    
    def _extract_key_events(self, events: list[SessionEvent]) -> list[SessionEvent]:
        """
        Extract most important events from full log.
        
        Filters out redundant events and keeps only significant ones.
        """
        # Simple filtering: keep errors, successes, and sample of normal events
        key_events = []
        
        # Always include errors and successes
        for event in events:
            if event.event_type in ('error', 'success', 'failure'):
                key_events.append(event)
        
        # Add sample of other events (evenly spaced)
        other_events = [e for e in events if e.event_type not in ('error', 'success', 'failure')]
        
        if other_events:
            step = max(1, len(other_events) // (self.max_events - len(key_events)))
            key_events.extend(other_events[::step])
        
        # Sort by timestamp
        key_events.sort(key=lambda e: e.timestamp)
        
        # Limit total
        return key_events[:self.max_events]
    
    def _calculate_duration(self, events: list[SessionEvent]) -> int:
        """Calculate session duration in seconds."""
        if len(events) < 2:
            return 0
        
        try:
            start = datetime.fromisoformat(events[0].timestamp.replace('Z', '+00:00'))
            end = datetime.fromisoformat(events[-1].timestamp.replace('Z', '+00:00'))
            return int((end - start).total_seconds())
        except Exception as e:
            logger.warning(f"Failed to calculate duration: {e}")
            return 0
    
    def _call_llm(
        self,
        session_id: str,
        events: list[SessionEvent],
        duration: int
    ) -> dict:
        """Call LLM to generate summary."""
        # Build prompt
        events_text = self._format_events_for_llm(events)
        
        prompt = f"""Analyze this agent session log and extract key information.

Session ID: {session_id}
Duration: {duration} seconds
Total Events: {len(events)}

Events:
{events_text}

Generate a structured summary with:
1. **Objective**: What was the agent trying to accomplish?
2. **Outcome**: success, failure, or partial
3. **Key Learnings**: 3-5 important lessons or insights (as a list)
4. **Difficulty**: Rate 1-10 how challenging this task was
5. **Summary**: 2-3 paragraph markdown summary of what happened

Respond in JSON format:
{{
  "objective": "Brief description of the goal",
  "outcome": "success|failure|partial",
  "key_learnings": [
    "Learning 1: Specific insight or pattern",
    "Learning 2: Another important lesson"
  ],
  "difficulty": 5,
  "summary": "## Session Summary\\n\\nMarkdown content here..."
}}

Focus on extracting reusable knowledge, patterns, and gotchas that would help in future similar tasks.
"""
        
        try:
            response = self.client.chat.completions.create(
                model=self.model,
                messages=[
                    {
                        "role": "system",
                        "content": "You are an expert at analyzing software development sessions and extracting actionable knowledge."
                    },
                    {
                        "role": "user",
                        "content": prompt
                    }
                ],
                temperature=0.3,  # Lower temperature for more consistent output
                response_format={"type": "json_object"}
            )
            
            result = json.loads(response.choices[0].message.content)
            return result
            
        except Exception as e:
            logger.error(f"LLM call failed: {e}")
            return self._fallback_summary()
    
    def _format_events_for_llm(self, events: list[SessionEvent]) -> str:
        """Format events into human-readable text for LLM."""
        lines = []
        for i, event in enumerate(events, 1):
            # Truncate long content
            content = event.content[:200] + "..." if len(event.content) > 200 else event.content
            lines.append(f"{i}. [{event.event_type}] {content}")
        
        return "\n".join(lines)
    
    def _fallback_summary(self) -> dict:
        """Fallback summary if LLM call fails."""
        return {
            "objective": "Unknown",
            "outcome": "partial",
            "key_learnings": ["Unable to generate summary - LLM call failed"],
            "difficulty": 5,
            "summary": "## Summary\n\nFailed to generate automated summary."
        }
    
    def _parse_llm_response(
        self,
        session_id: str,
        llm_response: dict,
        duration: int
    ) -> SessionSummary:
        """Parse LLM response into SessionSummary."""
        return SessionSummary(
            session_id=session_id,
            objective=llm_response.get("objective", "Unknown"),
            duration_seconds=duration,
            outcome=llm_response.get("outcome", "partial"),
            difficulty=llm_response.get("difficulty", 5),
            key_learnings=llm_response.get("key_learnings", []),
            markdown=llm_response.get("summary", ""),
            timestamp=datetime.now().isoformat()
        )
    
    def _create_empty_summary(self, session_id: str) -> SessionSummary:
        """Create empty summary for sessions with no events."""
        return SessionSummary(
            session_id=session_id,
            objective="Empty session",
            duration_seconds=0,
            outcome="failure",
            difficulty=0,
            key_learnings=[],
            markdown="## Empty Session\n\nNo events recorded.",
            timestamp=datetime.now().isoformat()
        )


def summarize_session(
    session_id: str,
    session_log_path: str,
    model: str = "gpt-4o-mini"
) -> SessionSummary:
    """
    Convenience function to summarize a session.
    
    Args:
        session_id: Session identifier
        session_log_path: Path to JSONL log file
        model: LLM model to use
    
    Returns:
        SessionSummary object
    """
    summarizer = SessionSummarizer(model=model)
    return summarizer.summarize(session_id, session_log_path)


if __name__ == "__main__":
    import sys
    
    if len(sys.argv) < 3:
        print("Usage: python summarizer.py <session_id> <log_path>")
        sys.exit(1)
    
    session_id = sys.argv[1]
    log_path = sys.argv[2]
    
    print(f"Summarizing session: {session_id}")
    print(f"Log file: {log_path}")
    print()
    
    summary = summarize_session(session_id, log_path)
    
    print("=" * 70)
    print("SESSION SUMMARY")
    print("=" * 70)
    print()
    print(summary.markdown)
    print()
    print("=" * 70)
    print(f"Objective: {summary.objective}")
    print(f"Outcome: {summary.outcome}")
    print(f"Duration: {summary.duration_seconds}s")
    print(f"Difficulty: {summary.difficulty}/10")
    print()
    print("Key Learnings:")
    for i, learning in enumerate(summary.key_learnings, 1):
        print(f"  {i}. {learning}")
