import asyncio
import threading
import uuid
import time
from datetime import datetime
from typing import Callable, Optional, Dict, Any

from langchain_core.messages import HumanMessage, ToolMessage, AIMessage

from soloqueue.core.logger import logger
from soloqueue.core.registry import Registry
from soloqueue.orchestration.frame import TaskFrame
from soloqueue.orchestration.runner import AgentRunner
from soloqueue.orchestration.signals import ControlSignal, SignalType, ParallelDelegateTarget
from soloqueue.orchestration.tools import resolve_tools_for_agent
from soloqueue.core.memory.manager import MemoryManager
from soloqueue.core.memory.session_manager import SessionManager
from soloqueue.core.memory.session_logger import (
    SessionLogger,
    ConversationTurn,
    ToolCallRecord,
    SkillCallRecord,
    AIResponse,
    TokenUsage
)


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
        self._memory_manager_lock = threading.Lock()
        self.session_logger = SessionLogger(workspace_root)
        self.session_manager = SessionManager(self.session_logger)
        
    def _get_memory_manager(self, group: str) -> MemoryManager:
        """线程安全地获取或创建 MemoryManager。"""
        if group in self.memory_managers:
            return self.memory_managers[group]
        with self._memory_manager_lock:
            if group not in self.memory_managers:
                self.memory_managers[group] = MemoryManager(self.workspace_root, group)
            return self.memory_managers[group]

    async def run(
        self,
        initial_agent: str,
        user_message: str,
        step_callback: Optional[Callable[[Dict[str, Any]], None]] = None,
        session_id: Optional[str] = None,
        user_id: Optional[str] = None,
    ) -> str:
        """
        主事件循环（异步）。

        Args:
            initial_agent: 入口 Agent 名称
            user_message: 用户输入
            step_callback: 可选的回调函数，用于流式传输执行步骤
            session_id: 可选的会话ID（已废弃，优先使用 user_id 自动解析）
            user_id: 用户标识，用于 SessionManager 生成/解析 session_id

        Returns:
            最终结果字符串
        """
        start_time = time.time()

        # Determine initial group for session purposes
        initial_config = self.registry.get_agent_by_name(initial_agent)
        primary_group = initial_config.group if initial_config else "default"

        # Get memory manager for primary group
        primary_memory = self._get_memory_manager(primary_group)

        # --- Session 管理 ---
        if user_id:
            # 处理 /new 命令
            if user_message.strip() == "/new":
                old_session_id = self.session_manager.get_previous_session_id(user_id)
                session_info = self.session_manager.force_new_session(user_id)
                session_id = session_info.session_id

                # 归档旧 session
                if old_session_id:
                    self.session_manager.archive_session(
                        old_session_id, user_id, primary_memory,
                    )

                # 通知客户端新 session
                if step_callback:
                    step_callback({
                        "type": "session_new",
                        "session_id": session_id,
                        "message": "New session created.",
                    })

                return "New session created."

            # 正常消息：通过 SessionManager 解析 session_id
            session_info = self.session_manager.resolve_session(user_id)
            session_id = session_info.session_id

            # 如果是跨天新 session，归档旧 session
            if session_info.is_new:
                old_session_id = self.session_manager.get_previous_session_id(user_id)
                if old_session_id:
                    self.session_manager.archive_session(
                        old_session_id, user_id, primary_memory,
                    )
        else:
            # 兼容旧逻辑：无 user_id 时使用传入的 session_id 或随机生成
            if not session_id:
                session_id = str(uuid.uuid4())

        logger.bind(session_id=session_id).info(
            f"Orchestrator started: session={session_id}, "
            f"agent={initial_agent}, group={primary_group}, user_id={user_id}"
        )

        resolved_agent_name = initial_config.node_id if initial_config else initial_agent

        # 初始化对话日志记录
        turn_records = self.session_logger.get_turns(session_id)
        current_turn = len(turn_records) + 1

        conversation_turn = ConversationTurn(
            session_id=session_id,
            turn=current_turn,
            timestamp=datetime.now().isoformat(),
            group=primary_group,
            entry_agent=resolved_agent_name,
            user_id=user_id or "",
            user_message=user_message,
        )
        
        # 1. 初始化根 Frame
        root_frame = TaskFrame(
            agent_name=resolved_agent_name,
            instruction=user_message
        )

        # 从 SessionLogger 加载历史消息（Session恢复）
        # 使用 limit 做预截断，避免全量加载浪费内存
        history = self.session_logger.get_history(session_id, limit=20)
        for msg in history:
            root_frame.memory.append(msg)
        if history:
            logger.debug(f"Loaded {len(history)} history messages for session={session_id}")
        
        # 添加当前用户消息
        root_frame.memory.append(HumanMessage(content=user_message))
        self.stack.append(root_frame)
        
        # 记录委派链
        delegation_chain = [resolved_agent_name]
        
        # 2. 事件循环
        iteration = 0
        max_iterations = 100  # 防止无限循环
        
        try:
            while self.stack and iteration < max_iterations:
                iteration += 1
                frame = self.stack[-1]
                
                logger.debug(f"Loop {iteration}: agent={frame.agent_name}, stack_depth={len(self.stack)}")
                
                # 执行 Agent（同步阻塞，放入线程池避免阻塞 event loop）
                loop = asyncio.get_event_loop()
                signal = await loop.run_in_executor(
                    None,
                    lambda: self._execute_frame(frame, session_id=session_id, step_callback=step_callback)
                )
                
                # 处理信号
                match signal.type:
                    case SignalType.CONTINUE:
                        continue
                        
                    case SignalType.DELEGATE:
                        # 解析目标 Agent
                        target_name = signal.target_agent
                        
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

                        # 记录工具调用
                        tool_call_record = ToolCallRecord(
                            agent=frame.agent_name,
                            tool_name="delegate_to",
                            tool_args={"agent": target_name, "instruction": signal.instruction or ""},
                            result={"status": "started"},
                            timestamp=datetime.now().isoformat(),
                            duration_ms=0
                        )
                        conversation_turn.tool_calls.append(tool_call_record)
                        
                        # 记录到委派链
                        if target_name not in delegation_chain:
                            delegation_chain.append(target_name)

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
                    
                    case SignalType.DELEGATE_PARALLEL:
                        # 并行委派：同时向多个子 Agent 发送任务
                        if not signal.parallel_delegates:
                            self._handle_delegation_error(frame, signal, "No parallel delegate targets specified.")
                            continue
                        
                        # 权限检查 & 解析所有目标
                        source_config = self.registry.get_agent_by_node_id(frame.agent_name)
                        resolved_targets = []
                        for pd_target in signal.parallel_delegates:
                            target_config = self._resolve_agent(pd_target.target_agent, frame.agent_name)
                            if not target_config:
                                self._handle_delegation_error(frame, signal, f"Agent '{pd_target.target_agent}' not found.")
                                break
                            if not self._check_permission(source_config, target_config):
                                reason = f"Permission Denied: {source_config.node_id} -> {target_config.node_id}"
                                self._handle_delegation_error(frame, signal, reason)
                                break
                            resolved_targets.append((target_config, pd_target))
                        else:
                            # 所有目标解析成功，执行并行委派
                            # 记录工具调用
                            tool_call_record = ToolCallRecord(
                                agent=frame.agent_name,
                                tool_name="delegate_parallel",
                                tool_args={
                                    "targets": [
                                        {"agent": tc.node_id, "instruction": pd.instruction}
                                        for tc, pd in resolved_targets
                                    ]
                                },
                                result={"status": "started", "count": len(resolved_targets)},
                                timestamp=datetime.now().isoformat(),
                                duration_ms=0
                            )
                            conversation_turn.tool_calls.append(tool_call_record)
                            
                            # 记录到委派链
                            for tc, _ in resolved_targets:
                                if tc.node_id not in delegation_chain:
                                    delegation_chain.append(tc.node_id)
                            
                            # 通知前端并行委派开始
                            if step_callback:
                                step_callback({
                                    "type": "parallel_started",
                                    "agent_id": frame.agent_name,
                                    "targets": [tc.node_id for tc, _ in resolved_targets],
                                    "group": source_config.group if source_config else "default",
                                    "from_actor": frame.agent_name,
                                    "timestamp": datetime.now().isoformat(),
                                })
                            
                            # 执行并行委派
                            results = await self._run_parallel_delegates(
                                resolved_targets, session_id, step_callback
                            )
                            
                            # 将所有结果注入 Leader frame 的 memory
                            for tc, pd, result_str in results:
                                frame.memory.append(
                                    ToolMessage(
                                        tool_call_id=signal.tool_call_id,
                                        content=f"[{tc.node_id}] Result:\n{result_str}",
                                        name="delegate_parallel"
                                    )
                                )
                            
                            # 通知前端并行委派完成
                            if step_callback:
                                step_callback({
                                    "type": "parallel_completed",
                                    "agent_id": frame.agent_name,
                                    "targets": [tc.node_id for tc, _, _ in results],
                                    "group": source_config.group if source_config else "default",
                                    "from_actor": frame.agent_name,
                                    "timestamp": datetime.now().isoformat(),
                                })
                        
                        continue
                        
                    case SignalType.USE_SKILL:
                         # MVP Skill Logic
                         logger.info(f"Skill used: {signal.skill_name}")
                         
                         skill_start = time.time()
                         skill_result = None
                         
                         try:
                             from soloqueue.core.loaders.skill_loader import SkillLoader
                             from soloqueue.core.skills.processor import SkillPreprocessor
                             from soloqueue.core.loaders.schema import AgentSchema
                             
                             loader = SkillLoader()
                             skill = loader.load(signal.skill_name)
                             if not skill: 
                                 raise ValueError(f"Skill {signal.skill_name} not found")
                             
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
                             
                             # 记录Skill调用
                             skill_result = "started"
                             
                         except Exception as e:
                             logger.error(f"Skill Error: {e}")
                             frame.memory.append(HumanMessage(content=f"Skill Error: {e}"))
                             skill_result = f"error: {e}"
                         
                         # 记录Skill调用日志
                         skill_call_record = SkillCallRecord(
                             skill_name=signal.skill_name,
                             skill_args=signal.skill_args or "",
                             agent=frame.agent_name,
                             result=skill_result or "",
                             timestamp=datetime.now().isoformat(),
                             duration_ms=int((time.time() - skill_start) * 1000)
                         )
                         conversation_turn.skill_calls.append(skill_call_record)
                         continue
    
                    case SignalType.RETURN:
                        completed_frame = self.stack.pop()
                        logger.info(f"Returned from {completed_frame.agent_name}")
                        
                        # 获取 completed_frame 的 group 信息
                        completed_config = self.registry.get_agent_by_node_id(completed_frame.agent_name)
                        completed_group = completed_config.group if completed_config else "default"
                        
                        # 记录到委派链
                        if completed_frame.agent_name not in delegation_chain:
                            delegation_chain.append(completed_frame.agent_name)
                        
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
                            # 根Frame完成，保存日志
                            conversation_turn.ai_response = AIResponse(content=signal.result or "No result")
                            conversation_turn.delegation_chain = delegation_chain
                            conversation_turn.duration_ms = int((time.time() - start_time) * 1000)
                            conversation_turn.status = "completed"
                            self.session_logger.save_turn(conversation_turn)
                            return signal.result or "No result"
                            
                    case SignalType.ERROR:
                        logger.error(f"Error in {frame.agent_name}: {signal.error_msg}")
                        frame.memory.append(HumanMessage(content=f"Error: {signal.error_msg}"))
                        continue
            
            if iteration >= max_iterations:
                logger.warning(f"Max iterations ({max_iterations}) reached")
                # 保存错误日志
                conversation_turn.status = "timeout"
                conversation_turn.duration_ms = int((time.time() - start_time) * 1000)
                self.session_logger.save_turn(conversation_turn)
                return "Error: Max iterations reached"
            
            return "No result"

        except Exception as e:
            logger.bind(session_id=session_id).critical(f"Orchestrator Crashed: {e}", exc_info=True)
            # 保存错误日志
            conversation_turn.status = "error"
            conversation_turn.duration_ms = int((time.time() - start_time) * 1000)
            conversation_turn.ai_response = AIResponse(content=f"System Error: {e}")
            self.session_logger.save_turn(conversation_turn)
            return f"System Error: {e}"
            
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
        
        # Resolve tools with memory support (agent_id = node_id)
        agent_id = agent_config.node_id
        tools = resolve_tools_for_agent(agent_config, memory=memory, agent_id=agent_id)
        
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
        tool_call_id = signal.tool_call_id if signal.tool_call_id else None
        tool_name = "delegate_parallel" if signal.type == SignalType.DELEGATE_PARALLEL else "delegate_to"
        if tool_call_id:
            frame.memory.append(ToolMessage(tool_call_id=tool_call_id, content=f"Error: {reason}", name=tool_name))
        else:
            frame.memory.append(HumanMessage(content=f"Error: {reason}"))

    async def _run_parallel_delegates(
        self,
        resolved_targets: list[tuple],
        session_id: Optional[str],
        step_callback: Optional[Callable[[Dict[str, Any]], None]]
    ) -> list[tuple]:
        """
        并行执行多个子 Agent，返回 (config, pd_target, result_str) 三元组列表。
        
        使用 asyncio.gather + run_in_executor 将同步的子 Agent 循环放到线程池并发执行。
        采用 Retry + Best-Effort 策略：失败自动重试 1 次，仍失败则以错误信息替代结果。
        """
        loop = asyncio.get_event_loop()
        
        async def _run_one(target_config, pd_target: ParallelDelegateTarget) -> tuple:
            """运行单个子 Agent，返回 (config, pd_target, result_str)。"""
            # 使用局部函数固定参数绑定，避免 lambda late binding
            def _invoke():
                return self._run_sub_agent(target_config, pd_target.instruction, session_id, step_callback)
            
            try:
                result = await loop.run_in_executor(None, _invoke)
                return (target_config, pd_target, result)
            except Exception as e:
                # 第一次失败，重试 1 次
                logger.warning(f"Parallel sub-agent {target_config.node_id} failed, retrying: {e}")
                try:
                    result = await loop.run_in_executor(None, _invoke)
                    return (target_config, pd_target, result)
                except Exception as e2:
                    # 重试仍失败，返回错误信息
                    logger.error(f"Parallel sub-agent {target_config.node_id} failed after retry: {e2}")
                    return (target_config, pd_target, f"Error: Agent {target_config.node_id} failed after retry: {e2}")
        
        logger.info(f"Starting parallel delegation with {len(resolved_targets)} targets")
        tasks = [_run_one(tc, pd) for tc, pd in resolved_targets]
        results = await asyncio.gather(*tasks, return_exceptions=False)
        error_count = sum(1 for _, _, r in results if r.startswith("Error:"))
        logger.info(f"Parallel delegation completed: {len(results)} results, {error_count} errors")
        return list(results)
    
    def _run_sub_agent(
        self,
        agent_config,
        instruction: str,
        session_id: Optional[str],
        step_callback: Optional[Callable[[Dict[str, Any]], None]]
    ) -> str:
        """
        同步执行子 Agent 的完整循环（step -> CONTINUE -> step -> ... -> RETURN）。
        
        此方法在线程池中执行，每个子 Agent 拥有独立的 TaskFrame 和 AgentRunner，
        不共享任何可变状态，线程安全。
        
        Returns:
            子 Agent 最终返回的结果字符串
        """
        group = agent_config.group or "default"
        memory = self._get_memory_manager(group)
        agent_id = agent_config.node_id
        tools = resolve_tools_for_agent(agent_config, memory=memory, agent_id=agent_id)
        
        runner = AgentRunner(agent_config, tools, registry=self.registry, memory=memory, session_id=session_id)
        
        child_frame = TaskFrame(
            agent_name=agent_config.node_id,
            instruction=instruction,
        )
        child_frame.memory.append(HumanMessage(content=instruction))
        
        max_sub_iterations = 50
        for i in range(max_sub_iterations):
            signal = runner.step(child_frame, step_callback=step_callback)
            
            match signal.type:
                case SignalType.CONTINUE:
                    continue
                case SignalType.RETURN:
                    return signal.result or "No result"
                case SignalType.ERROR:
                    raise RuntimeError(f"Sub-agent {agent_config.node_id} error: {signal.error_msg}")
                case SignalType.DELEGATE | SignalType.DELEGATE_PARALLEL:
                    # 子 Agent 不支持再委派（第一版不支持嵌套并行）
                    raise RuntimeError(
                        f"Sub-agent {agent_config.node_id} attempted delegation, "
                        f"which is not supported in parallel execution."
                    )
                case _:
                    raise RuntimeError(f"Unexpected signal from sub-agent: {signal.type}")
        
        raise RuntimeError(f"Sub-agent {agent_config.node_id} exceeded max iterations ({max_sub_iterations})")

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

    def cleanup(self) -> None:
        """
        显式清理内存资源。

        应在 session 结束（如 WebSocket 断开）时调用，
        释放 stack 和 memory_managers 的引用以帮助 GC。
        """
        self.stack.clear()
        self.memory_managers.clear()
        logger.debug("Orchestrator cleanup completed")
