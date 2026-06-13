package simulation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// SimulationEngine orchestrates the full simulation lifecycle.
type SimulationEngine struct {
	store    Store
	factory  agent.AgentFactory
	registry *agent.Registry
	llm      agent.LLMClient
	toolsCfg tools.Config
	log      *logger.Logger
	config   SimulationConfigFile

	memoryEngine  *memoryengine.Engine  // optional, for KG-based seed processing
	resolveModel  agent.ModelResolver  // nil = skip model resolution (tests)

	subscribers   map[chan SimulationEvent]struct{}
	subscribersMu sync.RWMutex
}

// SimulationConfigFile mirrors the TOML config section.
type SimulationConfigFile struct {
	DefaultModelID         string `toml:"default_model_id"`
	DefaultProviderID      string `toml:"default_provider_id"`
	DBPath                 string `toml:"db_path,omitempty"`
	DefaultMaxActions      int    `toml:"default_max_actions"`
	DefaultMaxWallClockMs  int    `toml:"default_max_wall_clock_ms"`
}

// NewSimulationEngine creates a new engine.
func NewSimulationEngine(
	factory agent.AgentFactory,
	registry *agent.Registry,
	llm agent.LLMClient,
	toolsCfg tools.Config,
	cfg SimulationConfigFile,
	log *logger.Logger,
) *SimulationEngine {
	var store Store = NewSimulationStore()
	if cfg.DBPath != "" {
		sqlStore, err := NewSQLiteStore(cfg.DBPath)
		if err != nil && log != nil {
			log.Warn(logger.CatSimulation, "failed to open SQLite store, using memory", "err", err.Error())
		} else if err == nil {
			store = sqlStore
		}
	}

	return &SimulationEngine{
		store:       store,
		factory:     factory,
		registry:    registry,
		llm:         llm,
		toolsCfg:    toolsCfg,
		log:         log,
		config:      cfg,
		subscribers: make(map[chan SimulationEvent]struct{}),
	}
}

// Create validates and stores a new simulation, returning its ID.
func (e *SimulationEngine) Create(config SimulationConfig) (string, error) {
	if err := config.Validate(); err != nil {
		return "", err
	}
	return e.store.Create(config)
}

// Update persists updates to a simulation's state.
func (e *SimulationEngine) Update(id string, state *SimulationState) error {
	return e.store.Update(id, state)
}

// Start begins simulation execution in a background goroutine and returns an event channel.
func (e *SimulationEngine) Start(ctx context.Context, simID string) (<-chan SimulationEvent, error) {
	state, err := e.store.Get(simID)
	if err != nil {
		return nil, err
	}

	state.Lock()
	if state.Status == StatusRunning {
		state.Unlock()
		return nil, ErrSimAlreadyRunning
	}
	state.Status = StatusRunning
	now := time.Now()
	state.StartedAt = &now
	state.Unlock()

	if err := e.store.Update(simID, state); err != nil && e.log != nil {
		e.log.Warn(logger.CatSimulation, "failed to persist running status", "err", err.Error())
	}

	events := make(chan SimulationEvent, 64)

	go func() {
		defer close(events)
		defer func() {
			if r := recover(); r != nil {
				e.emit(events, SimulationEvent{
					Type:         "error",
					SimulationID: simID,
					Error:        fmt.Sprintf("panic: %v", r),
					Timestamp:    time.Now(),
				})
				state.Lock()
				state.Status = StatusFailed
				state.Error = fmt.Sprintf("panic: %v", r)
				state.Unlock()
				if err := e.store.Update(simID, state); err != nil && e.log != nil {
					e.log.Warn(logger.CatSimulation, "failed to persist failed status", "err", err.Error())
				}
			}
		}()

		e.runSimulation(ctx, state, events)
	}()

	return events, nil
}

func (e *SimulationEngine) Stop(simID string) error {
	state, err := e.store.Get(simID)
	if err != nil {
		return err
	}
	state.Lock()
	if state.Status != StatusRunning {
		state.Unlock()
		return ErrSimNotRunning
	}
	state.Status = StatusCancelled
	state.Unlock()
	if err := e.store.Update(simID, state); err != nil && e.log != nil {
		e.log.Warn(logger.CatSimulation, "failed to persist cancelled status", "err", err.Error())
	}
	return nil
}

