"""
Kimi K2.5 adapter.
"""

from langchain_core.language_models import BaseChatModel
from langchain_core.tools import BaseTool

from soloqueue.core.adapters.base import ModelAdapter, ModelCapabilities
from soloqueue.core.adapters.reasoning_wrapper import ReasoningChatOpenAI
from soloqueue.core.logger import logger


class KimiAdapter(ModelAdapter):
    """
    Adapter for Kimi K2.5 models.
    
    Kimi K2.5 always requires temperature=1.0.
    Thinking mode is controlled by the user's reasoning config.
    Kimi internally handles tool calling with/without thinking mode.
    
    When reasoning is enabled, we use ReasoningChatOpenAI to ensure
    reasoning_content is properly preserved during tool calling.
    """
    
    def get_capabilities(self) -> ModelCapabilities:
        return ModelCapabilities(
            supports_reasoning=True,
            supports_tools=True,
            supports_reasoning_with_tools=True,  # Kimi handles this internally
            fixed_temperature=1.0,  # Kimi K2.5 always requires temperature=1.0
        )
    
    def create(
        self,
        model: str,
        api_key: str,
        base_url: str | None = None,
        tools: list[BaseTool] | None = None,
        reasoning: bool = False,
    ) -> BaseChatModel:
        # Kimi K2.5 ALWAYS requires temperature=1.0
        temperature = 1.0
        
        extra_body = None
        if reasoning:
            extra_body = {"thinking": {"type": "enabled"}}
            logger.debug(f"Kimi thinking mode enabled (temperature={temperature})")
        else:
            logger.debug(f"Kimi thinking mode disabled (temperature={temperature})")
        
        # Use ReasoningChatOpenAI when reasoning is enabled
        # This ensures reasoning_content is properly preserved during tool calling
        if reasoning:
            logger.debug("Using ReasoningChatOpenAI for thinking mode")
            return ReasoningChatOpenAI(
                model=model,
                api_key=api_key,
                base_url=base_url,
                temperature=temperature,
                streaming=True,
                extra_body=extra_body,
            )
        
        # Standard ChatOpenAI for non-reasoning mode
        from langchain_openai import ChatOpenAI
        return ChatOpenAI(
            model=model,
            api_key=api_key,
            base_url=base_url,
            temperature=temperature,
            streaming=True,
        )
