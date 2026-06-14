package simulation

import (
	"context"
	"fmt"
	"strings"
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

// AskForRoundEvent is a deprecated compatibility shim for the old EventLoop.
// It builds a user message and calls the LLM directly. The new GA agent loop
// handles this through the tick-driven GAAgentLoop instead.
func (sa *SimAgent) AskForRoundEvent(ctx context.Context, seq int, topic string, worldState *WorldState, inbox []Message) (*RoundMessage, error) {
	userMsg := BuildUserMessageEvent(seq, topic, worldState, inbox)
	sa.cw.Push(ctxwin.RoleUser, userMsg)

	content, _, err := sa.agent.AskWithHistory(ctx, sa.cw, userMsg)
	if err != nil {
		return nil, fmt.Errorf("agent %s seq %d: %w", sa.persona.Name, seq, err)
	}

	msgType, proposals := parseResponse(content)
	for _, prop := range proposals {
		worldState.Set(prop.key, prop.value, sa.personaID, seq)
	}

	return &RoundMessage{
		AgentID:   sa.personaID,
		AgentName: sa.persona.Name,
		Content:   content,
		To:        "*",
		Type:      msgType,
		Round:     seq,
		SeqNum:    seq,
	}, nil
}

// parseResponse extracts message type and [PROPOSE key: value] directives.
// Deprecated shim: uses ParseActions for GA compatibility but preserves old
// type classification behavior for backward compatibility.
func parseResponse(content string) (msgType string, proposals []proposal) {
	actions, props := ParseActions(content)
	proposals = props

	msgType = classifyMessageType(content, actions)
	return msgType, proposals
}

// classifyMessageType determines the message type from content and parsed actions.
func classifyMessageType(content string, actions []Action) string {
	lines := strings.Split(content, "\n")
	hasRebuttal := false
	hasQuestion := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@") {
			hasRebuttal = true
		}
		if strings.HasSuffix(trimmed, "?") {
			hasQuestion = true
		}
	}

	// Check action types for additional signals
	for _, a := range actions {
		switch a.Type {
		case ActionSpeak:
			if a.Target != "*" {
				return "private_speak"
			}
		}
	}

	if hasRebuttal {
		return "rebuttal"
	}
	if hasQuestion {
		return "question"
	}
	return "statement"
}