func (e *SimulationEngine) Get(simID string) (*SimulationState, error) {
	return e.store.Get(simID)
}

func (e *SimulationEngine) List() []*SimulationState {
	return e.store.List()
}

// SetDBPath replaces the store with a SQLite-backed one at the given path.
// Must be called before Create/Start. Existing in-memory data is not migrated.
func (e *SimulationEngine) SetDBPath(path string) error {
	sqlStore, err := NewSQLiteStore(path)
	if err != nil {
		return fmt.Errorf("set db path: %w", err)
	}
	e.store = sqlStore
	return nil
}

// SetMemoryEngine wires an optional MemoryEngine for KG-based seed processing.
func (e *SimulationEngine) SetMemoryEngine(mem *memoryengine.Engine) {
	e.memoryEngine = mem
}

// WithModelResolver sets the model resolver used to translate model IDs to API model names.
func (e *SimulationEngine) WithModelResolver(resolver agent.ModelResolver) {
	e.resolveModel = resolver
}

// resolveModelID translates a model ID to the actual API model name.
// Returns the original modelID if no resolver is configured or resolution fails.
func (e *SimulationEngine) resolveModelID(modelID string) string {
	if e.resolveModel == nil || modelID == "" {
		return modelID
	}
	info, err := e.resolveModel(modelID)
	if err != nil || info.APIModel == "" {
		return modelID
	}
	return info.APIModel
}

// CreateFromSeedOptions defines configuration overrides during seed-based simulation generation.
type CreateFromSeedOptions struct {
	ModelID            string `json:"model_id,omitempty"`
	ProviderID         string `json:"provider_id,omitempty"`
	MaxActions         int    `json:"max_actions,omitempty"`
	MaxWallClockMs     int    `json:"max_wall_clock_ms,omitempty"`
	TriggerPolicy      string `json:"trigger_policy,omitempty"`
	MinSpeakIntervalMs int    `json:"min_speak_interval_ms,omitempty"`
}

