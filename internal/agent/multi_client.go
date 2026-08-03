package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// RoutingClient routes LLM requests to sub-clients by LLMRequest.ProviderID.
type RoutingClient struct {
	mu      sync.RWMutex
	clients map[string]LLMClient
}

func NewRoutingClient(clients map[string]LLMClient) *RoutingClient {
	return &RoutingClient{
		clients: clients,
	}
}

func (c *RoutingClient) UpdateClients(clients map[string]LLMClient) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clients = clients
}

func (c *RoutingClient) Chat(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	client, err := c.resolveClient(req.ProviderID)
	if err != nil {
		return nil, err
	}
	return client.Chat(ctx, req)
}

func (c *RoutingClient) ChatStream(ctx context.Context, req LLMRequest) (<-chan llm.Event, error) {
	client, err := c.resolveClient(req.ProviderID)
	if err != nil {
		return nil, err
	}
	return client.ChatStream(ctx, req)
}

func (c *RoutingClient) resolveClient(providerID string) (LLMClient, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if providerID == "" {
		return nil, fmt.Errorf("routing client: provider ID is empty")
	}

	client, ok := c.clients[providerID]
	if !ok {
		return nil, fmt.Errorf("routing client: provider %q not initialized or not found", providerID)
	}

	return client, nil
}
