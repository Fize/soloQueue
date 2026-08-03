package agent

import "github.com/xiaobaitu/soloqueue/internal/agent/llmtypes"

// LLMMessage is a message passed to the LLM
//
// Re-exported from llmtypes so external consumers can keep using
// agent.LLMMessage. See llmtypes for the full documentation.
type LLMMessage = llmtypes.LLMMessage

// LLMRequest is the input for LLMClient.Chat / ChatStream
//
// Re-exported from llmtypes so external consumers can keep using
// agent.LLMRequest. See llmtypes for the full documentation.
type LLMRequest = llmtypes.LLMRequest

// LLMResponse is the return value of LLMClient.Chat
//
// Re-exported from llmtypes so external consumers can keep using
// agent.LLMResponse. See llmtypes for the full documentation.
type LLMResponse = llmtypes.LLMResponse

// LLMClient is the minimal interface for LLM calls
//
// Re-exported from llmtypes so external consumers can keep using
// agent.LLMClient. See llmtypes for the full documentation.
type LLMClient = llmtypes.LLMClient
