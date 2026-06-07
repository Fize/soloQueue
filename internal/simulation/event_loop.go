package simulation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// EventLoop runs async goroutine-per-agent execution.
// Agents wake on message notification (not polling) and compete for
// a semaphore-controlled LLM pool.
type EventLoop struct {
	agents     []*SimAgent
	bus        *MessageBus
	worldState *WorldState
	graph      *RelationGraph
	topic      string
	trigger    TriggerPolicy
	timeout    time.Duration
	maxActions int
	poolSize   int // max concurrent LLM calls
	log        *logger.Logger

	events    chan SimulationEvent
	actionSeq atomic.Int64
	stopCh    chan struct{}
	stopOnce  sync.Once
	agentWg   sync.WaitGroup
	sem       chan struct{} // semaphore for LLM concurrency
}

// NewEventLoop creates an event-driven execution loop.
func NewEventLoop(
	agents []*SimAgent,
	bus *MessageBus,
	ws *WorldState,
	graph *RelationGraph,
	topic string,
	trigger TriggerPolicy,
	timeout time.Duration,
	maxActions int,
	poolSize int,
	log *logger.Logger,
) *EventLoop {
	if poolSize <= 0 {
		poolSize = 20 // default max concurrent LLM calls
	}

	return &EventLoop{
		agents:     agents,
		bus:        bus,
		worldState: ws,
		graph:      graph,
		topic:      topic,
		trigger:    trigger,
		timeout:    timeout,
		maxActions: maxActions,
		poolSize:   poolSize,
		log:        log,
		events:     make(chan SimulationEvent, 64),
		stopCh:     make(chan struct{}),
		sem:        make(chan struct{}, poolSize),
	}
}

// Run starts the event loop and returns an event channel. Closes when simulation ends.
func (el *EventLoop) Run(ctx context.Context) <-chan SimulationEvent {
	ctx, cancel := context.WithTimeout(ctx, el.timeout)

	go func() {
		defer close(el.events)
		defer cancel()

		// Seed: wake all agents with a system message
		el.broadcastSeed(ctx)

		// Register graph nodes
		for _, sa := range el.agents {
			el.graph.AddNode(sa.PersonaID())
		}

		// Launch agent goroutines (blocks on notifyCh, not polling)
		for _, sa := range el.agents {
			sa := sa
			el.agentWg.Add(1)
			go func() {
				defer el.agentWg.Done()
				el.agentLoop(ctx, sa)
			}()
		}

		go el.monitor(ctx)
		el.agentWg.Wait()

		el.emit(SimulationEvent{
			Type: "simulation_end",
			Data: map[string]any{
				"total_actions": el.actionSeq.Load(),
				"graph_nodes":   el.graph.NodeCount(),
			},
		})
	}()

	return el.events
}

// Stop signals termination.
func (el *EventLoop) Stop() {
	el.stopOnce.Do(func() {
		close(el.stopCh)
	})
}

func (el *EventLoop) broadcastSeed(ctx context.Context) {
	for _, sa := range el.agents {
		el.bus.Send(sa.PersonaID(), Message{
			From:    "system",
			To:      sa.PersonaID(),
			Content: fmt.Sprintf("Discussion started. Topic: %s. State your position.", el.topic),
			Round:   0,
			Type:    "system",
		})
	}
}

// agentLoop blocks on the notification channel, waking only when messages arrive.
func (el *EventLoop) agentLoop(ctx context.Context, sa *SimAgent) {
	notifyCh := el.bus.NotifyCh(sa.PersonaID())
	if notifyCh == nil {
		return
	}

	var lastSpokeAt time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-el.stopCh:
			return
		case _, ok := <-notifyCh:
			if !ok {
				return
			}

			if el.maxActions > 0 && el.actionSeq.Load() >= int64(el.maxActions) {
				return
			}

			// Drain inbox
			inbox := el.bus.DrainAll(sa.PersonaID())
			if len(inbox) == 0 {
				continue
			}

			wsSnap := el.worldState.Snapshot()

			if !el.trigger.ShouldSpeak(sa.PersonaID(), inbox, wsSnap, lastSpokeAt) {
				continue
			}

			// Acquire semaphore slot before LLM call
			select {
			case el.sem <- struct{}{}:
			case <-ctx.Done():
				return
			case <-el.stopCh:
				return
			}

			seq := int(el.actionSeq.Add(1))
			rm, err := sa.AskForRoundEvent(ctx, seq, el.topic, el.worldState, inbox)
			<-el.sem // release

			if err != nil {
				if el.log != nil {
					el.log.WarnContext(ctx, logger.CatApp, "event_loop: agent ask failed",
						"agent_id", sa.PersonaID(), "seq", seq, "err", err.Error())
				}
				el.emit(SimulationEvent{Type: "error", Round: seq, Error: fmt.Sprintf("%s: %s", sa.PersonaID(), err.Error())})
				continue
			}

			lastSpokeAt = time.Now()

			// Feed the relationship graph
			el.feedGraph(sa.PersonaID(), inbox, rm, seq)

			el.emit(SimulationEvent{Type: "agent_message", Round: seq, Data: rm})

			el.bus.Broadcast(sa.PersonaID(), Message{
				From:    sa.PersonaID(),
				To:      "*",
				Content: rm.Content,
				Round:   seq,
				Type:    rm.Type,
			})

			if el.maxActions > 0 && el.actionSeq.Load() >= int64(el.maxActions) {
				return
			}
		}
	}
}

