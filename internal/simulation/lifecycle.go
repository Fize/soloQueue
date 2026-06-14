package simulation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// lifecycleManager handles agent spawn and death during simulation runtime.
// It processes events from two sources:
//  1. Seed-driven scheduled events (sim_time, wall_time, condition triggers)
//  2. LLM-driven events from agent loops ([SPAWN] and [DIE] directives)
type lifecycleManager struct {
	engine          *SimulationEngine
	config          SimulationConfig
	clock           *SimClock
	env             *Environment
	bus             *MessageBus
	dialogueMgr     *DialogueManager
	relationshipMgr *RelationshipManager
	graph           *RelationGraph
	planGen         *PlanGenerator
	reflectionEng   *ReflectionEngine
	nameByID        map[string]string
	allPersonas     []Persona
	log             *logger.Logger
	worldState      *WorldState
	state           *SimulationState

	// Dynamic loop management
	gaLoops   map[string]*GAAgentLoop
	gaLoopsMu sync.RWMutex

	// Spawn tracking
	spawnCount      atomic.Int32
	maxSpawnTotal   int
	lastSpawnTick   int
	lastSpawnTickMu sync.Mutex

	// Lifecycle event channels
	lifecycleCh chan SimulationEvent
	stopCh      chan struct{}
}

func newLifecycleManager(
	engine *SimulationEngine,
	config SimulationConfig,
	clock *SimClock,
	env *Environment,
	bus *MessageBus,
	dialogueMgr *DialogueManager,
	relationshipMgr *RelationshipManager,
	graph *RelationGraph,
	planGen *PlanGenerator,
	reflectionEng *ReflectionEngine,
	nameByID map[string]string,
	allPersonas []Persona,
	worldState *WorldState,
	state *SimulationState,
	l *logger.Logger,
) *lifecycleManager {
	maxSpawn := len(allPersonas) * 2
	if maxSpawn < 4 {
		maxSpawn = 4
	}

	return &lifecycleManager{
		engine:          engine,
		config:          config,
		clock:           clock,
		env:             env,
		bus:             bus,
		dialogueMgr:     dialogueMgr,
		relationshipMgr: relationshipMgr,
		graph:           graph,
		planGen:         planGen,
		reflectionEng:   reflectionEng,
		nameByID:        nameByID,
		allPersonas:     allPersonas,
		log:             l,
		worldState:      worldState,
		state:           state,
		gaLoops:         make(map[string]*GAAgentLoop),
		maxSpawnTotal:   maxSpawn,
		lifecycleCh:     make(chan SimulationEvent, 32),
		stopCh:          make(chan struct{}),
	}
}

// registerLoop adds a GAAgentLoop to the dynamic map.
func (lm *lifecycleManager) registerLoop(personaID string, loop *GAAgentLoop) {
	lm.gaLoopsMu.Lock()
	lm.gaLoops[personaID] = loop
	lm.gaLoopsMu.Unlock()
}

// removeLoop removes a GAAgentLoop from the dynamic map.
func (lm *lifecycleManager) removeLoop(personaID string) {
	lm.gaLoopsMu.Lock()
	delete(lm.gaLoops, personaID)
	lm.gaLoopsMu.Unlock()
}

// activeLoopCount returns the number of currently running agent loops.
func (lm *lifecycleManager) activeLoopCount() int {
	lm.gaLoopsMu.RLock()
	defer lm.gaLoopsMu.RUnlock()
	return len(lm.gaLoops)
}

// ScheduleEvent is called by the lifecycle scheduler goroutine when a seed-driven event fires.
func (lm *lifecycleManager) ScheduleEvent(ctx context.Context, ev SeedLifecycleEvent) {
	switch ev.Type {
	case "agent_death":
		lm.handleSeedDeath(ctx, ev)
	case "agent_spawn":
		lm.handleSeedSpawn(ctx, ev)
	case "simulation_end":
		// Handled by the scheduler via return channel
	}
}

