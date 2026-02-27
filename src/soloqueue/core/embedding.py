"""
Global Embedding Model with Multi-Provider Support

Supports:
- OpenAI API
- Ollama (local deployment)
- OpenAI-compatible APIs (GLM, DeepSeek, etc.)

Design: Global singleton with flexible configuration
"""

from typing import Optional, List
from dataclasses import dataclass
from loguru import logger
import os


@dataclass
class EmbeddingConfig:
    """Embedding model configuration."""
    provider: str  # "openai", "ollama", "openai-compatible"
    model: str
    api_base: Optional[str] = None
    api_key: Optional[str] = None
    dimension: Optional[int] = None  # Auto-detect if None


# Global singleton instance
_global_embedding_model: Optional['EmbeddingModel'] = None
_config_loaded = False


class EmbeddingModel:
    """
    Unified embedding model interface supporting multiple API providers.
    
    Examples:
        # OpenAI
        model = EmbeddingModel(EmbeddingConfig(
            provider="openai",
            model="text-embedding-3-small",
            api_key="sk-..."
        ))
        
        # Ollama
        model = EmbeddingModel(EmbeddingConfig(
            provider="ollama",
            model="nomic-embed-text",
            api_base="http://localhost:11434/v1"
        ))
        
        # GLM (OpenAI-compatible)
        model = EmbeddingModel(EmbeddingConfig(
            provider="openai-compatible",
            model="embedding-2",
            api_base="https://open.bigmodel.cn/api/paas/v4",
            api_key="your-glm-key",
            dimension=1024
        ))
    """
    
    def __init__(self, config: EmbeddingConfig):
        self.config = config
        self.client = None
        self._dimension = config.dimension
        
        # Initialize based on provider
        if config.provider == "openai":
            self._init_openai()
        elif config.provider == "ollama":
            self._init_ollama()
        elif config.provider == "openai-compatible":
            self._init_openai_compatible()
        else:
            raise ValueError(f"Unknown provider: {config.provider}")
    
    def _init_openai(self):
        """Initialize OpenAI embedding client."""
        try:
            from openai import OpenAI
            
            api_key = self.config.api_key or os.getenv("OPENAI_API_KEY")
            if not api_key:
                raise ValueError("OpenAI API key not provided")
            
            self.client = OpenAI(api_key=api_key)
            
            # Set dimensions based on model
            model_dimensions = {
                "text-embedding-3-small": 1536,
                "text-embedding-3-large": 3072,
                "text-embedding-ada-002": 1536
            }
            self._dimension = model_dimensions.get(self.config.model, 1536)
            
            logger.info(f"Initialized OpenAI embedding: {self.config.model}")
        except ImportError:
            raise RuntimeError(
                "openai package is required. "
                "Install with: pip install openai"
            )
    
    def _init_ollama(self):
        """Initialize Ollama embedding client."""
        try:
            from openai import OpenAI
            
            # Ollama uses OpenAI-compatible API
            api_base = self.config.api_base or "http://localhost:11434/v1"
            
            self.client = OpenAI(
                base_url=api_base,
                api_key="ollama"  # Ollama doesn't need a real key
            )
            
            # Ollama models typically use 768 or 384 dimensions
            # Will be detected on first call
            self._dimension = self.config.dimension or 768
            
            logger.info(f"Initialized Ollama embedding: {self.config.model} at {api_base}")
        except ImportError:
            raise RuntimeError(
                "openai package is required for Ollama. "
                "Install with: pip install openai"
            )
    
    def _init_openai_compatible(self):
        """Initialize OpenAI-compatible API (GLM, DeepSeek, etc.)."""
        try:
            from openai import OpenAI
            
            if not self.config.api_base:
                raise ValueError("api_base is required for openai-compatible provider")
            
            api_key = self.config.api_key or os.getenv("EMBEDDING_API_KEY")
            if not api_key:
                raise ValueError("API key not provided for openai-compatible provider")
            
            self.client = OpenAI(
                base_url=self.config.api_base,
                api_key=api_key
            )
            
            # Dimension must be specified in config for custom providers
            if not self._dimension:
                logger.warning("Dimension not specified, defaulting to 1536")
                self._dimension = 1536
            
            logger.info(f"Initialized {self.config.api_base} embedding: {self.config.model}")
        except ImportError:
            raise RuntimeError(
                "openai package is required. "
                "Install with: pip install openai"
            )
    
    def embed(self, texts: str | List[str]) -> List[List[float]]:
        """
        Generate embeddings for text(s).
        
        Args:
            texts: Single text or list of texts
        
        Returns:
            List of embedding vectors
        """
        # Normalize input to list
        if isinstance(texts, str):
            texts = [texts]
        
        # All providers use OpenAI-compatible interface
        response = self.client.embeddings.create(
            model=self.config.model,
            input=texts
        )
        return [item.embedding for item in response.data]
    
    @property
    def dimension(self) -> int:
        """Get embedding dimension."""
        return self._dimension


