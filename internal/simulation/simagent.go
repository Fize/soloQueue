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
// and an AgentMemory that retains full history for report analysis.
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
// The CW should already contain the system prompt (pushed during agent creation).
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

// AskForRound builds the round-specific user message, pushes it to CW,
// calls AskWithHistory, records to AgentMemory, and returns a RoundMessage.
func (sa *SimAgent) AskForRound(ctx context.Context, round int, topic string, worldState *WorldState) (*RoundMessage, error) {
	// 1. Drain pending messages from the bus
	msgs := sa.bus.DrainAll(sa.personaID)

	// 2. Snapshot current world state
	wsSnap := worldState.Snapshot()

	// 3. Build the user message (variable part only — system prompt is static in CW)
	userMsg := BuildUserMessage(round, topic, worldState, msgs)

	// 4. Push user message to CW
	sa.cw.Push(ctxwin.RoleUser, userMsg)

	// 5. Record the incoming prompt into AgentMemory
	sa.memory.Record(MemoryRecord{
		Round:        round,
		Role:         "user",
		Content:      userMsg,
		WorldState:   wsSnap,
		ReceivedMsgs: sliceToMessages(msgs),
		Timestamp:    time.Now(),
	})

	// 6. Call AskWithHistory with timeout
	askCtx, cancel := context.WithTimeout(ctx, sa.timeout)
	defer cancel()

	content, reasoning, err := sa.agent.AskWithHistory(askCtx, sa.cw, userMsg)
	if err != nil {
		if sa.log != nil {
			sa.log.WarnContext(ctx, logger.CatSimulation, "simagent: ask with history failed",
				"persona_id", sa.personaID,
				"round", round,
				"err", err.Error(),
			)
		}
		return nil, fmt.Errorf("agent %s round %d: %w", sa.persona.Name, round, err)
	}

	// 7. Record the response into AgentMemory
	sa.memory.Record(MemoryRecord{
		Round:      round,
		Role:       "assistant",
		Content:    content,
		WorldState: wsSnap,
		Timestamp:  time.Now(),
	})

	// 8. Parse response to extract message type and [PROPOSE] directives
	msgType, proposals := parseResponse(content)

	// 9. Apply proposals to world state
	for _, prop := range proposals {
		worldState.Set(prop.key, prop.value, sa.personaID, round)
	}

	// 10. Build and broadcast the round message
	rm := &RoundMessage{
		AgentID:   sa.personaID,
		AgentName: sa.persona.Name,
		Content:   content,
		Reasoning: reasoning,
		To:        "*",
		Type:      msgType,
		Round:     round,
		SeqNum:    round,
	}

	sa.bus.Broadcast(sa.personaID, Message{
		From:    sa.personaID,
		To:      "*",
		Content: content,
		Round:   round,
		Type:    msgType,
	})

	return rm, nil
}

// AskForRoundEvent is like AskForRound but accepts pre-drained inbox messages.
// Used by EventLoop where message collection and broadcast are handled externally.
func (sa *SimAgent) AskForRoundEvent(ctx context.Context, seq int, topic string, worldState *WorldState, inbox []Message) (*RoundMessage, error) {
	// Snapshot current world state
	wsSnap := worldState.Snapshot()

	// Build user message with pre-collected inbox
	userMsg := BuildUserMessageEvent(seq, topic, worldState, inbox)

	// Push to CW
	sa.cw.Push(ctxwin.RoleUser, userMsg)

	// Record incoming prompt
	sa.memory.Record(MemoryRecord{
		Round:        seq,
		Role:         "user",
		Content:      userMsg,
		WorldState:   wsSnap,
		ReceivedMsgs: sliceToMessages(inbox),
		Timestamp:    time.Now(),
	})

	// Call LLM
	askCtx, cancel := context.WithTimeout(ctx, sa.timeout)
	defer cancel()

	content, reasoning, err := sa.agent.AskWithHistory(askCtx, sa.cw, userMsg)
	if err != nil {
		return nil, fmt.Errorf("agent %s seq %d: %w", sa.persona.Name, seq, err)
	}

	// Record response
	sa.memory.Record(MemoryRecord{
		Round:      seq,
		Role:       "assistant",
		Content:    content,
		WorldState: wsSnap,
		Timestamp:  time.Now(),
	})

	// Parse and apply proposals
	msgType, proposals := parseResponse(content)
	for _, prop := range proposals {
		worldState.Set(prop.key, prop.value, sa.personaID, seq)
	}

	return &RoundMessage{
		AgentID:   sa.personaID,
		AgentName: sa.persona.Name,
		Content:   content,
		Reasoning: reasoning,
		To:        "*",
		Type:      msgType,
		Round:     seq,
		SeqNum:    seq,
	}, nil
}

func (sa *SimAgent) Stop(timeout time.Duration) error {
	return sa.agent.Stop(timeout)
}

func (sa *SimAgent) InstanceID() string { return sa.instanceID }
func (sa *SimAgent) PersonaID() string  { return sa.personaID }
func (sa *SimAgent) Persona() *Persona  { return sa.persona }
func (sa *SimAgent) Memory() *AgentMemory { return sa.memory }

type proposal struct {
	key   string
	value string
}

// parseResponse extracts message type and [PROPOSE key: value] directives from agent output.
func parseResponse(content string) (msgType string, proposals []proposal) {
	msgType = "statement"

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect [PROPOSE key: value]
		if strings.HasPrefix(trimmed, "[PROPOSE ") && strings.HasSuffix(trimmed, "]") {
			inner := strings.TrimPrefix(trimmed, "[PROPOSE ")
			inner = strings.TrimSuffix(inner, "]")
			parts := strings.SplitN(inner, ":", 2)
			if len(parts) == 2 {
				proposals = append(proposals, proposal{
					key:   strings.TrimSpace(parts[0]),
					value: strings.TrimSpace(parts[1]),
				})
			}
			continue
		}

		// Detect message type from @mentions
		if strings.HasPrefix(trimmed, "@") {
			msgType = "rebuttal"
		}
		if strings.HasSuffix(trimmed, "?") {
			if msgType != "rebuttal" {
				msgType = "question"
			}
		}
	}
	return msgType, proposals
}

func personaTokenBudget(persona *Persona, contextWindow int) int {
	if contextWindow <= 0 {
		contextWindow = 128000
	}
	// Use 80% of context window as budget
	return contextWindow * 80 / 100
}

func sliceToMessages(msgs []Message) []Message {
	out := make([]Message, len(msgs))
	copy(out, msgs)
	return out
}
