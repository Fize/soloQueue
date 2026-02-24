"""
Token Counter for estimating message token counts.

Uses tiktoken to count tokens for OpenAI-compatible models.
"""

import tiktoken
from langchain_core.messages import BaseMessage, AIMessage, ToolMessage

from soloqueue.core.logger import logger


class TokenCounter:
    """
    Token counter using tiktoken for OpenAI-compatible models.
    
    Provides estimates for token counts to help manage context limits.
    """
    
    # Model context limits (approximate)
    MODEL_LIMITS = {
        "deepseek-reasoner": 131072,
        "deepseek-chat": 131072,
        "kimi-k2.5": 131072,
        "gpt-4o": 128000,
        "gpt-4-turbo": 128000,
        "gpt-4": 8192,
        "gpt-3.5-turbo": 16384,
    }
    
    def __init__(self, model: str = "gpt-4o"):
        """
        Initialize the token counter.
        
        Args:
            model: Model name for encoding selection (uses cl100k_base for most models)
        """
        self.model = model
        # Most modern models use cl100k_base encoding
        try:
            self.encoding = tiktoken.encoding_for_model(model)
        except KeyError:
            # Fall back to cl100k_base for unknown models
            self.encoding = tiktoken.get_encoding("cl100k_base")
    
    def count_text(self, text: str) -> int:
        """Count tokens in a text string."""
        if not text:
            return 0
        return len(self.encoding.encode(text))
    
    def count_message(self, message: BaseMessage) -> int:
        """
        Count tokens in a single message.
        
        Includes content, role overhead, and additional_kwargs like reasoning_content.
        """
        tokens = 0
        
        # Role overhead (approximate per message)
        tokens += 4  # role, content structure
        
        # Content
        content = message.content
        if isinstance(content, str):
            tokens += self.count_text(content)
        elif isinstance(content, list):
            # Multi-modal content
            for item in content:
                if isinstance(item, str):
                    tokens += self.count_text(item)
                elif isinstance(item, dict) and "text" in item:
                    tokens += self.count_text(item["text"])
        
        # Additional kwargs (like reasoning_content)
        additional_kwargs = getattr(message, "additional_kwargs", {})
        if "reasoning_content" in additional_kwargs:
            tokens += self.count_text(str(additional_kwargs["reasoning_content"]))
        
        # Tool calls (for AIMessage)
        if isinstance(message, AIMessage) and message.tool_calls:
            for tool_call in message.tool_calls:
                tokens += self.count_text(tool_call.get("name", ""))
                tokens += self.count_text(str(tool_call.get("args", {})))
                tokens += 10  # structure overhead
        
        # Tool message specifics
        if isinstance(message, ToolMessage):
            tokens += self.count_text(getattr(message, "name", "") or "")
            tokens += 5  # tool_call_id overhead
        
        return tokens
    
    def count_messages(self, messages: list[BaseMessage]) -> int:
        """Count total tokens in a list of messages."""
        total = 3  # Base overhead for messages array
        for message in messages:
            total += self.count_message(message)
        return total
    
    def get_model_limit(self, model: str | None = None) -> int:
        """Get the context limit for a model."""
        model_name = model or self.model
        return self.MODEL_LIMITS.get(model_name, 128000)  # Default to 128k
    
    def check_within_limit(
        self, 
        messages: list[BaseMessage], 
        model: str | None = None,
        buffer: int = 4096  # Reserve some space for completion
    ) -> tuple[bool, int, int]:
        """
        Check if messages are within the model's context limit.
        
        Returns:
            Tuple of (is_within_limit, token_count, limit)
        """
        token_count = self.count_messages(messages)
        limit = self.get_model_limit(model) - buffer
        return token_count <= limit, token_count, limit
    
    def log_token_usage(self, messages: list[BaseMessage], model: str | None = None) -> int:
        """Log token usage for debugging."""
        token_count = self.count_messages(messages)
        limit = self.get_model_limit(model)
        usage_pct = (token_count / limit) * 100
        
        logger.debug(f"Token usage: {token_count:,}/{limit:,} ({usage_pct:.1f}%)")
        
        if usage_pct > 80:
            logger.warning(f"High token usage: {usage_pct:.1f}%")
        
        return token_count
