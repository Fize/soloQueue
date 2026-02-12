"""TaskFrame - 运行时执行上下文，类似函数调用栈帧。"""

from dataclasses import dataclass, field
from typing import Any

from langchain_core.messages import AnyMessage


@dataclass
class TaskFrame:
    """
    运行时上下文，类似于函数调用的栈帧。
    
    每个 Agent 调用都有一个独立的 TaskFrame，保存：
    - 隔离的消息历史 (memory)
    - 任务级别的状态 (state)
    - 执行结果 (result)
    """
    
    # 执行此 Frame 的 Agent 名称 (e.g., "investment__leader")
    agent_name: str
    
    # 隔离的消息历史（仅此任务可见）
    memory: list[AnyMessage] = field(default_factory=list)
    
    # 任务级别的变量（可用于传递 artifacts）
    state: dict[str, Any] = field(default_factory=dict)
    
    # 父 Frame 发送的指令（用于初始化 memory）
    instruction: str = ""
    
    # 父 Frame 的 delegate_to 工具调用 ID（用于返回 ToolMessage）
    parent_tool_call_id: str | None = None
    
    # 执行结果（任务完成时设置）
    result: str | None = None
    
    # 动态配置（用于 Ad-hoc Agents 如 Skills）
    # 避免污染全局 Registry
    dynamic_config: Any | None = None 
    
    def to_dict(self) -> dict[str, Any]:
        """序列化为字典（用于 checkpoint）。"""
        return {
            "agent_name": self.agent_name,
            "memory": [msg.model_dump() for msg in self.memory],
            "state": self.state,
            "instruction": self.instruction,
            "result": self.result,
            # dynamic_config usually not serializable easily if it's a Pydantic object, 
            # but for checkpointing we might need to dump it. 
            # For MVP, we skip or naive dump.
             "dynamic_config": self.dynamic_config.model_dump() if self.dynamic_config else None
        }
