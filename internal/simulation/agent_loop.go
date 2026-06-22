package simulation

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine"
)

// GAAgentLoop implements the Generative Agents decision loop:
// Perceive → Retrieve → Plan (if needed) → React → Execute → Record
//
// Each agent goroutine runs this loop, waking on clock ticks rather than
// message notifications, enabling autonomous behavior.
type GAAgentLoop struct {
	sa              *SimAgent
	env             *Environment
	bus             *MessageBus
	clock           *SimClock
	plan            *DailyPlan
	planGen         *PlanGenerator
	relationshipMgr *RelationshipManager
	memoryEngine    *memoryengine.Engine
	reflectionEng   *ReflectionEngine
	dialogueMgr     *DialogueManager
	worldState      *WorldState
	nameByID        map[string]string
	allPersonas     []Persona
	log             *logger.Logger
	language        string

	// Runtime state
	actionSeq         atomic.Int64
	stopCh            chan struct{}
	stopOnce          sync.Once
	ticksSinceLastReflection int
	lastActionTime    time.Time
	reflections       []ReflectionRecord
	reflectionsMu     sync.Mutex
	busy              atomic.Int32 // 1 when LLM call is in progress

	// Output
	events chan SimulationEvent
}

// NewGAAgentLoop creates a Generative-Agents-style agent loop.
func NewGAAgentLoop(
	sa *SimAgent,
	env *Environment,
	bus *MessageBus,
	clock *SimClock,
	planGen *PlanGenerator,
	relationshipMgr *RelationshipManager,
	memEngine *memoryengine.Engine,
	reflectionEng *ReflectionEngine,
	dialogueMgr *DialogueManager,
	worldState *WorldState,
	nameByID map[string]string,
	allPersonas []Persona,
	log *logger.Logger,
	language string,
) *GAAgentLoop {
	return &GAAgentLoop{
		sa:                sa,
		env:               env,
		bus:               bus,
		clock:             clock,
		planGen:           planGen,
		relationshipMgr:   relationshipMgr,
		memoryEngine:      memEngine,
		reflectionEng:     reflectionEng,
		dialogueMgr:       dialogueMgr,
		worldState:        worldState,
		nameByID:          nameByID,
		allPersonas:       allPersonas,
		log:               log,
		language:          language,
		stopCh:            make(chan struct{}),
		events:            make(chan SimulationEvent, 64),
	}
}

// Events returns the event channel for this agent.
func (gal *GAAgentLoop) Events() <-chan SimulationEvent {
	return gal.events
}

// Stop signals this agent loop to terminate.
func (gal *GAAgentLoop) Stop() {
	gal.stopOnce.Do(func() {
		close(gal.stopCh)
	})
}

// Run starts the agent loop. Should be called in a goroutine.
func (gal *GAAgentLoop) Run(ctx context.Context) {
	defer close(gal.events)
	defer func() {
		if r := recover(); r != nil {
			gal.emit(SimulationEvent{
				Type:  "error",
				Error: fmt.Sprintf("agent %s panic: %v", gal.sa.PersonaID(), r),
			})
		}
	}()

	timeCh := make(chan SimTimeEvent, 16)
	gal.clock.Subscribe(timeCh)
	defer gal.clock.Unsubscribe(timeCh)

	gal.emit(SimulationEvent{
		Type: "agent_start",
		Data: map[string]string{
			"agent_id":     gal.sa.PersonaID(),
			"agent_name":   safePersonaName(gal.sa.Persona()),
			"current_zone": gal.env.GetAgentZone(gal.sa.PersonaID()),
		},
	})

	for {
		select {
		case <-ctx.Done():
			return
		case <-gal.stopCh:
			return
		case timeEvt := <-timeCh:
			if !gal.busy.CompareAndSwap(0, 1) {
				// Still processing previous tick — log and skip to avoid
				// accumulating backpressure. The agent will catch the next tick.
				if gal.log != nil {
					gal.log.WarnContext(ctx, logger.CatSimulation, "ga_agent_loop: tick dropped due to busy agent",
						"agent_id", gal.sa.PersonaID())
				}
				continue
			}
			gal.tick(ctx, timeEvt)
			gal.busy.Store(0)
		}
	}
}

