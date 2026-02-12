"""
Tests for the global embedding model system (API-based only).

Note: Tests use mocks to avoid actual API calls.
"""

import pytest
import os
from unittest.mock import Mock, patch, MagicMock


@patch('openai.OpenAI')
def test_openai_embedding(mock_openai_class):
    """Test OpenAI embedding initialization and usage."""
    from soloqueue.core.embedding import EmbeddingModel, EmbeddingConfig
    
    # Mock OpenAI response
    mock_client = MagicMock()
    mock_response = MagicMock()
    mock_response.data = [
        MagicMock(embedding=[0.1] * 1536),
        MagicMock(embedding=[0.2] * 1536)
    ]
    mock_client.embeddings.create.return_value = mock_response
    mock_openai_class.return_value = mock_client
    
    # Create OpenAI embedding model
    config = EmbeddingConfig(
        provider="openai",
        model="text-embedding-3-small",
        api_key="test-key"
    )
    
    model = EmbeddingModel(config)
    
    # Test embedding
    result = model.embed(["Text 1", "Text 2"])
    
    assert len(result) == 2
    assert len(result[0]) == 1536
    assert model.dimension == 1536
    
    # Verify API was called correctly
    mock_client.embeddings.create.assert_called_once_with(
        model="text-embedding-3-small",
        input=["Text 1", "Text 2"]
    )


@patch('openai.OpenAI')
def test_ollama_embedding(mock_openai_class):
    """Test Ollama embedding initialization."""
    from soloqueue.core.embedding import EmbeddingModel, EmbeddingConfig
    
    # Mock Ollama response
    mock_client = MagicMock()
    mock_response = MagicMock()
    mock_response.data = [MagicMock(embedding=[0.1] * 768)]
    mock_client.embeddings.create.return_value = mock_response
    mock_openai_class.return_value = mock_client
    
    config = EmbeddingConfig(
        provider="ollama",
        model="nomic-embed-text",
        api_base="http://localhost:11434/v1",
        dimension=768
    )
    
    model = EmbeddingModel(config)
    result = model.embed("Test text")
    
    assert len(result) == 1
    assert len(result[0]) == 768
    assert model.dimension == 768
    
    # Verify Ollama endpoint was used
    mock_openai_class.assert_called_with(
        base_url="http://localhost:11434/v1",
        api_key="ollama"
    )


@patch('openai.OpenAI')
def test_openai_compatible_embedding(mock_openai_class):
    """Test OpenAI-compatible embedding (GLM, etc.)."""
    from soloqueue.core.embedding import EmbeddingModel, EmbeddingConfig
    
    # Mock response
    mock_client = MagicMock()
    mock_response = MagicMock()
    mock_response.data = [MagicMock(embedding=[0.1] * 1024)]
    mock_client.embeddings.create.return_value = mock_response
    mock_openai_class.return_value = mock_client
    
    config = EmbeddingConfig(
        provider="openai-compatible",
        model="embedding-2",
        api_base="https://open.bigmodel.cn/api/paas/v4",
        api_key="test-glm-key",
        dimension=1024
    )
    
    model = EmbeddingModel(config)
    result = model.embed("测试文本")
    
    assert len(result) == 1
    assert len(result[0]) == 1024
    assert model.dimension == 1024
    
    # Verify custom endpoint was used
    mock_openai_class.assert_called_with(
        base_url="https://open.bigmodel.cn/api/paas/v4",
        api_key="test-glm-key"
    )


def test_invalid_provider():
    """Test that invalid provider raises error."""
    from soloqueue.core.embedding import EmbeddingModel, EmbeddingConfig
    
    config = EmbeddingConfig(
        provider="invalid-provider",
        model="some-model"
    )
    
    with pytest.raises(ValueError, match="Unknown provider"):
        EmbeddingModel(config)


def test_openai_missing_api_key():
    """Test that OpenAI without API key raises error."""
    from soloqueue.core.embedding import EmbeddingModel, EmbeddingConfig
    
    config = EmbeddingConfig(
        provider="openai",
        model="text-embedding-3-small"
    )
    
    # Clear env var if exists
    with patch.dict(os.environ, {}, clear=True):
        with pytest.raises(ValueError, match="API key not provided"):
            EmbeddingModel(config)


def test_openai_compatible_missing_api_base():
    """Test that openai-compatible without api_base raises error."""
    from soloqueue.core.embedding import EmbeddingModel, EmbeddingConfig
    
    config = EmbeddingConfig(
        provider="openai-compatible",
        model="some-model",
        api_key="test-key"
    )
    
    with pytest.raises(ValueError, match="api_base is required"):
        EmbeddingModel(config)


