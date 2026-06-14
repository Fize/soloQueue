package simulation

import (
	"context"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// SimAgent wraps an agent.Agent for simulation use.
// Each SimAgent owns one Agent instance, a persistent ContextWindow for LLM cache,
// and an AgentMemory that retains full history for analysis.
type SimAgent struct {
	agent       *agent.Agent
	persona     *Persona
	cw          *ctxwin.ContextWindow
	memory      *AgentMemory
	bus         *MessageBus
	log         *logger.Logger
	timeout     time.Duration
	tokenBudget int
	totalTokens int
	instanceID  string
	personaID   string
}

// NewSimAgent wraps an already-created and started agent.Agent.
func NewSimAgent(
	agt *agent.Agent,
	persona *Persona,
	cw *ctxwin.ContextWindow,
	bus *MessageBus,
	log *logger.Logger,
	timeout time.Duration,
) *SimAgent {
	return &SimAgent{
		agent:       agt,
		persona:     persona,
		cw:          cw,
		memory:      NewAgentMemory(persona.ID),
		bus:         bus,
		log:         log,
		timeout:     timeout,
		tokenBudget: personaTokenBudget(persona, agt.Def.ContextWindow),
		instanceID:  agt.InstanceID,
		personaID:   persona.ID,
	}
}

// PushSystemPrompt pushes the Generative Agents system prompt into the CW.
// Called once during agent creation, before the simulation starts.
func (sa *SimAgent) PushSystemPrompt(prompt string) {
	sa.cw.Push(ctxwin.RoleSystem, prompt)
}

// ClearCW replaces the context window with a fresh one containing only the system prompt.
func (sa *SimAgent) ClearCW(systemPrompt string) {
	maxTokens := sa.agent.Def.ContextWindow
	if maxTokens <= 0 {
		maxTokens = agent.DefaultContextWindow
	}
	sa.cw = ctxwin.NewContextWindow(maxTokens, 2000, 0, ctxwin.NewTokenizer())
	sa.cw.Push(ctxwin.RoleSystem, systemPrompt)
}

// AskRaw sends a raw user message to the LLM and returns the response.
// Does not record to AgentMemory. Used by the GA agent loop which handles
// memory recording separately.
func (sa *SimAgent) AskRaw(ctx context.Context, userMsg string) (string, error) {
	askCtx, cancel := context.WithTimeout(ctx, sa.timeout)
	defer cancel()

	content, _, err := sa.agent.AskWithHistory(askCtx, sa.cw, userMsg)
	if err != nil {
		if sa.log != nil {
			sa.log.WarnContext(ctx, logger.CatSimulation, "simagent: ask failed",
				"persona_id", sa.personaID, "err", err.Error())
		}
		return "", fmt.Errorf("agent %s: %w", sa.persona.Name, err)
	}
	return content, nil
}

// PushToCW pushes a message to the context window.
func (sa *SimAgent) PushToCW(role ctxwin.MessageRole, content string) {
	sa.cw.Push(role, content)
}

func (sa *SimAgent) Stop(timeout time.Duration) error {
	return sa.agent.Stop(timeout)
}

func (sa *SimAgent) InstanceID() string     { return sa.instanceID }
func (sa *SimAgent) PersonaID() string       { return sa.personaID }
func (sa *SimAgent) Persona() *Persona       { return sa.persona }
func (sa *SimAgent) Memory() *AgentMemory    { return sa.memory }
func (sa *SimAgent) ContextWindow() *ctxwin.ContextWindow { return sa.cw }

func personaTokenBudget(persona *Persona, contextWindow int) int {
	if contextWindow <= 0 {
		contextWindow = 128000
	}
	return contextWindow * 80 / 100
}