// tick executes one iteration of the Perceive→Retrieve→Plan→React loop.
func (gal *GAAgentLoop) tick(ctx context.Context, timeEvt SimTimeEvent) {
	personaID := gal.sa.PersonaID()
	persona := gal.sa.Persona()
	if persona == nil {
		return
	}

	// Limit memory growth: trim CW if too many messages
	if gal.sa.cw.Len() > 40 {
		if gal.log != nil {
			gal.log.InfoContext(ctx, logger.CatSimulation, "agent: trimming context window", "agent_id", personaID)
		}
		gal.sa.cw.Reset()
		// Re-push system prompt
		systemPrompt := BuildGenerativeAgentSystemPrompt(gal.language, *persona, gal.allPersonas, gal.env, gal.plan, gal.relationshipMgr, gal.reflections, gal.nameByID, gal.clock)
		gal.sa.cw.Push(ctxwin.RoleSystem, systemPrompt)
	}

	// Limit AgentMemory growth — use importance-weighted retention
	// so that significant observations survive pruning.
	memRecords := gal.sa.Memory().Records()
	if len(memRecords) > 500 {
		gal.sa.Memory().TruncateByImportance(300)
	}

	// Avoid acting on every tick — use a cooldown
	if !gal.lastActionTime.IsZero() {
		if time.Since(gal.lastActionTime) < 2*time.Second {
			return // too soon
		}
	}

	seq := int(gal.actionSeq.Add(1))

	// Tick-level memory logging (every 10th tick)
	if seq%10 == 0 {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		cwTokens := gal.sa.cw.CurrentTokens()
		memRecords := len(gal.sa.Memory().Records())
		if gal.log != nil {
			gal.log.InfoContext(ctx, logger.CatSimulation, "agent tick mem",
				"agent_id", personaID, "seq", seq,
				"cw_tokens", cwTokens,
				"cw_messages", gal.sa.cw.Len(),
				"memory_records", memRecords,
				"goroutines", runtime.NumGoroutine(),
				"heap_mb", m.HeapAlloc/1024/1024,
			)
		}
	}

	// ─── 1. PERCEIVE ────────────────────────────────────────────────
	observationCh := make(chan []Observation, 1)
	go func() {
		// Collect observations from environment
		ps := NewPerceptionSystem(gal.env, gal.bus, gal.clock)
		observationCh <- ps.CollectObservations(personaID, persona.Name)
	}()

	var observations []Observation
	select {
	case observations = <-observationCh:
	case <-time.After(5 * time.Second):
		return
	}

	if len(observations) == 0 {
		return
	}

	// Record observations to memory
	for _, obs := range observations {
		gal.sa.Memory().Record(ObservationToMemory(obs, personaID))
	}

	// ─── 2. RETRIEVE (Memory Search) ────────────────────────────────
	retrievedMemories := gal.retrieveRelevantMemories(ctx, observations)

	// ─── 3. DECIDE (Ask LLM) ─────────────────────────────────────────
	cw := gal.sa.cw
	userMsg := BuildTickUserMessage(seq, observations, gal.worldState, retrievedMemories, gal.plan, gal.clock, gal.language)

	cw.Push(ctxwin.RoleUser, userMsg)

	askCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	content, _, err := gal.sa.agent.AskWithHistory(askCtx, cw, userMsg)
	cancel()
	if err != nil {
		if gal.log != nil {
			gal.log.WarnContext(ctx, logger.CatSimulation, "ga_agent_loop: LLM ask failed",
				"agent_id", personaID, "seq", seq, "err", err.Error())
		}
		gal.emit(SimulationEvent{Type: "error", Round: seq, Error: fmt.Sprintf("%s: %s", personaID, err.Error())})
		return
	}

	// Record the response
	gal.sa.Memory().Record(MemoryRecord{
		Round:         seq,
		Role:          "assistant",
		Content:       content,
		WorldState:    gal.worldState.Snapshot(),
		SimulatedTime: timeEvt.SimTime,
		RecordType:    "action",
		Timestamp:     time.Now(),
	})

	// ─── 4. PARSE ACTIONS ────────────────────────────────────────────
	actions, proposals := ParseActions(content)

	if len(actions) == 0 && len(proposals) == 0 {
		gal.lastActionTime = time.Now()
		return
	}

	// ─── 5. EXECUTE ──────────────────────────────────────────────────
	for _, action := range actions {
		gal.executeAction(ctx, action, personaID, persona.Name, seq, timeEvt)
	}

	for _, prop := range proposals {
		gal.worldState.Set(prop.key, prop.value, personaID, seq)
	}

	// Update relationships from any RELATION directives
	relUpdates := ParseRelationshipUpdate(personaID, content)
	for _, ru := range relUpdates {
		gal.relationshipMgr.SetWithKind(personaID, ru.TargetID, ru.Kind, ru.Familiarity, ru.Affinity, ru.Tags)
		gal.emit(SimulationEvent{
			Type:  "relationship_update",
			Round: seq,
			Data: map[string]any{
				"subject_id":   personaID,
				"target_id":    ru.TargetID,
				"kind":         string(ru.Kind),
				"familiarity":  ru.Familiarity,
				"affinity":     ru.Affinity,
				"tags":         ru.Tags,
			},
		})
	}

	gal.lastActionTime = time.Now()

	// ─── 6. REFLECTION CHECK ─────────────────────────────────────────
	gal.ticksSinceLastReflection++
	if gal.reflectionEng != nil && gal.reflectionEng.ShouldReflect(gal.ticksSinceLastReflection) {
		gal.runReflection(ctx)
		gal.ticksSinceLastReflection = 0
	}
}

