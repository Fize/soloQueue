
from langchain_openai import ChatOpenAI
from langchain_core.language_models import BaseChatModel

from soloqueue.core.config import settings
from soloqueue.core.logger import logger

class LLMFactory:
    """
    Factory for creating configured LLM instances.
    Supports OpenAI-compatible APIs (OpenAI, DeepSeek, etc.)
    """
    
    @staticmethod
    def get_llm(model: str | None = None) -> BaseChatModel:
        """
        Get a configured ChatOpenAI instance.
        
        Args:
            model: Model identifier (e.g., "claude-3-5-sonnet", "gpt-4").
                  If None, uses settings.DEFAULT_MODEL.
                   
        Returns:
            Configured LangChain ChatModel.
        """
        target_model = model or settings.DEFAULT_MODEL
        
        logger.debug(f"Creating LLM: model={target_model}, base_url={settings.OPENAI_BASE_URL}")
        
        return ChatOpenAI(
            model=target_model,
            api_key=settings.OPENAI_API_KEY,
            base_url=settings.OPENAI_BASE_URL,
            temperature=0,
            streaming=True 
        )
