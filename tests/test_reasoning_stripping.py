
import pytest
from langchain_core.messages import AIMessage, HumanMessage, ToolMessage
from soloqueue.core.adapters.reasoning_wrapper import ReasoningChatOpenAI, REASONING_PLACEHOLDER

class TestReasoningStripping:
    def test_strip_historical_reasoning(self):
        """Test that historical reasoning content is stripped (replaced with placeholder)."""
        
        # Setup messages: 
        # 1. User
        # 2. AI (Old) - Long reasoning
        # 3. User
        # 4. AI (New) - Long reasoning
        
        messages = [
            HumanMessage(content="Question 1"),
            AIMessage(
                content="Answer 1", 
                additional_kwargs={"reasoning_content": "This is old reasoning that should be stripped." * 10}
            ),
            HumanMessage(content="Question 2"),
            AIMessage(
                content="Answer 2", 
                additional_kwargs={"reasoning_content": "This is new reasoning that should be preserved."}
            )
        ]
        
        # Initialize adapter (mocking credentials as they aren't used for this method)
        adapter = ReasoningChatOpenAI(api_key="mock", base_url="mock")
        
        # Convert messages
        converted = adapter._convert_messages_for_api(messages)
        
        # Verify AI 1 (Index 1) - Should be stripped to REASONING_PLACEHOLDER
        ai1 = converted[1]
        assert ai1["role"] == "assistant"
        # Since REASONING_TRUNCATE_LENGTH = 0, slicing gives "" and then "..." is appended if len > 0
        assert ai1["reasoning_content"] == "..." 
        
        # Verify AI 2 (Index 3) - Should be preserved FULL
        ai2 = converted[3]
        assert ai2["role"] == "assistant"
        assert ai2["reasoning_content"] == "This is new reasoning that should be preserved."
        
    def test_preserve_tool_call_reasoning(self):
        """Test that reasoning accompanying a tool call (if it's the last AI msg) is preserved."""
        
        # Setup messages:
        # 1. User
        # 2. AI (Tool Call) - Has reasoning
        # 3. Tool Output <-- wait, if tool output is present, then AI Tool Call IS NOT the last message in list.
        # But `_convert_messages_for_api` treats `last_reasoning_idx` as the last AIMessage in the list.
        # So it WILL be preserved.
        
        messages = [
            HumanMessage(content="Do task"),
            AIMessage(
                content="",
                tool_calls=[{"name": "test_tool", "args": {}, "id": "call_1"}],
                additional_kwargs={"reasoning_content": "I need to call test_tool."}
            ),
            ToolMessage(content="Result", tool_call_id="call_1")
        ]
        
        adapter = ReasoningChatOpenAI(api_key="mock", base_url="mock")
        converted = adapter._convert_messages_for_api(messages)
        
        # Verify AI (Index 1) - Should be preserved FULL because it is the LAST AIMessage in the list
        ai = converted[1]
        assert ai["reasoning_content"] == "I need to call test_tool."
        
    def test_placeholder_insertion(self):
        """Test that placeholder is inserted if reasoning_content is missing (for DeepSeek R1)."""
        
        messages = [
            HumanMessage(content="Hi"),
            AIMessage(content="Hello") # No reasoning_content
        ]
        
        adapter = ReasoningChatOpenAI(api_key="mock", base_url="mock")
        converted = adapter._convert_messages_for_api(messages)
        
        ai = converted[1]
        assert ai["reasoning_content"] == REASONING_PLACEHOLDER

if __name__ == "__main__":
    # Manually run tests if executed as script
    t = TestReasoningStripping()
    t.test_strip_historical_reasoning()
    t.test_preserve_tool_call_reasoning()
    t.test_placeholder_insertion()
    print("All tests passed!")
