"""
Model Adapters for multi-LLM support.

This module provides adapter classes that handle the differences between
various LLM providers while maintaining compatibility with LangChain/LangGraph.
"""

from soloqueue.core.adapters.base import ModelAdapter, ModelCapabilities
from soloqueue.core.adapters.factory import ModelAdapterFactory
from soloqueue.core.adapters.openai_adapter import OpenAIAdapter
from soloqueue.core.adapters.kimi_adapter import KimiAdapter
from soloqueue.core.adapters.deepseek_adapter import DeepSeekReasonerAdapter
from soloqueue.core.adapters.reasoning_wrapper import ReasoningChatOpenAI

__all__ = [
    "ModelAdapter",
    "ModelCapabilities",
    "ModelAdapterFactory",
    "OpenAIAdapter",
    "KimiAdapter",
    "DeepSeekReasonerAdapter",
    "ReasoningChatOpenAI",
]


