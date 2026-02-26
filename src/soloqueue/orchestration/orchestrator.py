import uuid
from datetime import datetime
from typing import Callable, Optional, Dict, Any

from langchain_core.messages import HumanMessage, ToolMessage

from soloqueue.core.logger import logger
from soloqueue.core.registry import Registry
from soloqueue.orchestration.frame import TaskFrame
from soloqueue.orchestration.runner import AgentRunner
from soloqueue.orchestration.signals import ControlSignal, SignalType
from soloqueue.orchestration.tools import resolve_tools_for_agent
from soloqueue.core.memory.manager import MemoryManager


class Orchestrator:
    """
    核心编排引擎。
    
    管理 TaskFrame 栈，驱动事件循环。
    支持 Agent 委派和结果返回。
    """
    
    def __init__(self, registry: Registry, workspace_root: str = "."):
        self.registry = registry
        self.workspace_root = workspace_root
        self.stack: list[TaskFrame] = []
        self.memory_managers: dict[str, MemoryManager] = {}
        
    def _get_memory_manager(self, group: str) -> MemoryManager:
        """Get or create MemoryManager for a group."""
        if group not in self.memory_managers:
            self.memory_managers[group] = MemoryManager(self.workspace_root, group)
        return self.memory_managers[group]

    def run(self, initial_agent: str, user_message: str, step_callback: Optional[Callable[[Dict[str, Any]], None]] = None) -> str:
        """
        主事件循环。
        
        Args:
            initial_agent: 入口 Agent 名称
            user_message: 用户输入
            step_callback: 可选的回调函数，用于流式传输执行步骤
            
        Returns:
            最终结果字符串
        """
        # Determine initial group for session purposes
        initial_config = self.registry.get_agent_by_name(initial_agent)
        # Assuming initial agent exists. If not, it will fail in loop anyway, but safe to default.
        primary_group = initial_config.group if initial_config else "default"
        
        session_id = str(uuid.uuid4())
        logger.info(f"Orchestrator started: session={session_id}, agent={initial_agent}, group={primary_group}")

        
        # Start session logging for primary group
        primary_memory = self._get_memory_manager(primary_group)
        primary_memory.start_session(session_id, initial_agent)
        
        resolved_agent_name = initial_config.node_id if initial_config else initial_agent
        
        # 1. 初始化根 Frame
        root_frame = TaskFrame(
            agent_name=resolved_agent_name,
            instruction=user_message
        )
        root_frame.memory.append(HumanMessage(content=user_message))
        self.stack.append(root_frame)
        
        # 2. 事件循环
        iteration = 0
        max_iterations = 100  # 防止无限循环
        
        try:
            while self.stack and iteration < max_iterations:
                iteration += 1
                frame = self.stack[-1]
                
                logger.debug(f"Loop {iteration}: agent={frame.agent_name}, stack_depth={len(self.stack)}")
                
                # 执行 Agent
                signal = self._execute_frame(frame, session_id=session_id, step_callback=step_callback)
                
                # 处理信号
                match signal.type:
                    case SignalType.CONTINUE:
                        continue
                        
                    case SignalType.DELEGATE:
                        # 解析目标 Agent
                        target_name = signal.target_agent
                        # (omitted simple resolution logic for brevity, assuming full names mostly or relying on registry)
                        # We should restore the robust resolution logic if possible, or trust basic registry.
                        # For now, let's keep it simple: 
                        # We rely on Registry to resolve if needed, but here we just use the name from signal.
                        # Wait, the previous logic had Permission Check and Resolution. I should probably keep it.
                        # I will add back the essential parts.
                        
                        target_config = self._resolve_agent(target_name, frame.agent_name)
                        if not target_config:
                             # Error handling
                             self._handle_delegation_error(frame, signal, f"Agent '{target_name}' not found.")
                             continue

                        target_name = target_config.node_id
                        
                        # Security Check
                        source_config = self.registry.get_agent_by_node_id(frame.agent_name)
                        if not self._check_permission(source_config, target_config):
                            reason = f"Permission Denied: {source_config.node_id} -> {target_config.node_id}"
                            self._handle_delegation_error(frame, signal, reason)
                            continue

                        # Create Child Frame
                        child_frame = TaskFrame(
                            agent_name=target_name,
                            instruction=signal.instruction or "",
                            parent_tool_call_id=signal.tool_call_id
                        )
                        child_frame.memory.append(
                            HumanMessage(content=signal.instruction or "")
                        )
                        self.stack.append(child_frame)
                        logger.info(f"Delegated: {frame.agent_name} -> {target_name}")
                        continue
                        
                    case SignalType.USE_SKILL:
                         # MVP Skill Logic (Referencing original)
                         # Simple implementation for now to fix syntax
                         logger.info(f"Skill used: {signal.skill_name}")
                         # In MVP, this was handled inside _execute_frame logic? 
                         # No, it was a signal type.
                         # I need to restore the detailed skill logic later or assume standard execution.
                         # For now, let's just log and continue to avoid crashing.
                         # Wait, if I don't handle it, it loops?
                         # The original code handled it. I should probably restore it fully.
                         # For the sake of this task "Memory Persistence", I can simplify if skills aren't the focus.
                         # But I should not break existing features.
                         # I will include the import and logic.
                         self._handle_skill(signal, frame)
                         continue
    
                    case SignalType.RETURN:
                        completed_frame = self.stack.pop()
                        logger.info(f"Returned from {completed_frame.agent_name}")
                        
                        # 获取 completed_frame 的 group 信息
                        completed_config = self.registry.get_agent_by_node_id(completed_frame.agent_name)
                        completed_group = completed_config.group if completed_config else "default"
                        
                        if step_callback:
                            step_callback({
                                "type": "agent_status",
                                "agent_id": completed_frame.agent_name,
                                "status": "completed",
                                "message": f"Agent {completed_frame.agent_name} completed",
                                "group": completed_group,
                                "from_actor": completed_frame.agent_name,
                            })
                        
                        if self.stack:
                            parent = self.stack[-1]
                            if completed_frame.parent_tool_call_id:
                                # 发送 action_return 事件，明确表示子 agent 返回结果给父 agent
                                if step_callback:
                                    action_type = "skill" if "skill__" in completed_frame.agent_name else "delegate"
                                    step_callback({
                                        "type": "action_return",
                                        "action_type": action_type,
                                        "from_actor": completed_frame.agent_name,
                                        "to_actor": parent.agent_name,
                                        "group": completed_group,
                                        "parent_tool_call_id": completed_frame.parent_tool_call_id,
                                        "content": signal.result,
                                        "timestamp": datetime.now().isoformat()
                                })
                                parent.memory.append(
                                    ToolMessage(
                                        tool_call_id=completed_frame.parent_tool_call_id,
                                        content=f"Result:\n{signal.result}",
                                        name="skill"
                                    )
                                )
                            else:
                                parent.memory.append(
                                    HumanMessage(content=f"Result:\n{signal.result}")
                                )
                        else:
                            return signal.result or "No result"
                            
                    case SignalType.ERROR:
                        logger.error(f"Error in {frame.agent_name}: {signal.error_msg}")
                        frame.memory.append(HumanMessage(content=f"Error: {signal.error_msg}"))
                        continue
            
            if iteration >= max_iterations:
                logger.warning(f"Max iterations ({max_iterations}) reached")
                return "Error: Max iterations reached"
            
            return "No result"

        except Exception as e:
            logger.critical(f"Orchestrator Crashed: {e}", exc_info=True)
            if 'primary_memory' in locals() and 'session_id' in locals():
                primary_memory.save_error(session_id, f"Orchestrator Crashed: {e}")
            return f"System Error: {e}"
        finally:
            if 'primary_memory' in locals() and 'session_id' in locals():
                primary_memory.end_session(session_id)
            
    def _execute_frame(self, frame: TaskFrame, session_id: Optional[str] = None, step_callback: Optional[Callable[[Dict[str, Any]], None]] = None) -> ControlSignal:
        """执行单个 Frame，返回控制信号。"""
        if frame.dynamic_config:
            agent_config = frame.dynamic_config
        else:
            agent_config = self.registry.get_agent_by_node_id(frame.agent_name)
        
        if not agent_config:
            return ControlSignal(
                type=SignalType.ERROR,
                error_msg=f"Agent '{frame.agent_name}' not found"
            )
        
        # Get Memory Manager
        group = agent_config.group or "default"
        memory = self._get_memory_manager(group)
        
        tools = resolve_tools_for_agent(agent_config)
        
        # Check permissions for memory? No, passed implicitly.
        
        runner = AgentRunner(agent_config, tools, registry=self.registry, memory=memory, session_id=session_id)
        return runner.step(frame, step_callback=step_callback)

    # --- Optimizing: Restored Helper Methods for Cleanliness ---
    
    def _resolve_agent(self, name: str, current_node_id: str):
        current = self.registry.get_agent_by_node_id(current_node_id)
        current_group = current.group if current else None
        
        # Try exact node_id
        if self.registry.get_agent_by_node_id(name):
            return self.registry.get_agent_by_node_id(name)
            
        # Try name resolution
        return self.registry.get_agent_by_name(name, current_group)

    def _check_permission(self, source, target):
        if not source or not target: return False
        if source.group == target.group: return True
        # Cross-group: Leader only
        return source.is_leader and target.is_leader

    def _handle_delegation_error(self, frame, signal, reason):
        logger.warning(reason)
        if signal.tool_call_id:
            frame.memory.append(ToolMessage(tool_call_id=signal.tool_call_id, content=f"Error: {reason}", name="delegate_to"))
        else:
            frame.memory.append(HumanMessage(content=f"Error: {reason}"))

    def _handle_skill(self, signal, frame):
        # Lazy imports to implement skill logic
        from soloqueue.core.loaders.skill_loader import SkillLoader
        from soloqueue.core.skills.processor import SkillPreprocessor
        from soloqueue.core.loaders.schema import AgentSchema
        
        try:
             loader = SkillLoader()
             skill = loader.load(signal.skill_name)
             if not skill: raise ValueError(f"Skill {signal.skill_name} not found")
             
             processor = SkillPreprocessor()
             processed_content = processor.process(skill.content, signal.skill_args or "", skill.path)
             
             current_config = self.registry.get_agent_by_node_id(frame.agent_name)
             
             dynamic_agent = AgentSchema(
                 name=f"skill__{signal.skill_name}",
                 description=skill.description,
                 model=current_config.model if current_config else None,
                 tools=skill.allowed_tools,
                 group=current_config.group if current_config else None,
                 system_prompt=processed_content
             )
             
             child_frame = TaskFrame(
                 agent_name=dynamic_agent.node_id,
                 instruction=signal.skill_args or "",
                 parent_tool_call_id=signal.tool_call_id,
                 dynamic_config=dynamic_agent
             )
             if signal.skill_args:
                 child_frame.memory.append(HumanMessage(content=signal.skill_args))
                 
             self.stack.append(child_frame)
             
        except Exception as e:
            logger.error(f"Skill Error: {e}")
            frame.memory.append(HumanMessage(content=f"Skill Error: {e}"))
