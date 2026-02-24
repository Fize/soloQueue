"""
Reasoning Chat Model.

Custom ChatOpenAI subclass that handles reasoning_content field preservation for
thinking mode models like Kimi K2.5 and DeepSeek R1.

These models require reasoning_content to be passed back to the API during multi-turn
tool calling in thinking mode.
"""

from collections.abc import Iterator, AsyncIterator
import json
from typing import Any

from langchain_core.callbacks import CallbackManagerForLLMRun, AsyncCallbackManagerForLLMRun
from langchain_core.messages import BaseMessage, AIMessage, AIMessageChunk
from langchain_core.outputs import ChatGenerationChunk, ChatResult
from langchain_openai import ChatOpenAI

from soloqueue.core.logger import logger
from soloqueue.core.context.token_counter import TokenCounter


# Maximum chars to keep for non-last reasoning_content
# DeepSeek R1 requires reasoning_content on EVERY AIMessage, so we can't remove them
# but we can truncate old ones to save tokens
# Setting to 0 effectively replaces context with just the placeholder
REASONING_TRUNCATE_LENGTH = 0

# Placeholder for AIMessages that don't have reasoning_content
# DeepSeek R1 requires this field to exist on ALL assistant messages
REASONING_PLACEHOLDER = "..."


class ReasoningChatOpenAI(ChatOpenAI):
    """
    ChatOpenAI subclass that preserves reasoning_content for thinking mode models.
    
    When models like Kimi K2.5 or DeepSeek R1 are in thinking mode and make tool calls,
    they return a reasoning_content field that must be passed back to the API in subsequent
    requests. Standard ChatOpenAI doesn't handle this automatically.
    
    This subclass:
    1. Captures reasoning_content from API responses and stores it in additional_kwargs
    2. Injects reasoning_content from additional_kwargs when sending messages to API
    3. TRUNCATES old reasoning_content (keeping only first N chars) to save tokens
    4. Only keeps the LAST AIMessage's reasoning_content FULL
    5. Provides placeholder for AIMessages missing reasoning_content (required by DeepSeek R1)
    """
    
    def _convert_messages_for_api(self, messages: list[BaseMessage]) -> list[dict[str, Any]]:
        """
        Convert messages to API format, injecting reasoning_content where needed.
        
        OPTIMIZATION: Only the LAST AIMessage's reasoning_content is kept FULL.
        Previous reasoning_content is TRUNCATED to save tokens.
        
        NOTE: DeepSeek R1 requires reasoning_content on EVERY AIMessage in thinking mode,
        so we provide a placeholder for messages that don't have it.
        """
        from langchain_openai.chat_models.base import _convert_message_to_dict
        
        # Find the index of the last AIMessage with reasoning_content
        last_reasoning_idx = None
        for i, message in enumerate(messages):
            if isinstance(message, (AIMessage, AIMessageChunk)):
                additional_kwargs = getattr(message, "additional_kwargs", {})
                if additional_kwargs.get("reasoning_content"):
                    last_reasoning_idx = i
        
        converted: list[dict[str, Any]] = []
        for i, message in enumerate(messages):
            msg_dict = _convert_message_to_dict(message)
            
            # DeepSeek R1 requires reasoning_content on ALL AIMessages
            if isinstance(message, (AIMessage, AIMessageChunk)):
                additional_kwargs = getattr(message, "additional_kwargs", {})
                reasoning_content = additional_kwargs.get("reasoning_content")
                
                if reasoning_content:
                    if i == last_reasoning_idx:
                        # Keep FULL reasoning_content for the LAST message
                        msg_dict["reasoning_content"] = reasoning_content
                        logger.debug(f"Injecting reasoning_content ({len(reasoning_content)} chars) [FULL]")
                    else:
                        # TRUNCATE reasoning_content for older messages
                        truncated = reasoning_content[:REASONING_TRUNCATE_LENGTH]
                        if len(reasoning_content) > REASONING_TRUNCATE_LENGTH:
                            truncated += "..."
                        msg_dict["reasoning_content"] = truncated
                        logger.debug(f"Injecting reasoning_content ({len(truncated)} chars) [TRUNCATED]")
                else:
                    # CRITICAL: DeepSeek R1 requires reasoning_content on ALL AIMessages
                    # Provide a placeholder for messages missing it
                    msg_dict["reasoning_content"] = REASONING_PLACEHOLDER
                    logger.debug("Injecting reasoning_content placeholder [MISSING]")
            
            # CRITICAL FIX: Ensure tool_calls are preserved
            # Sometimes _convert_message_to_dict might miss tool_calls if they are in additional_kwargs vs property
            if isinstance(message, (AIMessage, AIMessageChunk)) and message.tool_calls:
                 if "tool_calls" not in msg_dict:
                     # Manually reconstruct tool_calls if missing
                     logger.warning(f"Restoring missing tool_calls for message {i}")
                     msg_dict["tool_calls"] = [
                         {
                             "id": tc["id"],
                             "type": "function",
                             "function": {
                                 "name": tc["name"],
                                 "arguments": json.dumps(tc["args"]) if isinstance(tc["args"], dict) else tc["args"]
                             }
                         }
                         for tc in message.tool_calls
                     ]

            converted.append(msg_dict)
        
        return converted
    
    def _log_token_usage(self, messages: list[BaseMessage]) -> None:
        """Log token usage for debugging."""
        try:
            counter = TokenCounter(model=self.model_name)
            counter.log_token_usage(messages, model=self.model_name)
        except Exception as e:
            logger.debug(f"Token counting failed: {e}")
    
    def _convert_chunk_to_generation_chunk(
        self,
        chunk: dict,
        default_chunk_class: type,
        base_generation_info: dict | None,
    ) -> ChatGenerationChunk | None:
        """
        Override to capture reasoning_content from streaming responses.
        """
        # Call parent implementation first
        result = super()._convert_chunk_to_generation_chunk(
            chunk, default_chunk_class, base_generation_info
        )
        
        if result is None:
            return None
        
        # Check for reasoning_content in the chunk
        choices = chunk.get("choices", [])
        if choices:
            delta = choices[0].get("delta", {})
            reasoning_content = delta.get("reasoning_content")
            if reasoning_content and isinstance(result.message, AIMessageChunk):
                # Store reasoning_content in additional_kwargs
                if "reasoning_content" not in result.message.additional_kwargs:
                    result.message.additional_kwargs["reasoning_content"] = ""
                result.message.additional_kwargs["reasoning_content"] += reasoning_content
        
        return result
    
    def _generate(
        self,
        messages: list[BaseMessage],
        stop: list[str] | None = None,
        run_manager: CallbackManagerForLLMRun | None = None,
        **kwargs: Any,
    ) -> ChatResult:
        """Generate with reasoning_content preserved in messages."""
        # Log token usage
        self._log_token_usage(messages)
        
        # Convert messages ourselves to inject reasoning_content
        converted_messages = self._convert_messages_for_api(messages)
        
        # Get the payload that would normally be sent
        payload = self._get_request_payload(messages, stop=stop, **kwargs)
        
        # Replace the messages with our converted ones
        payload["messages"] = converted_messages
        
        # Make the API call directly
        self._ensure_sync_client_available()
        response = self.client.create(**payload)
        
        return self._create_chat_result(response)
    
    async def _agenerate(
        self,
        messages: list[BaseMessage],
        stop: list[str] | None = None,
        run_manager: AsyncCallbackManagerForLLMRun | None = None,
        **kwargs: Any,
    ) -> ChatResult:
        """Async generate with reasoning_content preserved in messages."""
        # Log token usage
        self._log_token_usage(messages)
        
        # Convert messages ourselves to inject reasoning_content
        converted_messages = self._convert_messages_for_api(messages)
        
        # Get the payload that would normally be sent
        payload = self._get_request_payload(messages, stop=stop, **kwargs)
        
        # Replace the messages with our converted ones
        payload["messages"] = converted_messages
        
        # Make the API call directly
        response = await self.async_client.create(**payload)
        
        return self._create_chat_result(response)
    
    def _stream(
        self,
        messages: list[BaseMessage],
        stop: list[str] | None = None,
        run_manager: CallbackManagerForLLMRun | None = None,
        **kwargs: Any,
    ) -> Iterator[ChatGenerationChunk]:
        """Stream with reasoning_content preserved in messages."""
        # Log token usage
        self._log_token_usage(messages)
        
        # Convert messages ourselves to inject reasoning_content
        converted_messages = self._convert_messages_for_api(messages)
        
        # Get the payload that would normally be sent
        payload = self._get_request_payload(messages, stop=stop, stream=True, **kwargs)
        
        # Replace the messages with our converted ones
        payload["messages"] = converted_messages
        
        # Make the streaming API call
        self._ensure_sync_client_available()
        response = self.client.create(**payload)
        
        for chunk in response:
            chunk_result = self._convert_chunk_to_generation_chunk(
                chunk.model_dump(), AIMessageChunk, None
            )
            if chunk_result:
                if run_manager:
                    run_manager.on_llm_new_token(
                        chunk_result.text, chunk=chunk_result
                    )
                yield chunk_result
    
    async def _astream(
        self,
        messages: list[BaseMessage],
        stop: list[str] | None = None,
        run_manager: AsyncCallbackManagerForLLMRun | None = None,
        **kwargs: Any,
    ) -> AsyncIterator[ChatGenerationChunk]:
        """Async stream with reasoning_content preserved in messages."""
        # Log token usage
        self._log_token_usage(messages)
        
        # Convert messages ourselves to inject reasoning_content
        converted_messages = self._convert_messages_for_api(messages)
        
        # Get the payload that would normally be sent
        payload = self._get_request_payload(messages, stop=stop, stream=True, **kwargs)
        
        # Replace the messages with our converted ones
        payload["messages"] = converted_messages
        
        # Make the async streaming API call
        response = await self.async_client.create(**payload)
        
        async for chunk in response:
            chunk_result = self._convert_chunk_to_generation_chunk(
                chunk.model_dump(), AIMessageChunk, None
            )
            if chunk_result:
                if run_manager:
                    await run_manager.on_llm_new_token(
                        chunk_result.text, chunk=chunk_result
                    )
                yield chunk_result
