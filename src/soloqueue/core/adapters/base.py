"""
Model Adapter base classes.
"""

from abc import ABC, abstractmethod
from dataclasses import dataclass

from langchain_core.language_models import BaseChatModel
from langchain_core.tools import BaseTool


@dataclass
class ModelCapabilities:
    """Describes the capabilities of a model."""
    
    supports_reasoning: bool = False
    """Whether the model supports reasoning/thinking mode."""
    
    supports_tools: bool = True
    """Whether the model supports tool/function calling."""
    
    supports_reasoning_with_tools: bool = True
    """Whether reasoning mode is compatible with tool calling."""
    
    fixed_temperature: float | None = None
    """If set, the model requires this specific temperature value."""


class ModelAdapter(ABC):
    """
    Abstract base class for model adapters.
    
    Adapters handle the differences between LLM providers while
    returning a LangChain-compatible BaseChatModel.
    """
    
    @abstractmethod
    def get_capabilities(self) -> ModelCapabilities:
        """Return the capabilities of this model."""
        ...
    
    @abstractmethod
    def create(
        self,
        model: str,
        api_key: str,
        base_url: str | None = None,
        tools: list[BaseTool] | None = None,
        reasoning: bool = False,
    ) -> BaseChatModel:
        """
        Create a LangChain-compatible ChatModel.
        
        Args:
            model: Model identifier (e.g., "kimi-k2.5", "deepseek-reasoner")
            api_key: API key for authentication
            base_url: Optional base URL for the API endpoint
            tools: Optional list of tools the model will use
            reasoning: Whether to enable reasoning/thinking mode (user preference)
            
        Returns:
            A configured BaseChatModel instance
        """
        ...
    
    def should_enable_reasoning(self, has_tools: bool) -> bool:
        """
        Determine if reasoning mode should be enabled.
        
        Args:
            has_tools: Whether the model will be used with tools
            
        Returns:
            True if reasoning should be enabled
        """
        caps = self.get_capabilities()
        if not caps.supports_reasoning:
            return False
        if has_tools and not caps.supports_reasoning_with_tools:
            return False
        return True
