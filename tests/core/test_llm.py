
import pytest
from unittest.mock import MagicMock, patch
from soloqueue.core.llm import LLMFactory

def test_llm_factory_initialization():
    """Test that LLMFactory creates a ChatOpenAI instance with correct config."""
    
    # Mock settings to ensure predictable values
    with patch("soloqueue.core.llm.settings") as mock_settings:
        mock_settings.DEFAULT_MODEL = "test-model"
        mock_settings.OPENAI_API_KEY = "sk-test"
        mock_settings.OPENAI_BASE_URL = "https://api.test/v1"
        
        # We don't want to actually connect to OpenAI, so we trust the constructor works
        # but we can verify properties if we mock ChatOpenAI or inspect the result
        llm = LLMFactory.get_llm()
        
        assert llm.model_name == "test-model"
        assert llm.openai_api_key.get_secret_value() == "sk-test"
        assert llm.openai_api_base == "https://api.test/v1"
        assert llm.temperature == 0

def test_llm_factory_model_override():
    """Test overriding the default model."""
    with patch("soloqueue.core.llm.settings") as mock_settings:
        mock_settings.DEFAULT_MODEL = "default"
        # API Key is mandatory for ChatOpenAI validation
        mock_settings.OPENAI_API_KEY = "sk-test" 
        mock_settings.OPENAI_BASE_URL = None # Explicitly set to None/str
        
        llm = LLMFactory.get_llm(model="specific-model")
        assert llm.model_name == "specific-model"