# ============================================================================
# Global Singleton API
# ============================================================================

def get_embedding_model() -> Optional[EmbeddingModel]:
    """
    Get the global embedding model (singleton).
    
    Returns None if embedding is not configured/enabled.
    Automatically loads from config on first call.
    """
    global _global_embedding_model, _config_loaded
    
    if not _config_loaded:
        _config_loaded = True
        _global_embedding_model = _load_from_config()
    
    return _global_embedding_model


def _load_from_config() -> Optional[EmbeddingModel]:
    """Load embedding model from system configuration."""
    try:
        # Load from Settings (which loads from .env file)
        from soloqueue.core.config import settings
        
        embedding_cfg = {
            "enabled": settings.SOLOQUEUE_EMBEDDING_ENABLED,
            "provider": settings.SOLOQUEUE_EMBEDDING_PROVIDER,
            "model": settings.SOLOQUEUE_EMBEDDING_MODEL,
            "api_base": settings.SOLOQUEUE_EMBEDDING_API_BASE,
            "api_key": settings.SOLOQUEUE_EMBEDDING_API_KEY,
            "dimension": settings.SOLOQUEUE_EMBEDDING_DIMENSION
        }
        
        # Check if enabled
        if not embedding_cfg.get("enabled", False):
            logger.info("Semantic memory disabled (embedding.enabled=false)")
            return None
        
        # Parse configuration
        provider = embedding_cfg.get("provider", "openai")
        model = embedding_cfg.get("model")
        
        if not model:
            logger.warning("Semantic memory disabled (no embedding.model configured)")
            return None
        
        # Build EmbeddingConfig
        config = EmbeddingConfig(
            provider=provider,
            model=model,
            api_base=embedding_cfg.get("api_base"),
            api_key=embedding_cfg.get("api_key"),
            dimension=embedding_cfg.get("dimension")
        )
        
        # Initialize model
        return EmbeddingModel(config)
    
    except Exception as e:
        logger.error(f"Failed to load embedding model: {e}")
        return None


def embed_text(texts: str | List[str]) -> Optional[List[List[float]]]:
    """
    Embed text using the global embedding model.
    
    Args:
        texts: Single text or list of texts
    
    Returns:
        List of embeddings, or None if embedding is not available
    """
    model = get_embedding_model()
    if model is None:
        return None
    
    return model.embed(texts)


def is_embedding_available() -> bool:
    """Check if embedding is configured and available."""
    return get_embedding_model() is not None


def get_embedding_dimension() -> Optional[int]:
    """Get the dimension of the global embedding model."""
    model = get_embedding_model()
    return model.dimension if model else None


# ============================================================================
# Testing & Examples
# ============================================================================

if __name__ == "__main__":
    # Example 1: OpenAI (requires API key)
    print("=" * 60)
    print("Example 1: OpenAI Embedding")
    print("=" * 60)
    print("Requires: export OPENAI_API_KEY=sk-...")
    print()
    
    # Example 2: Ollama (requires Ollama running)
    print("=" * 60)
    print("Example 2: Ollama Embedding")
    print("=" * 60)
    print("Setup:")
    print("  1. Install Ollama: https://ollama.ai")
    print("  2. Start Ollama: ollama serve")
    print("  3. Pull model: ollama pull nomic-embed-text")
    print()
    
    # Uncomment to test with Ollama
    # ollama_model = EmbeddingModel(EmbeddingConfig(
    #     provider="ollama",
    #     model="nomic-embed-text",
    #     api_base="http://localhost:11434/v1"
    # ))
    # result = ollama_model.embed("Hello, Ollama!")
    # print(f"Ollama embedding dimension: {len(result[0])}")
    
    # Example 3: GLM (requires API key)
    print("=" * 60)
    print("Example 3: GLM Embedding (OpenAI-compatible)")
    print("=" * 60)
    print("Setup:")
    print("  1. Register at: https://open.bigmodel.cn/")
    print("  2. Create API key")
    print("  3. export EMBEDDING_API_KEY=your-glm-key")
    print()
    
    # Uncomment to test with GLM
    # glm_model = EmbeddingModel(EmbeddingConfig(
    #     provider="openai-compatible",
    #     model="embedding-2",
    #     api_base="https://open.bigmodel.cn/api/paas/v4",
    #     api_key="your-glm-key",
    #     dimension=1024
    # ))
    # result = glm_model.embed("你好，GLM！")
    # print(f"GLM embedding dimension: {len(result[0])}")