@patch.dict(os.environ, {
    "SOLOQUEUE_EMBEDDING_ENABLED": "true",
    "SOLOQUEUE_EMBEDDING_PROVIDER": "openai",
    "SOLOQUEUE_EMBEDDING_MODEL": "text-embedding-3-small",
    "OPENAI_API_KEY": "test-key"
})
@patch('openai.OpenAI')
def test_global_singleton(mock_openai_class):
    """Test that global embedding model is a singleton."""
    from soloqueue.core.embedding import get_embedding_model
    from soloqueue.core import embedding as emb_module
    
    # Mock OpenAI
    mock_client = MagicMock()
    mock_openai_class.return_value = mock_client
    
    # Reset global state
    emb_module._global_embedding_model = None
    emb_module._config_loaded = False
    
    # First call loads model
    model1 = get_embedding_model()
    assert model1 is not None
    
    # Second call returns same instance
    model2 = get_embedding_model()
    assert model1 is model2  # Same object


@patch.dict(os.environ, {"SOLOQUEUE_EMBEDDING_ENABLED": "false"}, clear=True)
def test_embedding_disabled():
    """Test that embedding can be disabled via config."""
    from soloqueue.core.embedding import get_embedding_model, is_embedding_available
    from soloqueue.core import embedding as emb_module
    
    # Reset global state
    emb_module._global_embedding_model = None
    emb_module._config_loaded = False
    
    model = get_embedding_model()
    assert model is None
    assert not is_embedding_available()


@patch.dict(os.environ, {
    "SOLOQUEUE_EMBEDDING_ENABLED": "true",
    "SOLOQUEUE_EMBEDDING_PROVIDER": "openai",
    "SOLOQUEUE_EMBEDDING_MODEL": "text-embedding-3-small",
    "OPENAI_API_KEY": "test-key"
})
@patch('openai.OpenAI')
def test_embed_text_convenience_function(mock_openai_class):
    """Test the convenience embed_text() function."""
    from soloqueue.core.embedding import embed_text
    from soloqueue.core import embedding as emb_module
    
    # Mock OpenAI
    mock_client = MagicMock()
    mock_response = MagicMock()
    mock_response.data = [
        MagicMock(embedding=[0.1] * 1536)  # Single embedding for single text
    ]
    mock_client.embeddings.create.return_value = mock_response
    mock_openai_class.return_value = mock_client
    
    # Reset
    emb_module._global_embedding_model = None
    emb_module._config_loaded = False
    
    # Test single text
    result = embed_text("Hello")
    assert result is not None
    assert len(result) == 1
    assert len(result[0]) == 1536


@patch.dict(os.environ, {"SOLOQUEUE_EMBEDDING_ENABLED": "false"}, clear=True)
def test_embed_text_when_disabled():
    """Test embed_text returns None when embedding is disabled."""
    from soloqueue.core.embedding import embed_text
    from soloqueue.core import embedding as emb_module
    
    # Reset
    emb_module._global_embedding_model = None
    emb_module._config_loaded = False
    
    result = embed_text("Hello")
    assert result is None


@patch.dict(os.environ, {
    "SOLOQUEUE_EMBEDDING_ENABLED": "true",
    "SOLOQUEUE_EMBEDDING_PROVIDER": "openai",
    "SOLOQUEUE_EMBEDDING_MODEL": "text-embedding-3-small",
    "OPENAI_API_KEY": "test-key"
})
@patch('openai.OpenAI')
def test_dimension_property(mock_openai_class):
    """Test that dimension property works."""
    from soloqueue.core.embedding import get_embedding_dimension
    from soloqueue.core import embedding as emb_module
    
    # Mock OpenAI
    mock_client = MagicMock()
    mock_openai_class.return_value = mock_client
    
    # Reset
    emb_module._global_embedding_model = None
    emb_module._config_loaded = False
    
    dim = get_embedding_dimension()
    assert dim == 1536  # OpenAI text-embedding-3-small


def test_single_text_normalization():
    """Test that single string is normalized to list."""
    from soloqueue.core.embedding import EmbeddingModel, EmbeddingConfig
    
    with patch('openai.OpenAI') as mock_openai_class:
        mock_client = MagicMock()
        mock_response = MagicMock()
        mock_response.data = [MagicMock(embedding=[0.1] * 1536)]
        mock_client.embeddings.create.return_value = mock_response
        mock_openai_class.return_value = mock_client
        
        config = EmbeddingConfig(
            provider="openai",
            model="text-embedding-3-small",
            api_key="test-key"
        )
        model = EmbeddingModel(config)
        
        # Single string should be converted to list
        result = model.embed("Single text")
        assert len(result) == 1
        
        # Verify API was called with list
        call_args = mock_client.embeddings.create.call_args
        assert call_args.kwargs['input'] == ["Single text"]


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
