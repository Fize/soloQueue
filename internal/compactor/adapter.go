package compactor

import (
	"context"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

// AgentChatClient adapts agent.LLMClient to the compactor.ChatClient interface.
//
// This breaks the circular dependency: compactor package does not import
// agent package at compile time — the adapter is only used by the upstream
// (cmd/soloqueue) which imports both packages.
type AgentChatClient struct {
	Client agent.LLMClient
}

// NewAgentChatClient creates a new adapter wrapping an agent.LLMClient.
func NewAgentChatClient(client agent.LLMClient) *AgentChatClient {
	return &AgentChatClient{Client: client}
}

// Chat implements ChatClient by delegating to agent.LLMClient.Chat.
func (a *AgentChatClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	// Convert ChatRequest to agent.LLMRequest
	msgs := make([]agent.LLMMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, agent.LLMMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	resp, err := a.Client.Chat(ctx, agent.LLMRequest{
		Model:    req.Model,
		Messages: msgs,
	})
	if err != nil {
		return nil, err
	}

	return &ChatResponse{
		Content: resp.Content,
	}, nil
}
