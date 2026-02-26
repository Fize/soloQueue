"""AgentRunner - 执行单个 Agent 的一步操作。"""

from typing import Any, Optional, Callable

from langchain_core.messages import (
    AIMessage,
    ToolMessage
)

from datetime import datetime
from soloqueue.core.adapters.factory import ModelAdapterFactory
from soloqueue.core.logger import logger
from soloqueue.core.loaders.schema import AgentSchema
from soloqueue.orchestration.frame import TaskFrame
from soloqueue.orchestration.signals import ControlSignal, SignalType
from soloqueue.orchestration.tools import is_skill_tool
from soloqueue.core.memory.manager import MemoryManager
from soloqueue.core.context.token_counter import TokenCounter
from soloqueue.core.context.builder import ContextBuilder
from soloqueue.core.state import StateManager
from soloqueue.web.utils.colors import get_agent_color


class AgentRunner:
    """执行单个 Agent 的一步操作。"""
    
    config: AgentSchema
    tools: list[Any]
    tools_by_name: dict[str, Any]
    registry: Any
    memory: Optional[MemoryManager]
    llm: Any
    token_counter: TokenCounter
    context_builder: ContextBuilder

    def __init__(
        self, 
        config: AgentSchema, 
        tools: list[Any], 
        registry: Any, 
        memory: Optional[MemoryManager] = None,
        state_manager: Optional[StateManager] = None,
        session_id: Optional[str] = None
    ):
        self.session_id = session_id
        self.config = config
        self.tools = tools
        self.tools_by_name = {t.name: t for t in tools}
        self.registry = registry
        self.memory = memory
        
        # 创建 LLM 并绑定工具
        adapter = ModelAdapterFactory.create(config.model)
        self.llm = adapter.bind_tools(tools) if tools else adapter
        
        # Initialize Context Tools (Production)
        self.token_counter = TokenCounter(model=config.model)
        self.context_builder = ContextBuilder(self.token_counter)
        
        # State Management (Optional)
        self.state_manager = state_manager
        if state_manager:
            state_manager.register_agent(
                agent_id=config.name,
                group_id=config.group,
                capabilities=config.skills or []
            )
    
    def step(self, frame: TaskFrame, step_callback: Optional[Callable[[dict[str, Any]], None]] = None) -> ControlSignal:
        """
        执行一步，返回控制信号。
        
        Args:
            frame: 当前执行上下文
            
        Returns:
            ControlSignal 指示下一步操作
        """
        # 0. 计算Agent颜色用于UI输出
        agent_color = get_agent_color(self.config.name, self.config.color)
        agent_id = self.config.node_id
        agent_group = self.config.group or "default"
        agent_actor = self.config.node_id

        # 1. 构建消息（System Prompt + Frame Memory）
        system_content = self.config.system_prompt
        
        # 1.1 Auto-Inject Sub-Agents List
        if self.config.sub_agents and hasattr(self, 'registry') and self.registry:
            # Fetch sub-agent details from Registry
            sub_agent_info = []
            for sa_name in self.config.sub_agents:
                sa_config = self.registry.get_agent_by_name(sa_name, self.config.group)
                if sa_config:
                    sub_agent_info.append(f"- {sa_name}: {sa_config.description}")
                else:
                    sub_agent_info.append(f"- {sa_name}: (Description not found)")
            
            if sub_agent_info:
                injection = "\n\n## Available Sub-Agents\nYou have access to the following sub-agents. You can delegate tasks to them using the `delegate_to` tool.\n" + "\n".join(sub_agent_info)
                system_content = str(system_content or "") + injection

        # 1.2 Group Shared Context Injection (Production)
        if hasattr(self, 'registry') and self.registry and self.config.group:
             # Find the group config
             group_config = self.registry.groups.get(self.config.group)
             
             if group_config and group_config.shared_context:
                 context_text = group_config.shared_context
                 
                 # Length Warning (Token Budget Management)
                 if len(context_text) > 1000:
                     logger.warning(f"Group '{self.config.group}' shared_context is too long ({len(context_text)} chars). Context efficiency impacted. Consider moving to Memory/Artifacts.")
                 
                 # Append to System Prompt (Priority 0)
                 system_content = str(system_content or "") + f"\n\n## Group Shared Context ({self.config.group})\n{context_text}"

        # 1.3 Optimized Context Construction (Priority 0 & 1)
        history = frame.memory
        messages = self.context_builder.build_context(
            system_prompt=system_content or "",
            history=history
        )
        
        # Capture input for logging (Last meaningful message before LLM call)
        # Usually the last message in frame.memory
        input_msg_content = "Start"
        if frame.memory:
            last_msg = frame.memory[-1]
            if hasattr(last_msg, 'content'):
                input_msg_content = str(last_msg.content)
        
        logger.debug(f"AgentRunner.step: {self.config.name}, memory_len={len(frame.memory)}")
        
        # 2. 调用 LLM (Streaming)
        if step_callback:
            step_callback({
                "type": "agent_status",
                "agent_id": agent_id,
                "status": "starting",
                "message": f"Agent [{self.config.name}] starting...",
                "agent_color": agent_color,
                "timestamp": datetime.now().isoformat(),
                "group": agent_group,
                "from_actor": agent_actor,
            })
        
        full_response = None
        has_reasoning_started = False
        has_content_started = False
        reasoning_buffer = ""
        
        try:
            for chunk in self.llm.stream(messages):
                # Accumulate full response
                if full_response is None:
                    full_response = chunk
                else:
                    full_response += chunk
                
                # Handle Reasoning Content
                reasoning = chunk.additional_kwargs.get("reasoning_content", "")
                if reasoning:
                    if not has_reasoning_started:
                        has_reasoning_started = True

                    reasoning_buffer += reasoning
                    # Only show thinking bubble for explicit reasoning models
                    if self.config.reasoning and step_callback:
                        step_callback({
                            "type": "stream",
                            "agent_id": agent_id,
                            "content": reasoning,
                            "agent_color": agent_color,
                            "stream_type": "thinking",
                            "timestamp": datetime.now().isoformat(),
                            "group": agent_group,
                            "from_actor": agent_actor,
                        })
                    
                    if len(reasoning_buffer) > 50000:
                         raise ValueError("Reasoning limit (50k chars) exceeded. Terminating to prevent loop.")
                
                # Handle Actual Content
                if chunk.content:
                    if not has_content_started:
                        has_content_started = True
                    if step_callback:
                        step_callback({
                            "type": "stream",
                            "agent_id": agent_id,
                            "content": chunk.content,
                            "agent_color": agent_color,
                            "stream_type": "answer",
                            "timestamp": datetime.now().isoformat(),
                            "group": agent_group,
                            "from_actor": agent_actor,
                        })
            
            # Ensure full_response is an AIMessage
            if full_response is None:
                # Fallback for empty stream
                full_response = AIMessage(content="")
            
            # Manually ensure reasoning_content is preserved
            if reasoning_buffer:
                 if not hasattr(full_response, 'additional_kwargs') or full_response.additional_kwargs is None:
                     full_response.additional_kwargs = {}
                 full_response.additional_kwargs["reasoning_content"] = reasoning_buffer

            response = full_response
                
        except Exception as e:
            logger.error(f"LLM streaming failed: {e}")
            if self.memory and self.session_id:
                self.memory.save_error(self.session_id, f"LLM streaming failed: {e}", {"agent": self.config.name})
            return ControlSignal(type=SignalType.ERROR, error_msg=str(e))
        
        frame.memory.append(response)
        
        # LOGGING: Save Interaction
        if self.memory and self.session_id:
            # We log the raw interaction (Input -> Response)
            # Tools will be logged separately
            tool_calls_log = []
            if response.tool_calls:
                tool_calls_log = response.tool_calls
            
            self.memory.save_interaction(
                session_id=self.session_id,
                agent_name=self.config.name,
                input_msg=input_msg_content,
                output_msg=str(response.content),
                tools=tool_calls_log
            )
        
        # 3. 解析响应
        if response.tool_calls:
            # Clear streamed answer bubbles — narration text is redundant with tool_call events
            if step_callback:
                step_callback({
                    "type": "clear_agent_answer",
                    "agent_id": agent_id,
                    "group": agent_group,
                    "from_actor": agent_actor,
                })

            # 检查是否是 delegate_to 调用
            delegate_call = self._find_delegate_call(response.tool_calls)
            
            if delegate_call:
                # 1. Serializing delegation (Keep logic from before)
                if len(response.tool_calls) > 1:
                    logger.warning(f"Detected multiple tool calls ({len(response.tool_calls)}). Serializing delegation: {delegate_call['id']}")
                    target_tool_call = next(tc for tc in response.tool_calls if tc["id"] == delegate_call["id"])
                    
                    new_kwargs = {}
                    if hasattr(response, 'additional_kwargs') and response.additional_kwargs and "reasoning_content" in response.additional_kwargs:
                        new_kwargs["reasoning_content"] = response.additional_kwargs["reasoning_content"]
                    
                    serialized_response = AIMessage(
                        content=str(response.content),
                        tool_calls=[target_tool_call],
                        additional_kwargs=new_kwargs
                    )
                    frame.memory[-1] = serialized_response
                    
                return ControlSignal(
                    type=SignalType.DELEGATE,
                    target_agent=delegate_call["args"]["target"],
                    instruction=delegate_call["args"]["instruction"],
                    tool_call_id=delegate_call["id"]
                )
            
            # 普通工具调用
            tool_results = self._execute_tools(response.tool_calls, step_callback)
            # Check for Skill Signals and Filter Memory
            final_results = []
            skill_signal = None
            
            for res in tool_results:
                content_str = str(res.content)
                if content_str.startswith("__USE_SKILL__:"):
                    try:
                        _, payload = content_str.split(":", 1)
                        name, args = payload.split("|", 1)
                        skill_signal = ControlSignal(
                            type=SignalType.USE_SKILL,
                            skill_name=name.strip(),
                            skill_args=args.strip(),
                            tool_call_id=res.tool_call_id
                        )
                    except ValueError:
                         logger.error(f"Failed to parse Skill Signal: {content_str}")
                         final_results.append(res)
                else:
                    final_results.append(res)
            
            frame.memory.extend(final_results)
            
            if skill_signal:
                return skill_signal
            
            return ControlSignal(type=SignalType.CONTINUE)
        
        # 4. 最终回答
        return ControlSignal(
            type=SignalType.RETURN,
            result=str(response.content)
        )
    
    def _find_delegate_call(self, tool_calls: list) -> dict | None:
        """查找 delegate_to 工具调用。"""
        for call in tool_calls:
            if call["name"] == "delegate_to":
                return call
        return None
    
    def _execute_tools(self, tool_calls: list, step_callback: Optional[Callable[[dict[str, Any]], None]] = None) -> list[ToolMessage]:
        """执行工具，返回 ToolMessage 列表。"""
        results = []

        # Calculate agent color and ID for UI events
        agent_id = self.config.node_id
        agent_color = get_agent_color(self.config.name, self.config.color)
        agent_group = self.config.group or "default"
        agent_actor = self.config.node_id

        for call in tool_calls:
            # Terminal log for tool call
            logger.info(f"🔧 Tool Call: <cyan>{call['name']}</cyan>({call['args']})")

            # 判断 action_type 和 to_actor
            action_type = "normal"
            to_actor = None
            
            if call["name"] == "delegate_to":
                action_type = "delegate"
                to_actor = call["args"].get("target", "")
            elif is_skill_tool(call["name"]):
                # skill proxy 工具通过 is_skill_tool 函数判断
                action_type = "skill"
                to_actor = f"skill__{call['name']}"

            # Execute tool first
            tool = self.tools_by_name.get(call["name"])
            
            output = ""
            if not tool:
                output = f"Error: Tool '{call['name']}' not found."
            else:
                try:
                    output = tool.invoke(call["args"])
                except Exception as e:
                    output = f"Tool execution failed: {e}"
                    logger.warning(f"Tool {call['name']} failed: {e}")
                    if self.memory and self.session_id:
                        self.memory.save_error(self.session_id, f"Tool {call['name']} execution failed: {e}")
            
            # Terminal log for tool result
            result_preview = str(output)[:200]
            if len(str(output)) > 200:
                result_preview += "..."
            logger.info(f"📋 Tool Result: <cyan>{call['name']}</cyan> → {result_preview}")

            # 判断是否为 skill proxy
            is_skill_proxy = str(output).startswith("__USE_SKILL__")
            
            # 根据输出判断 action_type
            if is_skill_proxy:
                action_type = "skill"
                to_actor = f"skill__{call['name']}"

            if step_callback:
                # Send tool_call event
                step_callback({
                    "type": "tool_call",
                    "agent_id": agent_id,
                    "content": f"Calling tool '{call['name']}' with args: {call['args']}",
                    "tool_name": call["name"],
                    "tool_args": str(call["args"]),
                    "agent_color": agent_color,
                    "preview_snippet": f"Tool: {call['name']}",
                    "collapsible": False,
                    "collapsed_by_default": False,
                    "timestamp": datetime.now().isoformat(),
                    "group": agent_group,
                    "from_actor": agent_actor,
                    "to_actor": to_actor,
                    "action_type": action_type,
                    "parent_tool_call_id": call["id"],
                })

                # Send tool_result event
                preview = str(output)[:200]
                if len(str(output)) > 200:
                    preview += "..."
                
                # skill proxy 显示简短提示
                result_content = "[Skill invoked and queued]" if is_skill_proxy else str(output)

                step_callback({
                    "type": "tool_result",
                    "agent_id": agent_id,
                    "content": result_content,
                    "tool_name": call["name"],
                    "agent_color": agent_color,
                    "preview_snippet": preview,
                    "collapsible": False,
                    "collapsed_by_default": False,
                    "timestamp": datetime.now().isoformat(),
                    "group": agent_group,
                    "from_actor": agent_actor,
                    "to_actor": to_actor,
                    "action_type": action_type,
                })

            # LOGGING: Save Tool Output
            if self.memory and self.session_id:
                self.memory.save_tool_output(
                    session_id=self.session_id,
                    tool_name=call["name"],
                    tool_input=str(call["args"]),
                    tool_output=str(output)
                )

            # CONTEXT OFFLOADING (Production)
            final_output = str(output)
            if self.memory and len(final_output) > 2000:
                final_output = self._offload_large_output(final_output, call["name"])

            results.append(ToolMessage(
                tool_call_id=call["id"],
                content=final_output,
                name=call["name"]
            ))
        
        return results

    def _offload_large_output(self, content: str, tool_name: str) -> str:
        """
        Offload large tool output to L4 Artifact Store.
        
        Args:
            content: Raw output
            tool_name: Originating tool
            
        Returns:
            Reference string with summary
        """
        # 1. Generate Summary (Preview)
        if len(content) > 700:
            summary = f"{content[:500]}\n[... truncated {len(content) - 700} chars ...]\n{content[-200:]}"
        else:
            summary = content
            
        # 2. Save as Ephemeral Artifact
        art_id = self.memory.save_artifact(
            content=content,
            title=f"Tool Output Offload: {tool_name}",
            author=self.config.name,
            tags=["sys:ephemeral", f"tool:{tool_name}"],
            artifact_type="text"
        )
        
        size_kb = len(content) / 1024
        ref_msg = (
            f"[Output too large ({size_kb:.1f}KB). Saved as Artifact: {art_id}. "
            f"Preview:\\n---\\n{summary}\\n---\\n"
            f"Use read_artifact('{art_id}') to see full content.]"
        )
        
        logger.info(f"Offloaded large output from '{tool_name}' to artifact {art_id}")
        return ref_msg
    
    def run_queue_worker(self, poll_interval: int = 5):
        """
        Continuous queue worker loop.
        
        Polls the state manager for pending tasks and executes them.
        
        Args:
            poll_interval: Seconds between queue polls when empty
        """
        import time
        
        if not self.state_manager:
            raise ValueError("StateManager is required for queue worker mode")
        
        logger.info(f"[{self.config.name}] Starting queue worker (poll_interval={poll_interval}s)")
        
        while True:
            try:
                # Heartbeat
                self.state_manager.update_heartbeat(self.config.name)
                
                # Claim next task
                task = self.state_manager.claim_next_task(
                    agent_id=self.config.name,
                    group_id=self.config.group or "default",
                    capabilities=self.config.skills
                )
                
                if not task:
                    time.sleep(poll_interval)
                    continue
                
                logger.info(f"[{self.config.name}] Claimed task {task['task_id']}")
                
                # Mark busy
                self.state_manager.mark_agent_busy(
                    self.config.name,
                    task['task_id']
                )
                
                # Execute task
                result = self._execute_task_from_queue(task)
                
                # Update success
                self.state_manager.update_task_status(
                    task_id=task['task_id'],
                    status='complete',
                    result_artifact_id=result.get('artifact_id')
                )
                
                logger.info(f"[{self.config.name}] Task {task['task_id']} completed")
                
            except KeyboardInterrupt:
                logger.info(f"[{self.config.name}] Queue worker stopped by user")
                break
                
            except Exception as e:
                logger.error(f"[{self.config.name}] Task execution failed: {e}")
                
                if task:
                    self.state_manager.update_task_status(
                        task_id=task['task_id'],
                        status='failed',
                        error_msg=str(e)
                    )
            
            finally:
                # Mark idle
                if self.state_manager:
                    self.state_manager.mark_agent_idle(self.config.name)
    
    def _execute_task_from_queue(self, task: dict) -> dict:
        """
        Execute a task from the queue.
        
        Args:
            task: Task dict from state manager
        
        Returns:
            Result dict with status and optional artifact_id
        """
        from langchain_core.messages import HumanMessage
        from soloqueue.orchestration.frame import TaskFrame
        
        # Build frame from task
        frame = TaskFrame(
            agent_name=self.config.name,
            memory=[HumanMessage(content=task['instruction'])]
        )
        
        # Run standard step logic
        signal = self.step(frame)
        
        # Return result
        return {
            "status": signal.type.value,
            "artifact_id": None  # Could save output as artifact
        }
