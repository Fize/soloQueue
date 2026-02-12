"""
ContextBuilder - Priority-based context construction for LLM prompts.

PRODUCTION SPECIFICATION:
- 95% Safety Margin to handle tokenizer variance
- Priority 0: System Prompt (always included)
- Priority 1: Recent History (sliding window, newest first)
- Respects token budgets and response buffers
"""

import logging
from typing import List
from langchain_core.messages import BaseMessage, SystemMessage

from soloqueue.core.context.token_counter import TokenCounter

logger = logging.getLogger(__name__)

class ContextBuilder:
    """
    Builds optimized context for LLM within token limits.
    
    Strategy:
    1. Reserve response buffer (default: 4096 tokens)
    2. Apply safety margin (95% of limit)
    3. Always include system prompt (Priority 0)
    4. Fill remaining with recent history (Priority 1, newest first)
    """
    
    def __init__(
        self, 
        token_counter: TokenCounter,
        response_buffer: int = 4096,
        safety_margin: float = 0.95
    ):
        """
        Initialize context builder.
        
        Args:
            token_counter: TokenCounter instance
            response_buffer: Tokens reserved for model response
            safety_margin: Fraction of limit to use (0.95 = 95%)
        """
        self.token_counter = token_counter
        self.response_buffer = response_buffer
        self.safety_margin = safety_margin
    
    def build_context(
        self,
        system_prompt: str | SystemMessage,
        history: List[BaseMessage],
        model_limit: int | None = None
    ) -> List[BaseMessage]:
        """
        Build context within token limits.
        
        Args:
            system_prompt: System prompt (Priority 0)
            history: Conversation history (Priority 1)
            model_limit: Optional override for model limit
        
        Returns:
            List of messages that fit within budget
        """
        # 0. Determine effective limit
        if model_limit is None:
            model_limit = self.token_counter.get_model_limit()
        
        safe_limit = int(model_limit * self.safety_margin)
        budget = safe_limit - self.response_buffer
        
        logger.debug(f"Context budget: {budget:,} tokens (limit: {model_limit:,}, safety: {safe_limit:,})")
        
        # 1. System Prompt (Priority 0 - Always included)
        if isinstance(system_prompt, str):
            sys_msg = SystemMessage(content=system_prompt)
        else:
            sys_msg = system_prompt
        
        sys_tokens = self.token_counter.count_message(sys_msg)
        remaining_budget = budget - sys_tokens
        
        if remaining_budget < 0:
            logger.warning(f"System prompt ({sys_tokens} tokens) exceeds budget! Truncating history to zero.")
            return [sys_msg]
        
        # 2. History (Priority 1 - Sliding window from newest)
        selected_msgs = []
        for msg in reversed(history):
            msg_tokens = self.token_counter.count_message(msg)
            
            if remaining_budget - msg_tokens < 0:
                logger.debug(f"Budget exhausted. Truncating {len(history) - len(selected_msgs)} older messages.")
                break
            
            selected_msgs.insert(0, msg)
            remaining_budget -= msg_tokens
        
        logger.debug(f"Built context: {len(selected_msgs)}/{len(history)} history messages ({budget - remaining_budget:,}/{budget:,} tokens)")
        
        return [sys_msg] + selected_msgs
    
    def estimate_tokens(self, messages: List[BaseMessage]) -> int:
        """Estimate total tokens for a list of messages."""
        return self.token_counter.count_messages(messages)
