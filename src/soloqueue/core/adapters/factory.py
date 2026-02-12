"""
Model Adapter Factory.
"""

from langchain_core.language_models import BaseChatModel
from langchain_core.tools import BaseTool

from soloqueue.core.adapters.base import ModelAdapter
from soloqueue.core.adapters.openai_adapter import OpenAIAdapter
from soloqueue.core.adapters.kimi_adapter import KimiAdapter
from soloqueue.core.adapters.deepseek_adapter import DeepSeekReasonerAdapter
from soloqueue.core.config import settings
from soloqueue.core.logger import logger


class ModelAdapterFactory:
    """
    Factory for creating model adapters based on model name.
    
    Uses prefix matching to select the appropriate adapter.
    Falls back to OpenAIAdapter for unknown models.
    """
    
    _registry: dict[str, type[ModelAdapter]] = {
        "kimi": KimiAdapter,
        "deepseek-reasoner": DeepSeekReasonerAdapter,
        "deepseek-r1": DeepSeekReasonerAdapter,
    }
    
    @classmethod
    def create(
        cls,
        model: str | None = None,
        tools: list[BaseTool] | None = None,
        reasoning: bool = False,
    ) -> BaseChatModel:
        """
        Create a LangChain-compatible ChatModel for the given model.
        
        Args:
            model: Model identifier (e.g., "kimi-k2.5", "deepseek-reasoner").
                   If None, uses settings.DEFAULT_MODEL.
            tools: Optional list of tools the model will use
            reasoning: Whether to enable reasoning/thinking mode (from agent config)
            
        Returns:
            A configured BaseChatModel instance
        """
        if not settings.OPENAI_API_KEY:
            raise ValueError("OPENAI_API_KEY is not set in .env or environment variables.")
        
        target_model = model or settings.DEFAULT_MODEL
        
        adapter = cls._get_adapter(target_model)
        logger.debug(f"Using adapter {adapter.__class__.__name__} for model {target_model}")
        
        return adapter.create(
            model=target_model,
            api_key=settings.OPENAI_API_KEY,
            base_url=settings.OPENAI_BASE_URL,
            tools=tools,
            reasoning=reasoning,
        )
    
    @classmethod
    def _get_adapter(cls, model: str) -> ModelAdapter:
        """Get the appropriate adapter for a model using prefix matching."""
        model_lower = model.lower()
        for prefix, adapter_cls in cls._registry.items():
            if model_lower.startswith(prefix):
                return adapter_cls()
        return OpenAIAdapter()
    
    @classmethod
    def register(cls, prefix: str, adapter_cls: type[ModelAdapter]) -> None:
        """Register a new adapter for a model prefix."""
        cls._registry[prefix] = adapter_cls
