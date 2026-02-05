from typing import List, Dict, Any

from langchain_core.messages import SystemMessage, HumanMessage, ToolMessage
from soloqueue.core.logger import logger
from langchain_core.tools import BaseTool

from soloqueue.core.loaders import AgentSchema
from soloqueue.core.llm import LLMFactory
from soloqueue.orchestration.state import AgentState

def create_agent_runner(config: AgentSchema, tools: List[BaseTool]):
    """
    Factory that returns the executable node function for a specific agent.
    """
    # 1. Initialize LLM with Tools
    # We bind tools here so the LLM knows what it can do.
    llm = LLMFactory.get_llm(config.model).bind_tools(tools)
    
    async def agent_node(state: AgentState) -> Dict[str, Any]:
        messages = state["messages"]
        input_messages = messages
        
        # Check for Delegation Entry
        # If the last message is a ToolMessage from 'delegate_to', we scope the history.
        if messages and isinstance(messages[-1], ToolMessage) and messages[-1].name == "delegate_to":
            last_msg = messages[-1]
            content = str(last_msg.content)
            
            # Extract instruction
            if content.startswith("Instruction: "):
                instruction = content.replace("Instruction: ", "", 1)
                
                # REWRITE HISTORY
                # We present this as a fresh Human request to the Sub-Agent
                logger.info(f"Scoping history for {config.name}: {instruction[:50]}...")
                input_messages = [HumanMessage(content=instruction)]
        
        # 2. Inject System Prompt
        # Dynamically inject available tools to ground the model
        tool_names = [t.name for t in tools]
        base_prompt = config.system_prompt or "You are a helpful assistant."
        
        system_instruction = f"""{base_prompt}

## TOOL USE GUIDELINES
- You have access to these tools ONLY: {tool_names}
- You do NOT have access to any other tools.
- If you do not see 'delegate_to' in the list above, you CANNOT delegate. You must do the work yourself.
- Focus on your specific role and capabilities.
"""
        
        input_messages = [SystemMessage(content=system_instruction)] + input_messages
        
        # 3. Invoke LLM
        response = await llm.ainvoke(input_messages)
        
        # 4. Return update
        # We simply return the new AIMessage. 
        # LangGraph's reducer will append it to state["messages"].
        return {
            "messages": [response],
            "active_agent": config.name,
        }
        
    return agent_node