// handleSeedDeath processes a seed-driven agent death.
func (lm *lifecycleManager) handleSeedDeath(ctx context.Context, ev SeedLifecycleEvent) {
	var personaID string
	for id, name := range lm.nameByID {
		if strings.EqualFold(name, ev.AgentName) {
			personaID = id
			break
		}
	}
	if personaID == "" {
		if lm.log != nil {
			lm.log.WarnContext(ctx, logger.CatSimulation, "lifecycle: seed death target not found",
				"agent_name", ev.AgentName)
		}
		return
	}

	if lm.log != nil {
		lm.log.InfoContext(ctx, logger.CatSimulation, "lifecycle: seed-triggered agent death",
			"agent_id", personaID, "reason", ev.Reason)
	}

	lm.handleAgentDeath(ctx, personaID, ev.Reason)
}

// handleSeedSpawn processes a seed-driven agent spawn.
func (lm *lifecycleManager) handleSeedSpawn(ctx context.Context, ev SeedLifecycleEvent) {
	if int(lm.spawnCount.Load()) >= lm.maxSpawnTotal {
		return
	}

	spawnInfo := SpawnInfo{
		Name:        ev.AgentName,
		Description: ev.AgentRole,
		RequestedBy: "seed",
	}
	lm.handleAgentSpawn(ctx, spawnInfo)
}

// LifecycleEvents returns the channel for receiving lifecycle events from agent loops.
func (lm *lifecycleManager) LifecycleEvents() chan<- SimulationEvent {
	return lm.lifecycleCh
}

// Run starts the lifecycle event loop. Should be called in a goroutine.
func (lm *lifecycleManager) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-lm.stopCh:
			return
		case ev := <-lm.lifecycleCh:
			lm.processEvent(ctx, ev)
		}
	}
}

// Stop signals the lifecycle manager to stop.
func (lm *lifecycleManager) Stop() {
	select {
	case <-lm.stopCh:
	default:
		close(lm.stopCh)
	}
}

func (lm *lifecycleManager) processEvent(ctx context.Context, ev SimulationEvent) {
	switch ev.Type {
	case "agent_death":
		data, ok := ev.Data.(map[string]string)
		if !ok {
			return
		}
		personaID := data["agent_id"]
		lm.handleAgentDeath(ctx, personaID, "agent chose to exit")

	case "agent_spawn":
		spawnInfo, ok := ev.Data.(*SpawnInfo)
		if !ok {
			return
		}
		lm.handleAgentSpawn(ctx, *spawnInfo)
	}
}

// handleAgentDeath performs the full death cleanup chain for an agent.
func (lm *lifecycleManager) handleAgentDeath(ctx context.Context, personaID, reason string) {
	lm.gaLoopsMu.RLock()
	loop, exists := lm.gaLoops[personaID]
	lm.gaLoopsMu.RUnlock()

	if !exists {
		return
	}

	// 1. Stop the agent's tick loop
	loop.Stop()

	// 2. End any active dialogues
	lm.dialogueMgr.EndDialogue(personaID)

	// 3. Remove from environment
	lm.env.RemoveAgent(personaID)

	// 4. Unregister from message bus
	lm.bus.Unregister(personaID)

	// 5. Remove relationships
	lm.relationshipMgr.RemoveSubject(personaID)

	// 6. Remove from interaction graph
	lm.graph.RemoveNode(personaID)

	// 7. Remove from dynamic loop map
	lm.removeLoop(personaID)

	// 8. Mark as inactive and persist for API visibility
	if lm.state != nil {
		lm.state.Lock()
		if as, ok := lm.state.AgentStates[personaID]; ok {
			as.IsActive = false
		}
		// Persist to store so API consumers can see the state change
		if err := lm.engine.store.Update(lm.state.RunID, lm.state); err != nil && lm.log != nil {
			lm.log.WarnContext(ctx, logger.CatSimulation, "lifecycle: failed to persist agent death state",
				"agent_id", personaID, "err", err.Error())
		}
		lm.state.Unlock()
	}

	// 9. Record in world state
	lm.worldState.Set("agent_departed:"+personaID, reason, "system", 0)

	if lm.log != nil {
		lm.log.InfoContext(ctx, logger.CatSimulation, "lifecycle: agent death processed",
			"agent_id", personaID, "reason", reason,
			"remaining_agents", lm.activeLoopCount())
	}
}

