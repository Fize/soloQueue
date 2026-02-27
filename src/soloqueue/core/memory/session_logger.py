"""
SessionLogger - AI调用日志系统

功能：
1. 记录完整的AI调用日志（结构化JSONL）
2. 支持Session恢复（从日志加载历史）
3. 包含工具调用、Skill调用等详细信息

存储位置: .soloqueue/logs/conversations.jsonl
"""

import json
import os
import uuid
from datetime import datetime
from pathlib import Path
from typing import Any, Optional
from dataclasses import dataclass, field, asdict

from langchain_core.messages import HumanMessage, AIMessage, BaseMessage

from soloqueue.core.logger import logger


@dataclass
class ToolCallRecord:
    """工具调用记录"""
    agent: str
    tool_name: str
    tool_args: dict[str, Any]
    result: Any
    timestamp: str
    duration_ms: int


@dataclass
class SkillCallRecord:
    """Skill调用记录"""
    skill_name: str
    skill_args: str
    agent: str
    result: str
    timestamp: str
    duration_ms: int


@dataclass
class AIResponse:
    """AI响应"""
    content: str
    thinking: Optional[str] = None


@dataclass
class TokenUsage:
    """Token使用统计"""
    prompt_tokens: int = 0
    completion_tokens: int = 0
    total_tokens: int = 0


@dataclass
class ConversationTurn:
    """一轮对话的完整记录"""
    session_id: str
    turn: int
    timestamp: str
    group: str
    entry_agent: str

    # 用户标识（多渠道支持）
    user_id: str = ""

    # 用户消息
    user_message: str = ""

    # AI响应
    ai_response: Optional[AIResponse] = None

    # 工具调用记录
    tool_calls: list[ToolCallRecord] = field(default_factory=list)

    # Skill调用记录
    skill_calls: list[SkillCallRecord] = field(default_factory=list)

    # Agent委派链
    delegation_chain: list[str] = field(default_factory=list)

    # Token使用
    token_usage: TokenUsage = field(default_factory=TokenUsage)

    # 执行时长
    duration_ms: int = 0

    # 状态
    status: str = "completed"  # completed, error, timeout

    def to_dict(self) -> dict:
        """转换为字典"""
        result = {
            "session_id": self.session_id,
            "turn": self.turn,
            "timestamp": self.timestamp,
            "group": self.group,
            "entry_agent": self.entry_agent,
            "user_message": self.user_message,
            "ai_response": asdict(self.ai_response) if self.ai_response else None,
            "tool_calls": [asdict(tc) for tc in self.tool_calls],
            "skill_calls": [asdict(sc) for sc in self.skill_calls],
            "delegation_chain": self.delegation_chain,
            "token_usage": asdict(self.token_usage),
            "duration_ms": self.duration_ms,
            "status": self.status,
        }
        if self.user_id:
            result["user_id"] = self.user_id
        return result


