package simulation

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine"
)

// GAAgentLoop implements the Generative Agents decision loop:
// Perceive → Retrieve → Plan (if needed) → React → Execute → Record
//
// Unlike the previous goroutine-per-agent design with a decoupled clock,
// ProcessRound() is called synchronously by the engine's barrier loop.
// All active agents process each round in parallel via goroutines launched
// by the barrier, then the engine drains events before advancing time.
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
	graph           *RelationGraph  // interaction graph for edge tracking

	// Runtime state
	actionSeq              int64
	stopCh                 chan struct{}
	stopOnce               sync.Once
	startOnce              sync.Once // emits agent_start event on first ProcessRound
	ticksSinceLastReflection int
	reflections            []ReflectionRecord
	reflectionsMu          sync.Mutex

	// Output
	events chan SimulationEvent

	activeIntention string
	activeDirective string
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
	graph *RelationGraph,
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
		graph:             graph,
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

// ProcessRound processes one round of the Perceive→Retrieve→Decide→Execute→Reflect loop.
// Called by the engine's barrier loop. This replaces the old Run() goroutine pattern.
//
// Unlike the old tick() which could skip ticks when busy, ProcessRound is guaranteed
// to be called exactly once per round by the barrier. No ticks are ever dropped.
// No action cooldown is needed because the barrier itself paces the simulation.
func (gal *GAAgentLoop) ProcessRound(ctx context.Context, round int, timeEvt SimTimeEvent) {
	// Check if this loop has been stopped (e.g., via agent death)
	select {
	case <-gal.stopCh:
		return
	default:
	}

	gal.activeIntention = ""
	gal.activeDirective = ""

	personaID := gal.sa.PersonaID()
	persona := gal.sa.Persona()
	if persona == nil {
		return
	}

	// Emit agent_start on first invocation
	gal.startOnce.Do(func() {
		gal.emit(SimulationEvent{
			Type: "agent_start",
			Data: map[string]string{
				"agent_id":     personaID,
				"agent_name":   safePersonaName(persona),
				"current_zone": gal.env.GetAgentZone(personaID),
			},
		})
	})

	// Limit memory growth: trim CW if too many messages
	if gal.sa.cw.Len() > 40 {
		if gal.log != nil {
			gal.log.InfoContext(ctx, logger.CatSimulation, "agent: trimming context window", "agent_id", personaID)
		}
		gal.sa.cw.Reset()
		systemPrompt := BuildGenerativeAgentSystemPrompt(gal.language, *persona, gal.allPersonas, gal.env, gal.plan, gal.relationshipMgr, gal.reflections, gal.nameByID, gal.clock, gal.worldState.Snapshot())
		gal.sa.cw.Push(ctxwin.RoleSystem, systemPrompt)
	}

	// Limit AgentMemory growth
	memRecords := gal.sa.Memory().Records()
	if len(memRecords) > 500 {
		gal.sa.Memory().TruncateByImportance(300)
	}

	// Increment action sequence (per-agent counter for reference)
	gal.actionSeq++
	seq := int(gal.actionSeq)

	// Log memory stats every 10 rounds
	if seq%10 == 0 {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		if gal.log != nil {
			gal.log.InfoContext(ctx, logger.CatSimulation, "agent round mem",
				"agent_id", personaID, "round", round, "seq", seq,
				"cw_tokens", gal.sa.cw.CurrentTokens(),
				"cw_messages", gal.sa.cw.Len(),
				"memory_records", len(gal.sa.Memory().Records()),
				"goroutines", runtime.NumGoroutine(),
				"heap_mb", m.HeapAlloc/1024/1024,
			)
		}
	}

	// ─── Plan Follower Gate ──────────────────────────────────────────
	// If the agent is on a routine plan with no external stimuli, skip
	// LLM entirely and auto-continue the current activity.
	// This reduces LLM calls by 80-90% during routine periods.
	if gal.shouldAutoPass() {
		gal.activeDirective = "PASS"
		gal.autoPassMemory(timeEvt)
		gal.checkAndRunReflection(ctx)
		return
	}

	// ─── 1. PERCEIVE ────────────────────────────────────────────────
	ps := NewPerceptionSystem(gal.env, gal.bus, gal.clock)
	observations := ps.CollectObservations(personaID, persona.Name)

	// Perform perception checks for hidden agents in the zone, and boost familiarity for visible ones
	currentZone := gal.env.GetAgentZone(personaID)
	zoneAgents := gal.env.GetAgentsInZone(currentZone)
	observerPerception := getAgentPerception(persona)

	var discovered []Observation
	for _, otherID := range zoneAgents {
		if otherID == personaID {
			continue
		}
		if gal.env.IsAgentHidden(otherID) {
			otherPersona := gal.findPersonaByID(otherID)
			hiddenStealth := getAgentStealth(otherPersona)
			pDiscover := 0.4 + (observerPerception-hiddenStealth)/200.0
			if pDiscover < 0.1 {
				pDiscover = 0.1
			}
			if pDiscover > 0.9 {
				pDiscover = 0.9
			}
			if rand.Float64() < pDiscover {
				discObs := Observation{
					Type:       "agent_present",
					Content:    fmt.Sprintf("你凭着敏锐的洞察力，发现了躲在暗处的 %s。", otherPersona.Name),
					Source:     otherID,
					Importance: 5,
					At:         gal.clock.Now(),
				}
				discovered = append(discovered, discObs)
				gal.relationshipMgr.BoostFamiliarity(personaID, otherID)
			}
		} else {
			// Seen visible agent -> boost familiarity automatically
			gal.relationshipMgr.BoostFamiliarity(personaID, otherID)
		}
	}
	observations = append(observations, discovered...)

	if len(observations) == 0 {
		// Nothing to perceive this round — still check reflection
		gal.activeDirective = "PASS"
		gal.checkAndRunReflection(ctx)
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
	userMsg := BuildTickUserMessage(round, observations, gal.worldState, retrievedMemories, gal.plan, gal.clock, gal.language)

	cw.Push(ctxwin.RoleUser, userMsg)

	askCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	content, _, err := gal.sa.agent.AskWithHistory(askCtx, cw, userMsg)
	cancel()
	if err != nil {
		if gal.log != nil {
			gal.log.WarnContext(ctx, logger.CatSimulation, "ga_agent_loop: LLM ask failed",
				"agent_id", personaID, "round", round, "err", err.Error())
		}
		gal.emit(SimulationEvent{Type: "error", Round: round, Error: fmt.Sprintf("%s: %s", personaID, err.Error())})
		return
	}

	gal.activeIntention = parseReasoning(content)

	// Record the response
	gal.sa.Memory().Record(MemoryRecord{
		Round:         round,
		Role:          "assistant",
		Content:       content,
		WorldState:    gal.worldState.Snapshot(),
		SimulatedTime: timeEvt.SimTime,
		RecordType:    "action",
		Timestamp:     time.Now(),
	})

	// ─── 4. PARSE ACTIONS ────────────────────────────────────────────
	actions, proposals := ParseActions(content)

	// ─── 5. EXECUTE ──────────────────────────────────────────────────
	// Check if B has a pending conflict
	initiatorID, hasConflict := gal.env.GetConflictInitiator(personaID)
	if hasConflict {
		// Clear conflict state immediately to avoid double processing
		gal.env.ClearConflict(personaID)

		isSneak := false
		if strings.HasPrefix(initiatorID, "sneak:") {
			initiatorID = strings.TrimPrefix(initiatorID, "sneak:")
			isSneak = true
		}

		initiatorPersona := gal.findPersonaByID(initiatorID)

		// Determine B's action
		var bAction *Action
		for i := range actions {
			if actions[i].Type == ActionMove || actions[i].Type == ActionConflict || actions[i].Type == ActionSpeak || actions[i].Type == ActionPass {
				bAction = &actions[i]
				break
			}
		}

		if bAction == nil {
			bAction = &Action{Type: ActionPass}
		}

		// Execute conflict resolution
		gal.resolveConflictState(ctx, initiatorID, initiatorPersona.Name, personaID, persona.Name, bAction, isSneak, round, timeEvt)

		// Filter out B's movement/conflict actions as they are resolved
		var filteredActions []Action
		for _, act := range actions {
			if act.Type != ActionMove && act.Type != ActionConflict {
				filteredActions = append(filteredActions, act)
			}
		}
		actions = filteredActions
	}

	if len(actions) == 0 && len(proposals) == 0 {
		gal.activeDirective = "PASS"
		gal.checkAndRunReflection(ctx)
		return
	}

	for _, action := range actions {
		gal.executeAction(ctx, action, personaID, persona.Name, round, timeEvt)
	}

	for _, prop := range proposals {
		gal.worldState.Set(prop.key, prop.value, personaID, round)
	}

	// Update relationships from any RELATION directives
	relUpdates := ParseRelationshipUpdate(personaID, content)
	for _, ru := range relUpdates {
		gal.relationshipMgr.SetWithKind(personaID, ru.TargetID, ru.Kind, ru.Familiarity, ru.Affinity, ru.Tags)
		gal.emit(SimulationEvent{
			Type:  "relationship_update",
			Round: round,
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

	// ─── 6. REFLECTION CHECK ─────────────────────────────────────────
	gal.checkAndRunReflection(ctx)
}

// checkAndRunReflection runs a reflection cycle if enough rounds have passed.
func (gal *GAAgentLoop) checkAndRunReflection(ctx context.Context) {
	gal.ticksSinceLastReflection++
	if gal.reflectionEng != nil && gal.reflectionEng.ShouldReflect(gal.ticksSinceLastReflection) {
		gal.runReflection(ctx)
		gal.ticksSinceLastReflection = 0
	}
}

// ─── Plan Follower Gate Helpers ─────────────────────────────────────────────

// shouldAutoPass determines if this agent can safely skip LLM inference
// and automatically continue following their routine plan.
// Returns true when ALL conditions are met:
//  1. Agent has a current plan activity
//  2. Not near a plan transition (within 1.5x stepSize of next activity)
//  3. No pending messages in bus
//  4. No other agents in the same zone
//  5. No active conflict involving this agent
func (gal *GAAgentLoop) shouldAutoPass() bool {
	// 1. Must have an active plan and current activity
	if gal.plan == nil {
		return false
	}
	now := gal.clock.Now()
	current := gal.plan.GetCurrentActivity(now)
	if current == nil {
		return false
	}

	// 2. Must not be near a plan transition
	stepSize := gal.clock.StepSize()
	if isNearPlanTransition(gal.plan, now, stepSize) {
		return false
	}

	// 3. No pending messages
	if gal.bus.HasMessages(gal.sa.PersonaID()) {
		return false
	}

	// 4. No other agents in the same zone
	myZone := gal.env.GetAgentZone(gal.sa.PersonaID())
	others := gal.env.GetAgentsInZone(myZone)
	if len(others) > 1 {
		return false
	}

	// 5. No active conflict targeting this agent
	if _, hasConflict := gal.env.GetConflictInitiator(gal.sa.PersonaID()); hasConflict {
		return false
	}

	return true
}

// isNearPlanTransition checks if the current simulated time is within
// 1.5x the current stepSize of the current activity's end time.
// When near a transition, we want LLM involvement to decide how to transition.
func isNearPlanTransition(plan *DailyPlan, now time.Time, stepSize time.Duration) bool {
	current := plan.GetCurrentActivity(now)
	if current == nil {
		return false
	}
	timeUntilEnd := current.EndTime.Sub(now)
	// Near transition if time remaining is less than 1.5 step sizes
	return timeUntilEnd > 0 && timeUntilEnd < stepSize*3/2
}

// autoPassMemory records a routine continuation in memory without an LLM call.
// This preserves continuity in the agent's memory stream even when skipping ticks.
func (gal *GAAgentLoop) autoPassMemory(timeEvt SimTimeEvent) {
	current := gal.plan.GetCurrentActivity(gal.clock.Now())
	if current == nil {
		return
	}

	// Build a natural continuation message based on current activity type
	content := fmt.Sprintf("继续在 %s %s。", current.Location, current.Activity)

	gal.sa.Memory().Record(MemoryRecord{
		Round:         int(gal.actionSeq),
		Role:          "observation",
		Content:       content,
		RecordType:    "auto_pass",
		SimulatedTime: timeEvt.SimTime,
		Timestamp:     time.Now(),
	})
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
		gal.activeDirective = fmt.Sprintf("SPEAK to %s: %s", action.Target, action.Content)
		rm := &RoundMessage{
			AgentID:   personaID,
			AgentName: personaName,
			Content:   action.Content,
			To:        action.Target,
			Type:      "speak",
			SeqNum:    seq,
			Reasoning: gal.activeIntention,
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
			if gal.graph != nil {
				gal.graph.AddEdge(personaID, action.Target, RelMention, rm.SeqNum, rm.Content)
			}
		} else {
			zone := gal.env.GetAgentZone(personaID)
			for _, agentID := range gal.env.GetAgentsInZone(zone) {
				if agentID != personaID {
					gal.relationshipMgr.BoostFamiliarity(personaID, agentID)
					if gal.graph != nil {
						gal.graph.AddEdge(personaID, agentID, RelMention, rm.SeqNum, rm.Content)
					}
				}
			}
		}

	case ActionMove:
		gal.activeDirective = fmt.Sprintf("MOVE to %s", action.Target)
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
		gal.activeDirective = fmt.Sprintf("INTERACT with %s: %s", action.Target, action.Content)
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
		gal.activeDirective = "PASS"
		// Nothing to execute, agent chose to do nothing

	case ActionConflict:
		gal.activeDirective = fmt.Sprintf("CONFLICT with %s: %s", action.Target, action.Content)
		rm := &RoundMessage{
			AgentID:   personaID,
			AgentName: personaName,
			Content:   action.Content,
			To:        action.Target,
			Type:      "conflict",
			SeqNum:    seq,
			Reasoning: gal.activeIntention,
		}

		prefix := ""
		if gal.env.IsAgentHidden(personaID) {
			prefix = "sneak:"
		}

		gal.bus.Send(action.Target, Message{
			From:    personaID,
			To:      action.Target,
			Content: action.Content,
			Type:    "conflict",
			Round:   seq,
		})

		gal.emit(SimulationEvent{Type: "agent_message", Round: seq, Data: rm})

		// Record interaction edge for conflict
		if gal.graph != nil {
			gal.graph.AddEdge(personaID, action.Target, RelRebuttal, rm.SeqNum, rm.Content)
		}

		// Track the conflict in the environment
		gal.env.InitiateConflict(prefix+personaID, action.Target)

	case ActionHide:
		gal.activeDirective = "HIDE"
		gal.env.HideAgent(personaID)
		gal.sa.Memory().Record(MemoryRecord{
			Round:         seq,
			Role:          "action",
			Content:       "你隐蔽了身形，进入了隐藏状态。",
			RecordType:    "hide",
			SimulatedTime: timeEvt.SimTime,
			Timestamp:     time.Now(),
		})
		gal.emit(SimulationEvent{
			Type:  "agent_hide",
			Round: seq,
			Data:  map[string]string{"agent_id": personaID},
		})

	case ActionSpawn:
		gal.activeDirective = fmt.Sprintf("SPAWN %s", action.Target)
		isAdventure := gal.isAdventureEnabled()
		known := false
		targetName := action.Target
		persona := gal.sa.Persona()

		for _, otherP := range gal.allPersonas {
			if strings.EqualFold(otherP.Name, targetName) {
				rel := gal.relationshipMgr.Get(personaID, otherP.ID)
				if rel != nil && rel.Familiarity > 0.0 {
					known = true
					break
				}
			}
		}

		if !known && persona != nil {
			bioLower := strings.ToLower(persona.Bio + " " + persona.Persona)
			targetLower := strings.ToLower(targetName)
			if strings.Contains(bioLower, targetLower) {
				known = true
			}
		}

		if !known && !isAdventure {
			gal.emit(SimulationEvent{
				Type:  "error",
				Round: seq,
				Error: fmt.Sprintf("%s: Spawn failed. Adventure (奇遇) is disabled and character %s is not in your background/relationships.", personaName, targetName),
			})
			gal.sa.Memory().Record(MemoryRecord{
				Round:         seq,
				Role:          "observation",
				Content:       fmt.Sprintf("因为奇遇（adventure）未开启，且你与 %s 并不相识，你无法引入这名角色。", targetName),
				RecordType:    "spawn_failed",
				SimulatedTime: timeEvt.SimTime,
				Timestamp:     time.Now(),
			})
			return
		}

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
		gal.activeDirective = "DIE"
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
			Data:  map[string]string{"agent_id": personaID, "agent_name": personaName, "reason": "voluntary exit"},
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
		Round:         int(gal.actionSeq),
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

func (gal *GAAgentLoop) resolveConflictState(ctx context.Context, initiatorID, initiatorName, targetID, targetName string, action *Action, isSneak bool, round int, timeEvt SimTimeEvent) {
	currentZone := gal.env.GetAgentZone(targetID)
	presentAgents := gal.env.GetAgentsInZone(currentZone)
	
	// Faction strengths
	aFactionStrength := gal.getFactionStrength(initiatorID, presentAgents)
	bFactionStrength := gal.getFactionStrength(targetID, presentAgents)
	
	if isSneak {
		aFactionStrength += 30.0
	}
	
	switch action.Type {
	case ActionMove:
		// B tries to flee
		pEscape := 0.5 + (bFactionStrength-aFactionStrength)/200.0
		if pEscape < 0.1 {
			pEscape = 0.1
		}
		if pEscape > 0.9 {
			pEscape = 0.9
		}
		
		if rand.Float64() < pEscape {
			// Escape success! B moves.
			obs, err := gal.env.MoveAgent(targetID, action.Target)
			if err == nil {
				gal.sa.Memory().Record(MemoryRecord{
					Round:         round,
					Role:          "observation",
					Content:       fmt.Sprintf("你成功摆脱了 %s 的纠缠，逃到了 %s。", initiatorName, action.Target),
					RecordType:    "conflict_result",
					SimulatedTime: timeEvt.SimTime,
					Timestamp:     time.Now(),
				})
				
				// Emit move event
				for _, o := range obs {
					gal.sa.Memory().Record(MemoryRecord{
						Round:         round,
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
					Round: round,
					Data: map[string]string{
						"agent_id": targetID,
						"to_zone":  action.Target,
					},
				})
				
				// Notify initiator A
				gal.bus.Send(initiatorID, Message{
					From:    "System",
					To:      initiatorID,
					Content: fmt.Sprintf("%s 成功摆脱了你的纠缠，逃到了 %s。", targetName, action.Target),
					Type:    "system",
					Round:   round,
				})
			}
		} else {
			// Escape failed! B stays, forced into fight.
			gal.sa.Memory().Record(MemoryRecord{
				Round:         round,
				Role:          "observation",
				Content:       fmt.Sprintf("你试图逃往 %s，但被实力更强（实力估算: %.0f）的 %s（你方实力: %.0f）强行拦截，被迫进入群体打架！", action.Target, aFactionStrength, initiatorName, bFactionStrength),
				RecordType:    "conflict_result",
				SimulatedTime: timeEvt.SimTime,
				Timestamp:     time.Now(),
			})
			
			gal.bus.Send(initiatorID, Message{
				From:    "System",
				To:      initiatorID,
				Content: fmt.Sprintf("%s 试图逃往 %s，被你方强行拦截，双方爆发群体冲突和打架！", targetName, action.Target),
				Type:    "system",
				Round:   round,
			})
			
			// Resolve the fight
			gal.resolveFight(ctx, initiatorID, initiatorName, targetID, targetName, aFactionStrength, bFactionStrength, round, timeEvt)
		}
		
	case ActionConflict:
		// B fights back
		gal.sa.Memory().Record(MemoryRecord{
			Round:         round,
			Role:          "observation",
			Content:       fmt.Sprintf("面对 %s 的冲突挑衅，你果断召集盟友迎战！", initiatorName),
			RecordType:    "conflict_result",
			SimulatedTime: timeEvt.SimTime,
			Timestamp:     time.Now(),
		})
		
		gal.bus.Send(initiatorID, Message{
			From:    "System",
			To:      initiatorID,
			Content: fmt.Sprintf("面对你的挑衅，%s 出手反击，双方爆发群体冲突与打架！", targetName),
			Type:    "system",
			Round:   round,
		})
		
		// Resolve the fight
		gal.resolveFight(ctx, initiatorID, initiatorName, targetID, targetName, aFactionStrength, bFactionStrength, round, timeEvt)
		
	default:
		// B submits/talks
		gal.sa.Memory().Record(MemoryRecord{
			Round:         round,
			Role:          "observation",
			Content:       fmt.Sprintf("面对 %s 的冲突挑衅，你没有动手反抗，而是选择妥协/交谈/顺从。", initiatorName),
			RecordType:    "conflict_result",
			SimulatedTime: timeEvt.SimTime,
			Timestamp:     time.Now(),
		})
		
		gal.bus.Send(initiatorID, Message{
			From:    "System",
			To:      initiatorID,
			Content: fmt.Sprintf("面对你的挑衅，%s 没有动手反抗，而是选择了妥协/交谈/顺从。", targetName),
			Type:    "system",
			Round:   round,
		})
	}
}

func (gal *GAAgentLoop) resolveFight(ctx context.Context, initiatorID, initiatorName, targetID, targetName string, aStrength, bStrength float64, round int, timeEvt SimTimeEvent) {
	diff := aStrength - bStrength
	
	var deadAgentID, deadAgentName, killerID, killerName string
	
	if diff >= 30.0 {
		// A wins, B loses
		gal.sa.Memory().Record(MemoryRecord{
			Round:         round,
			Role:          "observation",
			Content:       fmt.Sprintf("在与 %s 阵营的激烈冲突和打架中，你方由于实力不足（实力值: %.0f，对方: %.0f）被打败并受了轻伤。", initiatorName, bStrength, aStrength),
			RecordType:    "conflict_result",
			SimulatedTime: timeEvt.SimTime,
			Timestamp:     time.Now(),
		})
		
		gal.bus.Send(initiatorID, Message{
			From:    "System",
			To:      initiatorID,
			Content: fmt.Sprintf("在与 %s 的冲突打架中，你方凭着绝对实力（实力值: %.0f，对方: %.0f）彻底击败了对方，占了上风！", targetName, aStrength, bStrength),
			Type:    "system",
			Round:   round,
		})
		
		// Death check for B
		deadChance := 0.02
		if diff >= 40.0 {
			deadChance = 0.10
		}
		if rand.Float64() < deadChance {
			deadAgentID = targetID
			deadAgentName = targetName
			killerID = initiatorID
			killerName = initiatorName
		}
	} else if diff <= -30.0 {
		// B wins, A loses
		gal.sa.Memory().Record(MemoryRecord{
			Round:         round,
			Role:          "observation",
			Content:       fmt.Sprintf("在与 %s 阵营的冲突打架中，你方大获全胜，凭借实力（实力值: %.0f，对方: %.0f）将对方彻底击败！", initiatorName, bStrength, aStrength),
			RecordType:    "conflict_result",
			SimulatedTime: timeEvt.SimTime,
			Timestamp:     time.Now(),
		})
		
		gal.bus.Send(initiatorID, Message{
			From:    "System",
			To:      initiatorID,
			Content: fmt.Sprintf("在与 %s 的冲突打架中，对方反抗剧烈且实力强劲（实力值: %.0f，你方: %.0f），你方被打败了！", targetName, bStrength, aStrength),
			Type:    "system",
			Round:   round,
		})
		
		// Death check for A
		deadChance := 0.02
		if diff <= -40.0 {
			deadChance = 0.10
		}
		if rand.Float64() < deadChance {
			deadAgentID = initiatorID
			deadAgentName = initiatorName
			killerID = targetID
			killerName = targetName
		}
	} else {
		// Draw
		gal.sa.Memory().Record(MemoryRecord{
			Round:         round,
			Role:          "observation",
			Content:       fmt.Sprintf("在与 %s 阵营的打架冲突中，双方旗鼓相当，互有胜负，最终打成平手，各自受伤退开。", initiatorName),
			RecordType:    "conflict_result",
			SimulatedTime: timeEvt.SimTime,
			Timestamp:     time.Now(),
		})
		
		gal.bus.Send(initiatorID, Message{
			From:    "System",
			To:      initiatorID,
			Content: fmt.Sprintf("在与 %s 的冲突打架中，双方打得难解难分，最终平手，互有轻伤，不欢而散。", targetName),
			Type:    "system",
			Round:   round,
		})
		
		// Accidental death check for both
		if rand.Float64() < 0.02 {
			deadAgentID = targetID
			deadAgentName = targetName
			killerID = initiatorID
			killerName = initiatorName
		} else if rand.Float64() < 0.02 {
			deadAgentID = initiatorID
			deadAgentName = initiatorName
			killerID = targetID
			killerName = targetName
		}
	}
	
	// Handle death if triggered
	if deadAgentID != "" {
		gal.emit(SimulationEvent{
			Type:  "agent_message",
			Round: round,
			Data: &RoundMessage{
				AgentID:   deadAgentID,
				AgentName: deadAgentName,
				Content:   fmt.Sprintf("⚠️ %s 不幸在与 %s 的激烈冲突中被打死/意外身亡了！", deadAgentName, killerName),
				To:        "*",
				Type:      "agent_death_announcement",
				SeqNum:    round,
			},
		})
		
		gal.emit(SimulationEvent{
			Type:  "agent_death",
			Round: round,
			Data:  map[string]string{"agent_id": deadAgentID, "agent_name": deadAgentName, "reason": fmt.Sprintf("killed in conflict with %s", killerName)},
		})
		
		// Notify killer's memory
		gal.bus.Send(killerID, Message{
			From:    "System",
			To:      killerID,
			Content: fmt.Sprintf("系统通知：%s 已经在冲突打架中身亡/被击杀。", deadAgentName),
			Type:    "system",
			Round:   round,
		})
	}
}

func (gal *GAAgentLoop) getFactionStrength(agentID string, zoneAgents []string) float64 {
	total := getAgentStrength(gal.findPersonaByID(agentID))
	for _, otherID := range zoneAgents {
		if otherID == agentID {
			continue
		}
		rel := gal.relationshipMgr.Get(agentID, otherID)
		if rel != nil && rel.Affinity > 0.0 {
			otherPersona := gal.findPersonaByID(otherID)
			total += getAgentStrength(otherPersona)
		}
	}
	return total
}

func getAgentStrength(p *Persona) float64 {
	if p == nil {
		return 50.0
	}
	keys := []string{"combat_strength", "strength", "power", "武功", "实力", "武力值", "战斗力", "武力", "influence", "地位", "wealth", "财富"}
	for _, k := range keys {
		if valStr, ok := p.Traits[k]; ok {
			return parseTraitValue(valStr)
		}
	}
	return 50.0
}

func getAgentPerception(p *Persona) float64 {
	if p == nil {
		return 50.0
	}
	keys := []string{"perception", "detection", "感知", "洞察", "发现", "侦察", "灵觉"}
	for _, k := range keys {
		if valStr, ok := p.Traits[k]; ok {
			return parseTraitValue(valStr)
		}
	}
	return getAgentStrength(p)
}

func getAgentStealth(p *Persona) float64 {
	if p == nil {
		return 50.0
	}
	keys := []string{"stealth", "stealth_level", "潜行", "隐藏", "躲藏", "隐蔽", "隐匿"}
	for _, k := range keys {
		if valStr, ok := p.Traits[k]; ok {
			return parseTraitValue(valStr)
		}
	}
	return getAgentStrength(p)
}

func parseTraitValue(valStr string) float64 {
	valStr = strings.TrimSpace(strings.ToLower(valStr))
	var val float64
	if _, err := fmt.Sscanf(valStr, "%f", &val); err == nil {
		if val >= 0.0 && val <= 1.0 {
			return val * 100.0
		}
		return val
	}
	switch valStr {
	case "master", "expert", "high", "strong", "绝顶", "高", "强", "精通", "神乎其技", "震古烁今", "世所罕见":
		return 85.0
	case "medium", "average", "normal", "中", "普通", "一般", "中等":
		return 50.0
	case "low", "weak", "poor", "低", "弱", "差", "手无缚鸡之力", "不会武功":
		return 20.0
	}
	return 50.0
}

func (gal *GAAgentLoop) isAdventureEnabled() bool {
	if gal.worldState == nil {
		return false
	}
	for _, k := range []string{"adventure", "enable_adventure", "奇遇", "允许奇遇"} {
		if val, ok := gal.worldState.Get(k); ok {
			if b, ok := val.(bool); ok {
				return b
			}
			if s, ok := val.(string); ok {
				return strings.ToLower(s) == "true" || s == "是"
			}
		}
	}
	if val, ok := gal.worldState.Get("_seed_adventure"); ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return false
}

func (gal *GAAgentLoop) findPersonaByID(id string) *Persona {
	for i := range gal.allPersonas {
		if gal.allPersonas[i].ID == id {
			return &gal.allPersonas[i]
		}
	}
	if name, ok := gal.nameByID[id]; ok {
		return &Persona{ID: id, Name: name}
	}
	return &Persona{ID: id, Name: id}
}

func parseReasoning(content string) string {
	lines := strings.Split(content, "\n")
	var thoughts []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "[") || strings.HasSuffix(trimmed, "]") || strings.Contains(trimmed, "SAY") || strings.Contains(trimmed, "MOVE") || strings.Contains(trimmed, "CONFLICT") || strings.Contains(trimmed, "PASS") || strings.Contains(trimmed, "SET") || strings.Contains(trimmed, "RELATION") || strings.Contains(trimmed, "HIDE") || strings.Contains(trimmed, "SPAWN") || strings.Contains(trimmed, "DIE") {
			continue
		}
		thoughts = append(thoughts, trimmed)
	}
	return strings.Join(thoughts, " ")
}