// handleAgentSpawn creates a new agent and adds them to the running simulation.
func (lm *lifecycleManager) handleAgentSpawn(ctx context.Context, info SpawnInfo) {
	// Enforce spawn limits
	if int(lm.spawnCount.Load()) >= lm.maxSpawnTotal {
		if lm.log != nil {
			lm.log.WarnContext(ctx, logger.CatSimulation, "lifecycle: spawn rejected, max total reached")
		}
		return
	}

	// Generate a unique persona ID
	personaID := fmt.Sprintf("spawned_%s_%d", sanitizeSpawnID(info.Name), time.Now().UnixNano()%100000)

	// Build a minimal persona
	persona := Persona{
		ID:   personaID,
		Name: info.Name,
		Role: info.Description,
		Bio:  fmt.Sprintf("Introduced by %s during the simulation.", info.RequestedBy),
	}

	// If an LLM is available, enrich the persona
	if lm.engine.llm != nil {
		enriched, err := lm.generatePersonaForSpawn(ctx, info)
		if err == nil && enriched != nil {
			persona = *enriched
			persona.ID = personaID
		}
	}

	// Create the underlying agent
	modelID := lm.engine.resolveModelID(lm.engine.config.DefaultModelID)
	tmpl := agent.AgentTemplate{
		ID:          "sim-" + personaID,
		Name:        persona.Name,
		Description: persona.Role,
		ModelID:     modelID,
		Permission:  true,
	}

	agt, cw, err := lm.engine.factory.Create(ctx, tmpl, "")
	if err != nil {
		if lm.log != nil {
			lm.log.WarnContext(ctx, logger.CatSimulation, "lifecycle: failed to create spawned agent",
				"persona_id", personaID, "err", err.Error())
		}
		return
	}

	maxTokens := agt.Def.ContextWindow
	if maxTokens <= 0 {
		maxTokens = agent.DefaultContextWindow
	}
	cw = ctxwin.NewContextWindow(maxTokens, 2000, 0, ctxwin.NewTokenizer())

	// Register on message bus
	lm.bus.Register(personaID)

	// Create SimAgent
	perAgentTimeout := time.Duration(lm.config.MaxWallClockMs) * time.Millisecond
	if perAgentTimeout <= 0 {
		perAgentTimeout = 5 * time.Minute
	}
	sa := NewSimAgent(agt, &persona, cw, lm.bus, lm.log, perAgentTimeout)

	// Generate daily plan
	plan, err := lm.planGen.GenerateDailyPlan(ctx, &persona, lm.env, lm.clock)
	if err != nil {
		plan = defaultDailyPlan(personaID, lm.clock.Now(), lm.env.ZoneNames())
	}

	// Build system prompt
	systemPrompt := BuildGenerativeAgentSystemPrompt(
		persona, lm.allPersonas, lm.env, plan,
		lm.relationshipMgr, nil, lm.nameByID, lm.clock,
	)
	sa.ClearCW(systemPrompt)

	// Add to interaction graph
	lm.graph.AddNode(personaID)

	// Place in town_square
	lm.env.PlaceAgent(personaID, "town_square")

	// Create and start GA agent loop
	loop := NewGAAgentLoop(
		sa, lm.env, lm.bus, lm.clock, lm.planGen, lm.relationshipMgr,
		lm.engine.memoryEngine, lm.reflectionEng, lm.dialogueMgr,
		lm.worldState, lm.nameByID, lm.allPersonas, lm.log,
	)
	loop.plan = plan

	lm.registerLoop(personaID, loop)

	// Fan-in this new agent's events
	go func(ch <-chan SimulationEvent) {
		for ev := range ch {
			ev.SimulationID = lm.state.RunID
			lm.engine.emitViaSubscribers(ev)
			if ev.Type == "agent_death" || ev.Type == "agent_spawn" {
				select {
				case lm.lifecycleCh <- ev:
				default:
				}
			}
		}
	}(loop.Events())

	go loop.Run(ctx)

	// Update tracking
	lm.spawnCount.Add(1)
	lm.nameByID[personaID] = persona.Name
	lm.allPersonas = append(lm.allPersonas, persona)

	// Mark in agent states
	if lm.state != nil {
		lm.state.Lock()
		lm.state.AgentStates[personaID] = &AgentState{
			PersonaID:  personaID,
			InstanceID: agt.InstanceID,
			IsActive:   true,
		}
		// Persist immediately for API visibility
		if err := lm.engine.store.Update(lm.state.RunID, lm.state); err != nil && lm.log != nil {
			lm.log.WarnContext(ctx, logger.CatSimulation, "lifecycle: failed to persist agent spawn state",
				"agent_id", personaID, "err", err.Error())
		}
		lm.state.Unlock()
	}

	// Record in world state
	lm.worldState.Set("agent_spawned:"+personaID, info.Description, "system", 0)

	if lm.log != nil {
		lm.log.InfoContext(ctx, logger.CatSimulation, "lifecycle: agent spawned",
			"persona_id", personaID,
			"name", persona.Name,
			"total_spawns", lm.spawnCount.Load(),
			"total_agents", lm.activeLoopCount())
	}
}