// CreateFromSeed extracts entities and generates personas from seed text,
// then creates a simulation. Returns the simulation ID, extraction, and personas.
func (e *SimulationEngine) CreateFromSeed(
	ctx context.Context,
	seedText string,
	topic string,
	personaCount int,
	opts CreateFromSeedOptions,
) (simID string, extraction *SeedExtraction, personas []Persona, err error) {

	// Truncate excessive seed text
	if len(seedText) > 50000 {
		seedText = seedText[:50000]
	}

	// Step 1: Extract entities, world state, topics
	extractorModel := e.resolveModelID(e.config.DefaultModelID)
	if opts.ModelID != "" {
		extractorModel = e.resolveModelID(opts.ModelID)
	}
	extractorProvider := e.config.DefaultProviderID
	if opts.ProviderID != "" {
		extractorProvider = opts.ProviderID
	}

	extractor := NewSeedExtractor(e.llm, extractorModel, extractorProvider, e.memoryEngine)
	extractor.SetLogger(e.log)
	if e.log != nil {
		e.log.InfoContext(ctx, logger.CatSimulation, "create from seed: starting extraction")
	}
	extraction, err = extractor.Extract(ctx, seedText)
	if err != nil {
		return "", nil, nil, fmt.Errorf("seed extract: %w", err)
	}

	// Determine persona count based on deduction capability first
	isDeduced := false
	if len(extraction.SuggestedAgents) > 0 {
		personaCount = len(extraction.SuggestedAgents)
		isDeduced = true
	} else if personaCount <= 0 {
		// Fallback to complexity-based auto-detect if no suggested agents and personaCount is 0
		personaCount = 3 // default baseline
		if len(extraction.ConflictAreas) >= 3 || len(extraction.Entities) >= 5 {
			personaCount = 5
		} else if len(extraction.ConflictAreas) >= 2 || len(extraction.Entities) >= 3 {
			personaCount = 4
		}
	}

	// Clamp persona count
	if personaCount < 2 {
		personaCount = 2
	}
	if isDeduced {
		if personaCount > 50 {
			personaCount = 50
		}
	} else {
		if personaCount > 5 {
			personaCount = 5
		}
	}

	// Use first key topic as topic if not provided
	if topic == "" {
		if len(extraction.KeyTopics) > 0 {
			topic = extraction.KeyTopics[0]
		} else {
			topic = "General discussion"
		}
	}

	// Step 2: Generate personas
	genModel := e.resolveModelID(e.config.DefaultModelID)
	if opts.ModelID != "" {
		genModel = e.resolveModelID(opts.ModelID)
	}
	genProvider := e.config.DefaultProviderID
	if opts.ProviderID != "" {
		genProvider = opts.ProviderID
	}

	gen := NewPersonaGenerator(e.llm, genModel, genProvider, e.memoryEngine)
	gen.SetLogger(e.log)
	if e.log != nil {
		e.log.InfoContext(ctx, logger.CatSimulation, "create from seed: generating personas", "count", personaCount, "extracted_entities", len(extraction.Entities))
	}
	personas, err = gen.Generate(ctx, extraction, topic, personaCount)
	if err != nil {
		return "", nil, nil, fmt.Errorf("persona generation: %w", err)
	}

	// Override individual persona models/providers with the requested custom ones
	for i := range personas {
		if opts.ModelID != "" {
			personas[i].ModelID = opts.ModelID
		}
		if opts.ProviderID != "" {
			personas[i].ProviderID = opts.ProviderID
		}
	}

	maxActions := e.config.DefaultMaxActions
	if opts.MaxActions > 0 {
		maxActions = opts.MaxActions
	}
	maxWallClockMs := e.config.DefaultMaxWallClockMs
	if opts.MaxWallClockMs > 0 {
		maxWallClockMs = opts.MaxWallClockMs
	}
	triggerPolicy := "selective"
	if opts.TriggerPolicy != "" {
		triggerPolicy = opts.TriggerPolicy
	}
	minSpeakIntervalMs := 2000
	if opts.MinSpeakIntervalMs > 0 {
		minSpeakIntervalMs = opts.MinSpeakIntervalMs
	}

	// Step 3: Create the simulation
	config := SimulationConfig{
		Topic:              topic,
		Personas:           personas,
		WorldState:         extraction.WorldState,
		MaxActions:         maxActions,
		MaxWallClockMs:     maxWallClockMs,
		TriggerPolicy:      triggerPolicy,
		MinSpeakIntervalMs: minSpeakIntervalMs,
	}

	simID, err = e.Create(config)
	if err != nil {
		return "", nil, nil, fmt.Errorf("create simulation: %w", err)
	}

	return simID, extraction, personas, nil
}