class SessionLogger:
    """
    AI调用日志管理器
    
    功能：
    - save_turn(): 保存一轮对话日志
    - get_history(): 获取session历史（用于恢复）
    - clear_session(): 清理session日志
    """
    
    def __init__(self, workspace_root: str = "."):
        self.workspace_root = Path(workspace_root)
        self.logs_dir = self.workspace_root / ".soloqueue" / "logs"
        self.logs_dir.mkdir(parents=True, exist_ok=True)
        self.log_file = self.logs_dir / "conversations.jsonl"
        
    def _generate_session_id(self) -> str:
        """生成新的session_id"""
        return str(uuid.uuid4())
    
    def save_turn(self, turn: ConversationTurn) -> None:
        """
        保存一轮对话日志
        
        Args:
            turn: 对话记录对象
        """
        try:
            with open(self.log_file, "a", encoding="utf-8") as f:
                f.write(json.dumps(turn.to_dict(), ensure_ascii=False) + "\n")
            logger.debug(f"Saved conversation turn: session={turn.session_id}, turn={turn.turn}")
        except Exception as e:
            logger.error(f"Failed to save conversation turn: {e}")
    
    def get_history(self, session_id: str, limit: int = 50) -> list[BaseMessage]:
        """
        获取session历史消息（用于恢复上下文）
        
        Args:
            session_id: 会话ID
            limit: 最大历史条数
            
        Returns:
            消息列表 [HumanMessage, AIMessage, ...]
        """
        messages: list[BaseMessage] = []
        
        if not self.log_file.exists():
            return messages
        
        try:
            turns = []
            with open(self.log_file, "r", encoding="utf-8") as f:
                for line in f:
                    line = line.strip()
                    if not line:
                        continue
                    try:
                        record = json.loads(line)
                        if record.get("session_id") == session_id:
                            turns.append(record)
                    except json.JSONDecodeError:
                        continue
            
            # 按turn排序，取最近的limit条
            turns = sorted(turns, key=lambda x: x.get("turn", 0))[-limit:]
            
            # 重建消息列表
            for turn in turns:
                user_msg = turn.get("user_message")
                if user_msg:
                    messages.append(HumanMessage(content=user_msg))
                
                ai_response = turn.get("ai_response")
                if ai_response:
                    content = ai_response.get("content", "")
                    messages.append(AIMessage(content=content))
            
            logger.debug(f"Loaded {len(messages)} messages for session={session_id}")
            return messages
            
        except Exception as e:
            logger.error(f"Failed to load session history: {e}")
            return messages
    
    def get_turns(self, session_id: str) -> list[dict]:
        """
        获取session的完整turn记录（用于调试/分析）
        
        Args:
            session_id: 会话ID
            
        Returns:
            turn记录列表
        """
        turns = []
        
        if not self.log_file.exists():
            return turns
        
        try:
            with open(self.log_file, "r", encoding="utf-8") as f:
                for line in f:
                    line = line.strip()
                    if not line:
                        continue
                    try:
                        record = json.loads(line)
                        if record.get("session_id") == session_id:
                            turns.append(record)
                    except json.JSONDecodeError:
                        continue
            
            return sorted(turns, key=lambda x: x.get("turn", 0))
            
        except Exception as e:
            logger.error(f"Failed to load session turns: {e}")
            return turns
    
    def clear_session(self, session_id: str) -> bool:
        """
        清理session日志（软删除，标记为cleared）
        
        Args:
            session_id: 会话ID
            
        Returns:
            是否成功
        """
        if not self.log_file.exists():
            return True
        
        try:
            # 读取所有记录
            lines = []
            with open(self.log_file, "r", encoding="utf-8") as f:
                for line in f:
                    line = line.strip()
                    if not line:
                        continue
                    try:
                        record = json.loads(line)
                        if record.get("session_id") == session_id:
                            continue  # 跳过要删除的session
                        lines.append(line)
                    except json.JSONDecodeError:
                        continue
            
            # 重写文件
            with open(self.log_file, "w", encoding="utf-8") as f:
                for line in lines:
                    f.write(line + "\n")
            
            logger.info(f"Cleared session: {session_id}")
            return True
            
        except Exception as e:
            logger.error(f"Failed to clear session: {e}")
            return False
    
    def get_sessions_by_user(self, user_id: str) -> list[str]:
        """
        获取指定用户的所有 session_id 列表（去重、按首次出现排序）。

        兼容旧数据：如果记录中无 user_id 字段，则跳过。

        Args:
            user_id: 用户标识

        Returns:
            session_id 列表，按出现顺序排序
        """
        sessions: list[str] = []
        seen: set[str] = set()

        if not self.log_file.exists():
            return sessions

        try:
            with open(self.log_file, "r", encoding="utf-8") as f:
                for line in f:
                    line = line.strip()
                    if not line:
                        continue
                    try:
                        record = json.loads(line)
                        record_user = record.get("user_id", "")
                        session_id = record.get("session_id", "")
                        if record_user == user_id and session_id and session_id not in seen:
                            sessions.append(session_id)
                            seen.add(session_id)
                    except json.JSONDecodeError:
                        continue

            return sessions

        except Exception as e:
            logger.error(f"Failed to get sessions for user {user_id}: {e}")
            return sessions

    def get_session_turns_text(self, session_id: str) -> str:
        """
        获取 session 所有对话轮次的文本拼接（用于 RAG 归档）。

        格式：
            User: xxx
            AI: xxx
            ---
            User: xxx
            AI: xxx

        Args:
            session_id: 会话ID

        Returns:
            拼接后的对话文本，空 session 返回空字符串
        """
        turns = self.get_turns(session_id)
        if not turns:
            return ""

        parts = []
        for turn in turns:
            user_msg = turn.get("user_message", "")
            ai_resp = turn.get("ai_response", {})
            ai_content = ai_resp.get("content", "") if ai_resp else ""

            turn_text = f"User: {user_msg}\nAI: {ai_content}"
            parts.append(turn_text)

        return "\n---\n".join(parts)

    def get_session_count(self) -> int:
        """获取总session数量"""
        sessions = set()
        
        if not self.log_file.exists():
            return 0
        
        try:
            with open(self.log_file, "r", encoding="utf-8") as f:
                for line in f:
                    line = line.strip()
                    if not line:
                        continue
                    try:
                        record = json.loads(line)
                        session_id = record.get("session_id")
                        if session_id:
                            sessions.add(session_id)
                    except json.JSONDecodeError:
                        continue
            
            return len(sessions)
            
        except Exception as e:
            logger.error(f"Failed to count sessions: {e}")
            return 0


# 便捷函数
_session_logger: Optional[SessionLogger] = None


def get_session_logger(workspace_root: str = ".") -> SessionLogger:
    """获取全局SessionLogger实例"""
    global _session_logger
    if _session_logger is None:
        _session_logger = SessionLogger(workspace_root)
    return _session_logger
