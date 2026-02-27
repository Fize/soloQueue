from typing import List, Set, TYPE_CHECKING, Optional

from langchain_core.tools import BaseTool, tool
from pydantic import BaseModel, Field

from soloqueue.core.logger import logger
from soloqueue.core.primitives import get_all_primitives
from soloqueue.core.loaders.skill_loader import SkillLoader

if TYPE_CHECKING:
    from soloqueue.core.memory import MemoryManager, MemoryEntry

# Global set to track skill tool names
_skill_tool_names: Set[str] = set()

class DelegateInput(BaseModel):
    """Input for delegating a task to another agent."""
    target: str = Field(description="The name of the agent to delegate to (e.g., 'cto', 'developer')")
    instruction: str = Field(description="Detailed instruction of what the sub-agent needs to do.")

def create_delegate_tool(allowed_targets: List[str]) -> BaseTool:
    """
    Creates a 'delegate_to' tool with dynamic enum validation.
    This tool is NOT executed by Python; it's a signal for the Router.
    """
    # Create dynamic Enums or just use description to guide LLM? 
    # Proper Enum in Pydantic is safer.
    
    # Dynamic class creation for strict validation
    description = f"Delegate a task to one of: {', '.join(allowed_targets)}"
    
    @tool(args_schema=DelegateInput)
    def delegate_to(target: str, instruction: str) -> str:
        """Delegate a task to a subordinate agent."""
        # This function body might never be called if Router intercepts it first.
        # But if it is called, we return a special signal string.
        return f"__DELEGATE_TO__: {target} | {instruction}"
    
    delegate_to.description = description
    return delegate_to

class DelegateParallelInput(BaseModel):
    """Input for delegating tasks to multiple agents in parallel."""
    tasks: str = Field(
        description='JSON array of parallel delegation tasks. '
                    'Format: [{"target": "agent_name", "instruction": "task description"}, ...]. '
                    'All agents run concurrently and results are aggregated.'
    )

def create_delegate_parallel_tool(allowed_targets: List[str]) -> BaseTool:
    """
    Creates a 'delegate_parallel' tool for parallel multi-agent delegation.
    This tool is NOT executed by Python; it's a signal for the Orchestrator.
    """
    description = (
        f"Delegate tasks to MULTIPLE agents in parallel. "
        f"Available agents: {', '.join(allowed_targets)}. "
        f"Use this when you need results from multiple agents simultaneously."
    )
    
    @tool(args_schema=DelegateParallelInput)
    def delegate_parallel(tasks: str) -> str:
        """Delegate tasks to multiple subordinate agents in parallel. All agents run concurrently and results are aggregated."""
        return f"__DELEGATE_PARALLEL__: {tasks}"
    
    delegate_parallel.description = description
    return delegate_parallel

from soloqueue.core.loaders import AgentConfig


# ==================== Memory Tools ====================

class SearchMemoryInput(BaseModel):
    """Input for searching semantic memory."""
    query: str = Field(description="搜索查询，描述你想查找的内容")
    top_k: int = Field(default=5, description="返回结果数量")


class RememberInput(BaseModel):
    """Input for storing knowledge in semantic memory."""
    content: str = Field(description="要记忆的内容，应简洁明确")
    importance: str = Field(default="normal", description="重要程度: normal/important/critical")


def _format_search_results(entries: List["MemoryEntry"]) -> str:
    """将搜索结果格式化为人类可读字符串。"""
    if not entries:
        return "未找到相关记忆。你可以尝试用 remember 工具存储这个知识。"
    
    lines = [f"搜索结果 (找到 {len(entries)} 条相关记忆):\n"]
    for i, entry in enumerate(entries, 1):
        ts = entry.metadata.get("timestamp", "")
        score = entry.score or 0.0
        # 时间戳格式：ISO 8601
        lines.append(f"{i}. [分数: {score:.2f}] [{ts}]")
        lines.append(f"{entry.content}\n")
    
    return "\n".join(lines)


def _should_store(
    memory: "MemoryManager",
    content: str,
    agent_id: str,
    threshold: float = 0.95
) -> bool:
    """检查是否存在相似记忆，相似度超过阈值则跳过存储。"""
    existing = memory.search_knowledge(content, top_k=1, agent_id=agent_id)
    if existing and existing[0].score >= threshold:
        return False  # 已存在相似记忆，跳过
    return True


