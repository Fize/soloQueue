# Hybrid Agent Architecture

Due to the strict API validation of DeepSeek R1 (`deepseek-reasoner`) regarding `reasoning_content` in conversation history, and its experimental tool calling support, we have adopted a Hybrid Architecture for SoloQueue groups.

## Configuration Strategy

### 1. Leader Agent (Orchestrator)
- **Model**: `deepseek-chat` (V3) or `gpt-4o`.
- **Role**: Task decomposition, routing, and tool calling.
- **Reasoning**: V3 provides robust tool calling support and lenient history validation, making it ideal for the graph's control flow.

### 2. Specialist Agent (Worker)
- **Model**: `deepseek-reasoner` (R1).
- **Role**: Deep analysis, complex reasoning, code generation.
- **Reasoning**: R1's "Chain of Thought" capabilities are leveraged here. Since Worker agents typically execute a single turn or don't manage complex multi-turn tool history, the API constraints are less critical.

## Constraints
- **Streaming**: The CLI supports parsing both V3 standard streams and R1 `reasoning_content` streams (via `cli.py` update).
- **History**: Care must be taken if R1 agents act as Leaders in the future; sophisticated message state management (preserving `reasoning_content`) would be required.
