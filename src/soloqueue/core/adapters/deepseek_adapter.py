"""
DeepSeek adapter for reasoning models.
"""

from langchain_core.language_models import BaseChatModel
from langchain_core.tools import BaseTool

from soloqueue.core.adapters.base import ModelAdapter, ModelCapabilities
from soloqueue.core.adapters.reasoning_wrapper import ReasoningChatOpenAI
from soloqueue.core.logger import logger


class DeepSeekReasonerAdapter(ModelAdapter):
    """
    Adapter for DeepSeek reasoning models (R1, deepseek-reasoner).
    
    As of DeepSeek R1-0528 (May 2025), the model natively supports
    function calling with reasoning mode enabled.
    
    Uses ReasoningChatOpenAI to properly handle reasoning_content
    in multi-turn tool calling.
    """
    
    def get_capabilities(self) -> ModelCapabilities:
        return ModelCapabilities(
            supports_reasoning=True,
            supports_tools=True,
            supports_reasoning_with_tools=True,  # Supported since R1-0528
        )
    
    def create(
        self,
        model: str,
        api_key: str,
        base_url: str | None = None,
        tools: list[BaseTool] | None = None,
        reasoning: bool = False,
    ) -> BaseChatModel:
        # DeepSeek R1 models always operate in reasoning mode
        # Use ReasoningChatOpenAI to ensure reasoning_content is properly
        # preserved during multi-turn tool calling
        logger.debug("DeepSeek Reasoner: Using ReasoningChatOpenAI")
        return ReasoningChatOpenAI(
            model=model,
            api_key=api_key,
            base_url=base_url,
            temperature=0,
            streaming=True,
        )