func (gal *GAAgentLoop) retrieveRelevantMemories(ctx context.Context, observations []Observation) string {
	if gal.memoryEngine == nil {
		return ""
	}

	var currentPlan *PlanItem
	if gal.plan != nil {
		currentPlan = gal.plan.GetCurrentActivity(gal.clock.Now())
	}

	query := BuildRetrievalQuery(gal.sa.Persona(), observations, currentPlan, gal.clock)
	if query == "" {
		return ""
	}

	results, err := gal.memoryEngine.Search(ctx, memoryengine.SearchQuery{
		Text:  query,
		Limit: 5,
	})
	if err != nil {
		return ""
	}

	if len(results.Results) == 0 {
		return ""
	}

	var result string
	result = "Here are some relevant past experiences:\n"
	for i, r := range results.Results {
		if i >= 3 {
			break
		}
		content := r.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		result += fmt.Sprintf("- %s\n", content)
	}
	return result
}

func (gal *GAAgentLoop) executeAction(ctx context.Context, action Action, personaID, personaName string, seq int, timeEvt SimTimeEvent) {
	switch action.Type {
	case ActionSpeak:
		rm := &RoundMessage{
			AgentID:   personaID,
			AgentName: personaName,
			Content:   action.Content,
			To:        action.Target,
			Type:      "speak",
			SeqNum:    seq,
		}

		if action.Target != "*" {
			// Private message via DialogueManager
			partner := gal.dialogueMgr.GetDialoguePartner(personaID)
			if partner == "" {
				// Initiate new dialogue
				if err := gal.dialogueMgr.Request(personaID, action.Target); err != nil {
					// Fall back to sending via bus
					gal.sendMessage(personaID, action.Target, action.Content)
				}
			}
			// Send the private message
			if err := gal.dialogueMgr.SendMessage(personaID, action.Content); err != nil {
				gal.sendMessage(personaID, action.Target, action.Content)
			}
			rm.Type = "private_speak"
		} else {
			// Broadcast to all agents in the same zone
			zone := gal.env.GetAgentZone(personaID)
			for _, agentID := range gal.env.GetAgentsInZone(zone) {
				if agentID != personaID {
					gal.sendMessage(personaID, agentID, action.Content)
				}
			}
		}

		gal.emit(SimulationEvent{Type: "agent_message", Round: seq, Data: rm})

		// Boost familiarity with whoever was spoken to
		if action.Target != "*" {
			gal.relationshipMgr.BoostFamiliarity(personaID, action.Target)
		} else {
			zone := gal.env.GetAgentZone(personaID)
			for _, agentID := range gal.env.GetAgentsInZone(zone) {
				if agentID != personaID {
					gal.relationshipMgr.BoostFamiliarity(personaID, agentID)
				}
			}
		}

	case ActionMove:
		obs, err := gal.env.MoveAgent(personaID, action.Target)
		if err != nil {
			gal.emit(SimulationEvent{Type: "error", Round: seq, Error: fmt.Sprintf("%s: move failed: %s", personaID, err.Error())})
			return
		}
		for _, o := range obs {
			gal.sa.Memory().Record(MemoryRecord{
				Round:         seq,
				Role:          "observation",
				Content:       o.Content,
				RecordType:    "agent_move",
				Source:        o.Source,
				Location:      action.Target,
				SimulatedTime: timeEvt.SimTime,
				Timestamp:     time.Now(),
			})
		}
		gal.emit(SimulationEvent{
			Type:  "agent_move",
			Round: seq,
			Data: map[string]string{
				"agent_id": personaID,
				"to_zone":  action.Target,
			},
		})

	case ActionInteract:
		detail, err := gal.env.Interact(personaID, action.Target, action.Content)
		if err != nil {
			gal.emit(SimulationEvent{Type: "error", Round: seq, Error: fmt.Sprintf("%s: interact failed: %s", personaID, err.Error())})
			return
		}
		gal.sa.Memory().Record(MemoryRecord{
			Round:         seq,
			Role:          "action",
			Content:       detail,
			RecordType:    "interact",
			SimulatedTime: timeEvt.SimTime,
			Timestamp:     time.Now(),
		})
		gal.emit(SimulationEvent{
			Type:  "agent_interact",
			Round: seq,
			Data:  map[string]string{"detail": detail},
		})

	case ActionWait, ActionPass:
		// Nothing to execute, agent chose to do nothing

	case ActionSpawn:
		// Request the simulation engine to spawn a new agent.
		gal.emit(SimulationEvent{
			Type:  "agent_spawn",
			Round: seq,
			Data: &SpawnInfo{
				Name:        action.Target,
				Description: action.Content,
				RequestedBy: personaID,
			},
		})
		gal.sa.Memory().Record(MemoryRecord{
			Round:      seq,
			Role:       "action",
			Content:    fmt.Sprintf("Requested spawn of new agent: %s — %s", action.Target, action.Content),
			RecordType: "spawn_request",
			Timestamp:  time.Now(),
		})

	case ActionDie:
		// Agent voluntarily exits the simulation.
		gal.emit(SimulationEvent{
			Type:  "agent_message",
			Round: seq,
			Data: &RoundMessage{
				AgentID:   personaID,
				AgentName: personaName,
				Content:   fmt.Sprintf("%s has left the simulation.", personaName),
				To:        "*",
				Type:      "agent_exit",
				SeqNum:    seq,
			},
		})
		gal.emit(SimulationEvent{
			Type:  "agent_death",
			Round: seq,
			Data:  map[string]string{"agent_id": personaID, "agent_name": personaName},
		})
	}
}