def create_memory_tools(
    memory: "MemoryManager",
    agent_id: str,
) -> List[BaseTool]:
    """
    创建 search_memory 和 remember 工具，通过闭包绑定 memory 和 agent_id。
    
    Args:
        memory: MemoryManager 实例
        agent_id: Agent 标识符（必需，不能为空）
    
    Returns:
        包含 search_memory 和 remember 工具的列表
    
    Raises:
        ValueError: 如果 agent_id 为空
    """
    # 验证 agent_id
    if not agent_id:
        raise ValueError("agent_id 不能为空")
    
    @tool(args_schema=SearchMemoryInput)
    def search_memory(query: str, top_k: int = 5) -> str:
        """搜索你的语义记忆库，查找与查询相关的历史知识、经验或信息。当你需要回忆之前的发现、用户偏好或学到的知识时使用。"""
        entries = memory.search_knowledge(query, top_k=top_k, agent_id=agent_id)
        return _format_search_results(entries)
    
    @tool(args_schema=RememberInput)
    def remember(content: str, importance: str = "normal") -> str:
        """将有价值的知识存入你的语义记忆库，供未来检索。适用于：用户偏好、重要发现、解决方案、经验教训等。不要存储临时信息或常见知识。"""
        # 去重检查
        if not _should_store(memory, content, agent_id):
            return "duplicate: 已存在相似记忆，无需重复存储"
        
        try:
            memory.add_knowledge(
                content,
                metadata={"importance": importance},
                agent_id=agent_id
            )
            return f"success: 记忆已存储，重要程度: {importance}"
        except Exception as e:
            logger.error(f"remember 失败: {e}")
            return "error: 存储失败，embedding 服务暂时不可用"
    
    return [search_memory, remember]


def resolve_tools_for_agent(
    config: AgentConfig,
    memory: Optional["MemoryManager"] = None,
    agent_id: Optional[str] = None
) -> List[BaseTool]:
    """
    Combine built-in primitives and delegation tools for an agent.
    Primitives are auto-included for ALL agents.
    
    Args:
        config: Agent configuration
        memory: Optional MemoryManager for memory tools
        agent_id: Optional agent identifier for memory isolation
    
    Returns:
        List of tools for the agent
    """
    # 0. Start with ALL Primitives (Built-in skills)
    # The user specification states that all agents have these by default.
    final_tools = get_all_primitives()
    
    # Track names to avoid duplicates if user still lists them in YAML
    existing_tool_names = {t.name for t in final_tools}
    
    # 1. Add Configured Skills
    skill_loader = SkillLoader()
    
    # Dynamic Proxy Tool Factory
    def create_skill_proxy(skill_name: str, description: str):
        """Creates a tool that just signals the desire to use a skill."""
        
        class SkillInput(BaseModel):
            arguments: str = Field(description="Arguments for the skill (e.g. CLI flags or natural language input)")
            
        @tool(args_schema=SkillInput)
        def proxy_tool(arguments: str) -> str:
            """Executes the skill."""
            # Signal string for Runner to intercept
            return f"__USE_SKILL__: {skill_name} | {arguments}"
            
        proxy_tool.name = skill_name
        proxy_tool.description = description
        _skill_tool_names.add(skill_name)  # Track skill tool name
        return proxy_tool

    for name in config.tools:
        if name in existing_tool_names:
            # Already added as a primitive, skip to avoid duplication
            continue
        else:
            # Try loading as a Skill
            try:
                skill_schema = skill_loader.load(name)
                # Success! active the skill as a Tool
                proxy = create_skill_proxy(skill_schema.name, skill_schema.description)
                final_tools.append(proxy)
                existing_tool_names.add(skill_schema.name)
                logger.debug(f"Attached Skill Proxy: {skill_schema.name} for {config.node_id}")
            except Exception as e:
                # It might be an invalid tool name, or a primitive not in get_all_primitives?
                logger.warning(f"Skill '{name}' not found in Skill Registry. Error: {e}")
                
    # 2. Add Delegation Tool if applicable (Leader only)
    if config.is_leader:
        # Leader can delegate.
        # If sub_agents are defined, we restrict to those (Whitelist).
        # If empty but is_leader is True, we might allow ANY_AGENT (Wildcard) 
        # or just fail. For now, strict whitelist if list is present.
        
        targets = config.sub_agents
        if not targets:
            # Fallback to ANY_AGENT if no sub-agents defined but is_leader=True
            # This maintains backward compatibility or "Super Leader" mode
            targets = ["ANY_AGENT"]
            
        final_tools.append(create_delegate_tool(allowed_targets=targets))
        final_tools.append(create_delegate_parallel_tool(allowed_targets=targets))
    
    # 3. Add Memory Tools if semantic memory is available
    if memory is not None and agent_id:
        try:
            memory_tools = create_memory_tools(memory, agent_id)
            final_tools.extend(memory_tools)
            logger.debug(f"Attached Memory Tools for {config.node_id} (agent_id={agent_id})")
        except ValueError as e:
            logger.warning(f"Failed to create memory tools: {e}")

    # 4. Add Artifact Tools if memory (with ArtifactStore) is available
    if memory is not None:
        try:
            from soloqueue.core.tools.artifact_tools import create_artifact_tools
            artifact_tools = create_artifact_tools(memory, agent_id=agent_id)
            for at in artifact_tools:
                if at.name not in existing_tool_names:
                    final_tools.append(at)
                    existing_tool_names.add(at.name)
            logger.debug(f"Attached Artifact Tools for {config.node_id}")
        except Exception as e:
            logger.warning(f"Failed to create artifact tools: {e}")

    return final_tools


def is_skill_tool(tool_name: str) -> bool:
    """Check if a tool name is a skill proxy."""
    return tool_name in _skill_tool_names
