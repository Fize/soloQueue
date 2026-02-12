"""控制信号定义 - Agent 返回的执行控制指令。"""

from dataclasses import dataclass
from enum import Enum, auto


class SignalType(Enum):
    """控制信号类型。"""
    CONTINUE = auto()   # Agent 需要继续执行（工具调用后）
    DELEGATE = auto()   # Agent 请求委派给子 Agent
    RETURN = auto()     # Agent 完成任务，返回结果
    ERROR = auto()      # 发生错误
    USE_SKILL = auto()  # Agent 调用 Skill (作为 Sub-Agent)


@dataclass
class ControlSignal:
    """Agent 执行后返回的控制信号。"""
    type: SignalType
    
    # For DELEGATE
    target_agent: str | None = None
    instruction: str | None = None
    tool_call_id: str | None = None  # 委派工具调用的 ID
    
    # For USE_SKILL
    skill_name: str | None = None
    skill_args: str | None = None
    
    # For RETURN
    result: str | None = None
    
    # For ERROR
    error_msg: str | None = None