// ReplayAsk queries an agent in-character using their simulation memories as context.
func (e *SimulationEngine) ReplayAsk(ctx context.Context, simID, personaID, question string) (string, error) {
	state, err := e.store.Get(simID)
	if err != nil {
		return "", err
	}

	state.RLock()
	status := state.Status
	state.RUnlock()

	if status != StatusCompleted {
		return "", fmt.Errorf("simulation %s is not completed (status: %s)", simID, status)
	}

	// Handle Report Agent Replay
	if personaID == "report" || personaID == "report_agent" {
		state.RLock()
		report := state.Report
		state.RUnlock()

		if report == "" {
			return "", fmt.Errorf("no report found for simulation %s", simID)
		}
		prompt := BuildReportAnalystPrompt(state.Config.Topic, report, question)
	resp, err := e.llm.Chat(ctx, agent.LLMRequest{
		Model:      e.resolveModelID(e.config.DefaultModelID),
		ProviderID: e.config.DefaultProviderID,
		Messages:   []agent.LLMMessage{{Role: "user", Content: prompt}},
		})
		if err != nil {
			return "", fmt.Errorf("report analyst ask: %w", err)
		}
		return resp.Content, nil
	}

	// Find the persona
	var persona *Persona
	for i, p := range state.Config.Personas {
		if p.ID == personaID {
			persona = &state.Config.Personas[i]
			break
		}
	}
	if persona == nil {
		return "", fmt.Errorf("persona %s not found in simulation %s", personaID, simID)
	}

	// Get agent memories
	records, err := e.store.GetAgentMemories(simID, personaID)
	if err != nil {
		return "", fmt.Errorf("get agent memories: %w", err)
	}
	if len(records) == 0 {
		return "", fmt.Errorf("no memories found for agent %s in simulation %s", personaID, simID)
	}

	// Build follow-up prompt
	prompt := BuildReplayPrompt(persona, state.Config.Topic, records, question)

	resp, err := e.llm.Chat(ctx, agent.LLMRequest{
		Model:      e.config.DefaultModelID,
		ProviderID: e.config.DefaultProviderID,
		Messages:   []agent.LLMMessage{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", fmt.Errorf("replay ask: %w", err)
	}

	return resp.Content, nil
}

// ForkRequest defines what-if parameters when forking a simulation.
type ForkRequest struct {
	NewWorldState   map[string]any `json:"new_world_state,omitempty"`
	ExtraPersonas   []Persona      `json:"extra_personas,omitempty"`
	NewTopic        string         `json:"new_topic,omitempty"`
	NewMaxActions   int            `json:"new_max_actions,omitempty"`
}

// Fork clones a completed simulation with modified parameters for "what-if" replay.
// Returns the new simulation ID. The original simulation is unchanged.
func (e *SimulationEngine) Fork(ctx context.Context, sourceSimID string, req ForkRequest) (string, error) {
	srcState, err := e.store.Get(sourceSimID)
	if err != nil {
		return "", err
	}

	srcState.RLock()
	status := srcState.Status
	srcConfig := srcState.Config
	srcState.RUnlock()

	if status != StatusCompleted && status != StatusCancelled && status != StatusFailed {
		return "", fmt.Errorf("source simulation %s is not finished (status: %s)", sourceSimID, status)
	}

	// Build new config from source
	newConfig := SimulationConfig{
		Topic: srcConfig.Topic,
	}

	// Clone all original personas
	newConfig.Personas = make([]Persona, len(srcConfig.Personas))
	copy(newConfig.Personas, srcConfig.Personas)

	// Apply overrides
	if req.NewTopic != "" {
		newConfig.Topic = req.NewTopic
	}

	if req.NewMaxActions > 0 {
		newConfig.MaxActions = req.NewMaxActions
	} else {
		newConfig.MaxActions = srcConfig.MaxActions
	}

	// Merge world state: start with original, overlay with fork overrides
	if srcConfig.WorldState != nil {
		newConfig.WorldState = make(map[string]any, len(srcConfig.WorldState))
		for k, v := range srcConfig.WorldState {
			newConfig.WorldState[k] = v
		}
	}
	if newConfig.WorldState == nil {
		newConfig.WorldState = make(map[string]any)
	}
	for k, v := range req.NewWorldState {
		newConfig.WorldState[k] = v
	}

	// Add extra personas if provided
	if len(req.ExtraPersonas) > 0 {
		// Check for ID conflicts
		existing := make(map[string]bool)
		for _, p := range newConfig.Personas {
			existing[p.ID] = true
		}
		for _, p := range req.ExtraPersonas {
			if existing[p.ID] {
				return "", fmt.Errorf("extra persona ID %q conflicts with existing persona", p.ID)
			}
			newConfig.Personas = append(newConfig.Personas, p)
		}
	}

	// Preserve other config from source
	if newConfig.MaxActions <= 0 {
		newConfig.MaxActions = srcConfig.MaxActions
	}
	newConfig.MaxWallClockMs = srcConfig.MaxWallClockMs
	newConfig.TriggerPolicy = srcConfig.TriggerPolicy
	newConfig.MinSpeakIntervalMs = srcConfig.MinSpeakIntervalMs

	return e.Create(newConfig)
}

func (e *SimulationEngine) Delete(simID string) error {
	state, err := e.store.Get(simID)
	if err != nil {
		return err
	}
	state.Lock()
	if state.Status == StatusRunning {
		state.Unlock()
		return ErrSimAlreadyRunning
	}
	state.Unlock()
	return e.store.Delete(simID)
}

func (e *SimulationEngine) Subscribe(ch chan SimulationEvent) {
	e.subscribersMu.Lock()
	e.subscribers[ch] = struct{}{}
	e.subscribersMu.Unlock()
}

func (e *SimulationEngine) Unsubscribe(ch chan SimulationEvent) {
	e.subscribersMu.Lock()
	delete(e.subscribers, ch)
	e.subscribersMu.Unlock()
}

// emitProgress sends a progress event with the given phase and action counts.
func (e *SimulationEngine) emitProgress(events chan SimulationEvent, simID, phase string, currentActions, maxActions int) {
	e.emit(events, SimulationEvent{
		Type:         "progress",
		SimulationID: simID,
		Data: &SimulationProgress{
			SimulationID:    simID,
			Phase:           phase,
			ProgressPercent: float64(currentActions) / float64(maxActions) * 100,
			CurrentActions:  currentActions,
			MaxActions:      maxActions,
			RecentLogs:      []string{},
			AgentStates:     map[string]*AgentProgressState{},
			GraphEdges:      []EdgeDTO{},
		},
	})
}

func (e *SimulationEngine) emit(events chan SimulationEvent, ev SimulationEvent) {
	ev.Timestamp = time.Now()
	select {
	case events <- ev:
	default:
	}
	e.subscribersMu.RLock()
	for ch := range e.subscribers {
		select {
		case ch <- ev:
		default:
		}
	}
	e.subscribersMu.RUnlock()
}

// runSimulation executes the event-driven simulation.
func (e *SimulationEngine) runSimulation(ctx context.Context, state *SimulationState, events chan SimulationEvent) {
	simID := state.RunID
	config := state.Config

	e.emitProgress(events, simID, "initializing", 0, config.MaxActions)

	e.emit(events, SimulationEvent{
		Type:         "simulation_start",
		SimulationID: simID,
		Data: map[string]any{
			"topic": config.Topic, "personas": len(config.Personas),
			"max_actions": config.MaxActions, "trigger_policy": config.TriggerPolicy,
		},
	})

	graph := NewRelationGraph()
	bus := NewMessageBus(64)
	simAgents, err := e.createSimAgents(ctx, config, bus)
	if err != nil {
		state.Lock()
		state.Status = StatusFailed
		state.Error = err.Error()
		state.Unlock()
		if errUpdate := e.store.Update(simID, state); errUpdate != nil && e.log != nil {
			e.log.Warn(logger.CatSimulation, "failed to persist failed status", "err", errUpdate.Error())
		}
		e.emit(events, SimulationEvent{Type: "error", SimulationID: simID, Error: err.Error()})
		return
	}
	defer func() {
		for _, sa := range simAgents {
			sa.Stop(10 * time.Second)
		}
	}()

	e.runEventDriven(ctx, state, simAgents, bus, graph, events)
}

// runEventDriven executes the event-driven loop.
func (e *SimulationEngine) runEventDriven(ctx context.Context, state *SimulationState, simAgents []*SimAgent, bus *MessageBus, graph *RelationGraph, events chan SimulationEvent) {
	config := state.Config

	minInterval := time.Duration(config.MinSpeakIntervalMs) * time.Millisecond
	trigger := NewTriggerPolicy(config.TriggerPolicy, minInterval)
	timeout := time.Duration(config.MaxWallClockMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	poolSize := 20
	if len(simAgents) < poolSize {
		poolSize = len(simAgents)
	}
	loop := NewEventLoop(simAgents, bus, state.WorldState, graph, config.Topic, trigger, timeout, config.MaxActions, poolSize, e.log)

	evCh := loop.Run(ctx)
	for ev := range evCh {
		ev.SimulationID = state.RunID
		if ev.Type == "progress" {
			if p, ok := ev.Data.(*SimulationProgress); ok {
				p.SimulationID = state.RunID
			}
		}
		e.emit(events, ev)

		if ev.Type == "agent_message" {
			if rm, ok := ev.Data.(*RoundMessage); ok {
				state.Lock()
				state.Rounds = append(state.Rounds, RoundResult{
					RoundNumber: ev.Round,
					Messages:    []RoundMessage{*rm},
					CompletedAt: time.Now(),
				})
				if as, ok2 := state.AgentStates[rm.AgentID]; ok2 {
					as.TotalMessages++
				}
				// Update current graph state in memory
				nodes := make([]string, 0, len(graph.nodes))
				for n := range graph.nodes {
					nodes = append(nodes, n)
				}
				state.Graph = &SimulationRelationGraph{
					Nodes: nodes,
					Edges: graph.ToEdgeDTOs(),
				}
				state.Unlock()
			}
		}
	}

	// Persist state if using SQLite store
	e.maybePersist(state)

	// Persist agent memories
	e.persistAgentMemories(state.RunID, simAgents)

	e.emitProgress(events, state.RunID, "generating_report", config.MaxActions, config.MaxActions)

	// Generate report with graph data
	report, err := e.generateReport(ctx, config, simAgents, graph, state.WorldState)
	if err == nil && report != "" {
		state.Lock()
		state.Report = report
		state.Unlock()
	}

	// Index simulation results into MemoryEngine KG (if configured)
	e.indexSimulationToKG(ctx, state.RunID, config.Topic, simAgents, graph, state.WorldState, report)

	e.emitProgress(events, state.RunID, "completed", config.MaxActions, config.MaxActions)

	e.emit(events, SimulationEvent{
		Type:         "finished",
		SimulationID: state.RunID,
		Data:         map[string]any{"report": report, "rounds": len(state.Rounds)},
	})

	state.Lock()
	if state.Status == StatusRunning {
		state.Status = StatusCompleted
	}
	now := time.Now()
	state.CompletedAt = &now
	nodes := make([]string, 0, len(graph.nodes))
	for n := range graph.nodes {
		nodes = append(nodes, n)
	}
	state.Graph = &SimulationRelationGraph{
		Nodes: nodes,
		Edges: graph.ToEdgeDTOs(),
	}
	state.Unlock()

	if err := e.store.Update(state.RunID, state); err != nil && e.log != nil {
		e.log.Warn(logger.CatSimulation, "failed to persist final simulation state", "err", err.Error())
	}
}

func (e *SimulationEngine) maybePersist(state *SimulationState) {
	if sqlStore, ok := e.store.(*SQLiteStore); ok {
		if err := sqlStore.SaveResults(state.RunID, state.Rounds, state.Report); err != nil && e.log != nil {
			e.log.Warn(logger.CatSimulation, "failed to persist simulation results", "err", err.Error())
		}
	}
}

// persistAgentMemories saves all agent memories to the store for replay.
func (e *SimulationEngine) persistAgentMemories(simID string, simAgents []*SimAgent) {
	for _, sa := range simAgents {
		records := sa.Memory().Records()
		if len(records) == 0 {
			continue
		}
		if err := e.store.SaveAgentMemories(simID, sa.PersonaID(), records); err != nil && e.log != nil {
			e.log.Warn(logger.CatSimulation, "failed to persist agent memories",
				"persona_id", sa.PersonaID(), "err", err.Error())
		}
	}
}

// indexSimulationToKG indexes simulation results into the MemoryEngine KG.
// Converts the RelationGraph into entity extractions and persists the report.
func (e *SimulationEngine) indexSimulationToKG(ctx context.Context, simID, topic string, simAgents []*SimAgent, graph *RelationGraph, ws *WorldState, report string) {
	if e.memoryEngine == nil {
		return
	}

	var entities []memoryengine.EntityExtraction

	// Helper to prefix agent IDs with SimulationID to prevent collision
	prefixAgent := func(id string) string {
		return "sim_" + simID + "_" + id
	}

	// 1. Each agent becomes an entity with their persona traits
	for _, sa := range simAgents {
		entity := memoryengine.EntityExtraction{
			Name:       prefixAgent(sa.PersonaID()),
			Type:       "agent",
			Confidence: 1.0,
		}
		// Add stance information from their traits
		p := sa.Persona()
		for k, v := range p.Traits {
			if len(k) > 7 && k[:7] == "stance:" {
				entity.Relations = append(entity.Relations, memoryengine.RelationExtraction{
					TargetName: k[7:], // Topic/entity is global, not prefixed
					RelType:    "stance_" + v,
					Weight:     0.8,
				})
			}
		}
		entities = append(entities, entity)
	}

	// 2. Convert RelationGraph edges to KG relations
	for _, edge := range graph.Edges() {
		entities = append(entities, memoryengine.EntityExtraction{
			Name:       prefixAgent(edge.Source),
			Type:       "agent",
			Confidence: 0.9,
			Relations: []memoryengine.RelationExtraction{
				{
					TargetName: prefixAgent(edge.Target),
					RelType:    string(edge.Type),
					Weight:     float64(edge.Weight) / 5.0, // normalize to ~0-1
				},
			},
		})
	}

	// 3. Index key world state changes
	changes := ws.History()
	if len(changes) > 0 {
		// Take snapshot of final state for the context
		finalState := ws.Snapshot()
		for k := range finalState {
			_ = k // use entity name; value not stored in KG
			entities = append(entities, memoryengine.EntityExtraction{
				Name:       "world_" + k,
				Type:       "world_state",
				Confidence: 0.7,
			})
		}
	}

	// 4. Build content from report (or fallback to topic)
	content := topic
	if report != "" {
		content = "Simulation Report: " + topic + "\n\n" + report
	}

	// 5. Save to KG
	now := time.Now().Format(time.RFC3339)
	hash, _, err := e.memoryEngine.SaveWithEntities(ctx, content, now, "simulation_result", now, entities)
	if err != nil {
		if e.log != nil {
			e.log.Warn(logger.CatSimulation, "failed to index simulation to KG", "sim_id", simID, "err", err.Error())
		}
		return
	}

	if e.log != nil {
		e.log.Info(logger.CatSimulation, "indexed simulation to KG", "sim_id", simID, "hash", hash, "entities", len(entities))
	}
}

// createSimAgents creates Agent instances for each persona.
func (e *SimulationEngine) createSimAgents(ctx context.Context, config SimulationConfig, bus *MessageBus) ([]*SimAgent, error) {
	var simAgents []*SimAgent

	for _, persona := range config.Personas {
		systemPrompt := BuildSimulationSystemPrompt(persona, config.Topic, config.Personas)

		modelID := persona.ModelID
		if modelID == "" {
			modelID = e.config.DefaultModelID
		}
		providerID := persona.ProviderID
		if providerID == "" {
			providerID = e.config.DefaultProviderID
		}

		tmpl := agent.AgentTemplate{
			ID:           "sim-" + persona.ID,
			Name:         persona.Name,
			Description:  persona.Role,
			SystemPrompt: systemPrompt,
			ModelID:      modelID,
			Permission:   true,
		}

		agt, cw, err := e.factory.Create(ctx, tmpl, "")
		if err != nil {
			for _, sa := range simAgents {
				sa.Stop(5 * time.Second)
			}
			return nil, fmt.Errorf("create agent for persona %s: %w", persona.ID, err)
		}

		// Replace the factory's framework system prompt (L2/L3) with the
		// simulation-specific persona prompt. Factory always pushes its own
		// L2/L3 prompt; simulation agents only need their persona prompt.
		maxTokens := agt.Def.ContextWindow
		if maxTokens <= 0 {
			maxTokens = agent.DefaultContextWindow
		}
		cw = ctxwin.NewContextWindow(maxTokens, 2000, 0, ctxwin.NewTokenizer())
		cw.Push(ctxwin.RoleSystem, systemPrompt)

		if providerID != "" {
			agt.Def.ProviderID = providerID
		}

		bus.Register(persona.ID)

		perAgentTimeout := time.Duration(config.MaxWallClockMs) * time.Millisecond
		if perAgentTimeout <= 0 {
			perAgentTimeout = 5 * time.Minute
		}
		simAgent := NewSimAgent(agt, &persona, cw, bus, e.log, perAgentTimeout)
		simAgents = append(simAgents, simAgent)

		if e.log != nil {
			e.log.InfoContext(ctx, logger.CatSimulation, "simulation: created agent",
				"persona_id", persona.ID,
				"instance_id", agt.InstanceID,
			)
		}
	}

	return simAgents, nil
}

// generateReport produces a final analysis using the LLM.
func (e *SimulationEngine) generateReport(ctx context.Context, config SimulationConfig, simAgents []*SimAgent, graph *RelationGraph, ws *WorldState) (string, error) {
	if e.llm == nil {
		return "", nil
	}

	memories := make(map[string]*AgentMemory, len(simAgents))
	for _, sa := range simAgents {
		memories[sa.PersonaID()] = sa.Memory()
	}

	prompt := BuildReportPrompt(config.Topic, memories, graph, ws)

	resp, err := e.llm.Chat(ctx, agent.LLMRequest{
		Model:      e.resolveModelID(e.config.DefaultModelID),
		ProviderID: e.config.DefaultProviderID,
		Messages:   []agent.LLMMessage{{Role: "user", Content: prompt}},
		MaxTokens:  2048,
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}