func (gal *GAAgentLoop) sendMessage(from, to, content string) {
	gal.bus.Send(to, Message{
		From:    from,
		To:      to,
		Content: content,
		Type:    "speak",
	})
}

func (gal *GAAgentLoop) runReflection(ctx context.Context) {
	persona := gal.sa.Persona()
	record, err := gal.reflectionEng.Reflect(ctx, persona, gal.sa.Memory(), gal.clock)
	if err != nil {
		if gal.log != nil {
			gal.log.WarnContext(ctx, logger.CatSimulation, "reflection failed", "agent_id", persona.ID, "err", err.Error())
		}
		return
	}
	if record == nil {
		return
	}

	// Store the reflection in AgentMemory
	gal.sa.Memory().Record(MemoryRecord{
		Role:          "reflection",
		Content:       record.Content,
		RecordType:    "reflection",
		Importance:    record.Importance,
		SimulatedTime: record.GeneratedAt,
		Timestamp:     time.Now(),
	})

	// Keep recent reflections for prompt injection (max 5)
	gal.reflectionsMu.Lock()
	gal.reflections = append(gal.reflections, *record)
	if len(gal.reflections) > 5 {
		gal.reflections = gal.reflections[1:]
	}
	gal.reflectionsMu.Unlock()

	// Also save to MemoryEngine for cross-simulation recall
	if gal.memoryEngine != nil {
		_, _, err := gal.memoryEngine.Save(ctx, record.Content, record.GeneratedAt.Format("2006-01-02"), "reflection", record.GeneratedAt.Format(time.RFC3339))
		if err != nil && gal.log != nil {
			gal.log.WarnContext(ctx, logger.CatSimulation, "failed to save reflection to memory engine", "agent_id", persona.ID, "err", err.Error())
		}
	}

	gal.emit(SimulationEvent{
		Type: "agent_reflection",
		Data: map[string]string{
			"agent_id": persona.ID,
			"content":  record.Content,
		},
	})
}

func (gal *GAAgentLoop) emit(ev SimulationEvent) {
	ev.Timestamp = time.Now()
	select {
	case gal.events <- ev:
	default:
	}
}

func safePersonaName(p *Persona) string {
	if p == nil {
		return "unknown"
	}
	return p.Name
}