// generatePersonaForSpawn calls the LLM to create a full persona from spawn info.
func (lm *lifecycleManager) generatePersonaForSpawn(ctx context.Context, info SpawnInfo) (*Persona, error) {
	gen := NewPersonaGenerator(
		lm.engine.llm,
		lm.engine.resolveModelID(lm.engine.config.DefaultModelID),
		lm.engine.config.DefaultProviderID,
		lm.engine.memoryEngine,
	)
	gen.SetLogger(lm.log)

	extraction := &SeedExtraction{
		WorldState: lm.worldState.Snapshot(),
		KeyTopics:  []string{lm.config.Topic},
		SuggestedAgents: []SuggestedAgent{
			{Name: info.Name, Role: info.Description},
		},
	}

	personas, err := gen.Generate(ctx, extraction, lm.config.Topic, 1)
	if err != nil || len(personas) == 0 {
		return nil, err
	}
	return &personas[0], nil
}

// sanitizeSpawnID converts a name to a valid ID component.
func sanitizeSpawnID(name string) string {
	name = strings.ToLower(name)
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, name)
	if len(name) > 20 {
		name = name[:20]
	}
	return name
}

// StartLifecycleScheduler runs a goroutine that checks seed-driven lifecycle events.
func (lm *lifecycleManager) StartLifecycleScheduler(ctx context.Context, simulationStart time.Time) <-chan bool {
	endCh := make(chan bool, 1)

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				close(endCh)
				return
			case <-lm.stopCh:
				close(endCh)
				return
			case <-ticker.C:
				for i := range lm.config.LifecycleEvents {
					ev := &lm.config.LifecycleEvents[i]
					if ev.Triggered {
						continue
					}
					if lm.checkLifecycleTrigger(ev, simulationStart) {
						ev.Triggered = true
						if ev.Type == "simulation_end" {
							if lm.log != nil {
								lm.log.InfoContext(ctx, logger.CatSimulation,
									"lifecycle: seed-triggered simulation end", "reason", ev.Reason)
							}
							endCh <- true
							return
						}
						lm.ScheduleEvent(ctx, *ev)
					}
				}
			}
		}
	}()

	return endCh
}

func (lm *lifecycleManager) checkLifecycleTrigger(ev *SeedLifecycleEvent, simStart time.Time) bool {
	switch ev.Trigger {
	case "sim_time":
		return lm.checkSimTimeTrigger(ev.TriggerValue)
	case "wall_time":
		return lm.checkWallTimeTrigger(ev.TriggerValue, simStart)
	case "condition":
		return lm.checkConditionTrigger(ev.TriggerValue)
	}
	return false
}

func (lm *lifecycleManager) checkSimTimeTrigger(value string) bool {
	value = strings.TrimSpace(value)
	if d, err := time.ParseDuration(value); err == nil {
		return lm.clock.ElapsedHours() >= d.Hours()
	}
	if t, err := time.Parse("15:04", value); err == nil {
		now := lm.clock.Now()
		return now.Hour() >= t.Hour() && now.Minute() >= t.Minute()
	}
	return false
}

func (lm *lifecycleManager) checkWallTimeTrigger(value string, simStart time.Time) bool {
	d, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	return time.Since(simStart) >= d
}

func (lm *lifecycleManager) checkConditionTrigger(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.Contains(value, ">=") && !strings.Contains(value, "<=") &&
		!strings.Contains(value, "==") && !strings.Contains(value, "!=") {
		_, ok := lm.worldState.Get(value)
		return ok
	}
	if strings.HasPrefix(value, "agent_count") {
		if strings.Contains(value, ">=") {
			parts := strings.SplitN(value, ">=", 2)
			if len(parts) == 2 {
				var threshold int
				fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &threshold)
				return lm.activeLoopCount() >= threshold
			}
		}
	}
	return false
}
