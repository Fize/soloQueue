"""
OpenAI-compatible adapter (default).
"""

from langchain_openai import ChatOpenAI
from langchain_core.language_models import BaseChatModel
from langchain_core.tools import BaseTool

from soloqueue.core.adapters.base import ModelAdapter, ModelCapabilities


class OpenAIAdapter(ModelAdapter):
    """
    Default adapter for OpenAI-compatible models.
    
    This adapter passes through to ChatOpenAI without any special handling.
    Use this for models that fully support the OpenAI API format.
    """
    
    def get_capabilities(self) -> ModelCapabilities:
        return ModelCapabilities(
            supports_reasoning=False,
            supports_tools=True,
            supports_reasoning_with_tools=True,
        )
    
    def create(
        self,
        model: str,
        api_key: str,
        base_url: str | None = None,
        tools: list[BaseTool] | None = None,
        reasoning: bool = False,
    ) -> BaseChatModel:
        # OpenAI-compatible models don't support reasoning mode
        # reasoning parameter is ignored
        return ChatOpenAI(
            model=model,
            api_key=api_key,
            base_url=base_url,
            temperature=0,
            streaming=True,
        )