// feedGraph extracts relationships from the agent's response and adds edges to the graph.
func (el *EventLoop) feedGraph(agentID string, inbox []Message, rm *RoundMessage, seq int) {
	// Extract mentions from the response content
	for _, msg := range inbox {
		if msg.From == "system" {
			continue
		}

		// Agent mentioned someone in their response
		if containsMention(rm.Content, msg.From) {
			relType := classifyRelation(msg, rm)
			el.graph.AddEdge(agentID, msg.From, relType, seq, rm.Content)
		}
	}

	// Also check for @mentions not in inbox (spontaneous mentions)
	mentioned := extractMentions(rm.Content)
	for _, target := range mentioned {
		if target != agentID {
			el.graph.AddEdge(agentID, target, RelMention, seq, rm.Content)
		}
	}
}

func containsMention(content, agentID string) bool {
	return containsWord(content, "@"+agentID) || containsWord(content, agentID)
}

func containsWord(s, word string) bool {
	for i := 0; i <= len(s)-len(word); i++ {
		if s[i:i+len(word)] == word {
			return true
		}
	}
	return false
}

func extractMentions(content string) []string {
	var mentions []string
	seen := make(map[string]bool)

	for i := 0; i < len(content); i++ {
		if content[i] == '@' {
			end := i + 1
			for end < len(content) && isAlphaNum(rune(content[end])) {
				end++
			}
			if end > i+1 {
				name := content[i+1 : end]
				if !seen[name] {
					mentions = append(mentions, name)
					seen[name] = true
				}
			}
		}
	}
	return mentions
}

func isAlphaNum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
}

func classifyRelation(inMsg Message, response *RoundMessage) RelationType {
	content := strings.ToLower(response.Content)
	fromName := strings.ToLower(inMsg.From)

	// Check for rebuttal patterns first (before "agree" to avoid substring matches)
	if containsAnyWord(content, "disagree", "i disagree", "however", "on the contrary", "i don't think", "that's not", "you're wrong", "incorrect", "but") {
		return RelRebuttal
	}

	// Check for agreement patterns
	if containsAnyWord(content, "i agree", "i concur", "good point", "you're right", "well said", "exactly", "i support") {
		return RelAgree
	}

	// If the response addresses someone specifically
	if containsMention(content, fromName) {
		return RelReply
	}

	return RelMention
}

func containsAnyWord(s string, patterns ...string) bool {
	lower := strings.ToLower(s)
	for _, p := range patterns {
		if matchWord(lower, p) {
			return true
		}
	}
	return false
}

// matchWord checks if pattern appears as a word (preceded/followed by non-letter).
func matchWord(s, pattern string) bool {
	idx := 0
	for {
		i := strings.Index(s[idx:], pattern)
		if i < 0 {
			return false
		}
		pos := idx + i
		// Check left boundary
		if pos > 0 && isAlphaNum(rune(s[pos-1])) {
			idx = pos + 1
			continue
		}
		// Check right boundary
		end := pos + len(pattern)
		if end < len(s) && isAlphaNum(rune(s[end])) {
			idx = pos + 1
			continue
		}
		return true
	}
}

func (el *EventLoop) monitor(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-el.stopCh:
	}
	el.Stop()
}

func (el *EventLoop) emit(ev SimulationEvent) {
	ev.Timestamp = time.Now()
	select {
	case el.events <- ev:
	default:
	}
}
