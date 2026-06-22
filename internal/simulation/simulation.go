package simulation

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
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

	cancels   map[string]context.CancelFunc
	cancelsMu sync.Mutex
}

// SimulationConfigFile mirrors the TOML config section.
type SimulationConfigFile struct {
	DefaultModelID        string `toml:"default_model_id"`
	DefaultProviderID     string `toml:"default_provider_id"`
	DBPath                string `toml:"db_path,omitempty"`
	DefaultMaxWallClockMs int    `toml:"default_max_wall_clock_ms"`
	EnableReflection      bool   `toml:"enable_reflection"`
	SimulatedHours        int    `toml:"simulated_hours"`
	TickIntervalMs        int    `toml:"tick_interval_ms"`
	TimeScale             int    `toml:"time_scale"`
	Language              string `toml:"language"`
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
		cancels:     make(map[string]context.CancelFunc),
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

	simCtx, simCancel := context.WithCancel(ctx)
	e.cancelsMu.Lock()
	if e.cancels == nil {
		e.cancels = make(map[string]context.CancelFunc)
	}
	e.cancels[simID] = simCancel
	e.cancelsMu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				// Print panic and stack trace to stderr so it's visible in test logs
				buf := make([]byte, 1024)
				n := runtime.Stack(buf, false)
				fmt.Fprintf(os.Stderr, "PANIC in simulation: %v\nStack trace:\n%s\n", r, buf[:n])

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
			close(events)
		}()

		e.runSimulation(simCtx, state, events)
		// Ensure fan-in goroutines have drained before closing events.
		time.Sleep(200 * time.Millisecond)
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

	e.cancelsMu.Lock()
	if cancel, exists := e.cancels[simID]; exists {
		cancel()
		delete(e.cancels, simID)
	}
	e.cancelsMu.Unlock()

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

// GetAgentMemories retrieves memory records for a specific agent in a simulation.
func (e *SimulationEngine) GetAgentMemories(simID, personaID string) ([]MemoryRecord, error) {
	return e.store.GetAgentMemories(simID, personaID)
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
	ModelID          string `json:"model_id,omitempty"`
	ProviderID       string `json:"provider_id,omitempty"`
	MaxWallClockMs   int    `json:"max_wall_clock_ms,omitempty"`
	SimulatedHours   int    `json:"simulated_hours,omitempty"`
	TickIntervalMs   int    `json:"tick_interval_ms,omitempty"`
	TimeScale        int    `json:"time_scale,omitempty"`
	EnableReflection bool   `json:"enable_reflection,omitempty"`
	Language         string `json:"language,omitempty"`
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

	// Resolve max_tokens from the model's generation config (DB-backed via ModelResolver)
	modelCfgID := e.config.DefaultModelID
	if opts.ModelID != "" {
		modelCfgID = opts.ModelID
	}
	var maxTokens int
	if e.resolveModel != nil {
		info, err := e.resolveModel(modelCfgID)
		if err == nil {
			maxTokens = info.MaxTokens
		}
	}

	lang := opts.Language
	if lang == "" {
		lang = e.config.Language
	}
	if lang == "" {
		lang = "zh"
	}

	gen := NewPersonaGenerator(e.llm, genModel, genProvider, e.memoryEngine)
	gen.SetLogger(e.log)
	if maxTokens > 0 {
		gen.SetMaxTokens(maxTokens)
	}
	if e.log != nil {
		e.log.InfoContext(ctx, logger.CatSimulation, "create from seed: generating personas", "count", personaCount, "extracted_entities", len(extraction.Entities))
	}
	personas, err = gen.Generate(ctx, extraction, topic, personaCount, lang)
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

	maxWallClockMs := e.config.DefaultMaxWallClockMs
	if opts.MaxWallClockMs > 0 {
		maxWallClockMs = opts.MaxWallClockMs
	}

	simulatedHours := 48
	if opts.SimulatedHours > 0 {
		simulatedHours = opts.SimulatedHours
	} else if e.config.SimulatedHours > 0 {
		simulatedHours = e.config.SimulatedHours
	}

	tickIntervalMs := 500
	if opts.TickIntervalMs > 0 {
		tickIntervalMs = opts.TickIntervalMs
	} else if e.config.TickIntervalMs > 0 {
		tickIntervalMs = e.config.TickIntervalMs
	}

	timeScale := 600
	if opts.TimeScale > 0 {
		timeScale = opts.TimeScale
	} else if e.config.TimeScale > 0 {
		timeScale = e.config.TimeScale
	}

	// Step 3: Create the simulation
	config := SimulationConfig{
		Topic:            topic,
		Personas:         personas,
		WorldState:       extraction.WorldState,
		MaxWallClockMs:   maxWallClockMs,
		SimulatedHours:   simulatedHours,
		TickIntervalMs:   tickIntervalMs,
		TimeScale:        timeScale,
		EnableReflection: opts.EnableReflection,
		LifecycleEvents:  extraction.LifecycleEvents,
		Language:         lang,
	}

	// Step 3a: Carry initial relationships from seed extraction
	if extraction != nil && len(extraction.InitialRelationships) > 0 {
		config.InitialRelationships = extraction.InitialRelationships
	}

	// Step 3b: Build initial graph edges from seed extraction entity relations
	config.InitialEdges = buildInitialEdges(extraction, personas)

	simID, err = e.Create(config)
	if err != nil {
		return "", nil, nil, fmt.Errorf("create simulation: %w", err)
	}

	return simID, extraction, personas, nil
}

// buildInitialEdges maps seed extraction entity relations to persona-to-persona
// graph edges, following the MiroFish approach of pre-populating the interaction
// graph before simulation begins.
func buildInitialEdges(extraction *SeedExtraction, personas []Persona) []EdgeDTO {
	if extraction == nil || len(extraction.Entities) == 0 || len(personas) < 2 {
		return nil
	}

	// entityStance[entityName][personaID] = stance ("pro", "con", "neutral")
	entityStance := make(map[string]map[string]string)
	for _, p := range personas {
		for traitKey, traitVal := range p.Traits {
			if len(traitKey) > 7 && traitKey[:7] == "stance:" {
				entityName := traitKey[7:]
				if entityStance[entityName] == nil {
					entityStance[entityName] = make(map[string]string)
				}
				entityStance[entityName][p.ID] = traitVal
			}
		}
	}

	dedup := make(map[string]bool)
	var edges []EdgeDTO

	addEdge := func(source, target, relType string) {
		key := source + "->" + target + ":" + relType
		if dedup[key] {
			return
		}
		dedup[key] = true
		edges = append(edges, EdgeDTO{
			Source: source,
			Target: target,
			Type:   relType,
			Weight: 1,
		})
	}

	// Map entity relations from seed extraction to persona edges.
	// For each relation (entity A → entity B with relType), create edges
	// between all personas that have stances on entity A and entity B.
	for _, entity := range extraction.Entities {
		sourcePersonas := entityStance[entity.Name]
		if len(sourcePersonas) == 0 {
			continue
		}
		for _, rel := range entity.Relations {
			targetPersonas := entityStance[rel.TargetName]
			if len(targetPersonas) == 0 {
				continue
			}
			relType := mapRelType(string(rel.RelType))
			for srcPID := range sourcePersonas {
				for tgtPID := range targetPersonas {
					if srcPID == tgtPID {
						continue
					}
					addEdge(srcPID, tgtPID, relType)
				}
			}
		}
	}

	// For personas on the same entity, create stance-based edges.
	for entityName, stances := range entityStance {
		_ = entityName
		pids := make([]string, 0, len(stances))
		for pid := range stances {
			pids = append(pids, pid)
		}
		for i := 0; i < len(pids); i++ {
			for j := i + 1; j < len(pids); j++ {
				a, b := pids[i], pids[j]
				sa, sb := stances[a], stances[b]
				if sa == "pro" && sb == "con" || sa == "con" && sb == "pro" {
					addEdge(a, b, "rebuttal")
					addEdge(b, a, "rebuttal")
				} else if sa == sb {
					addEdge(a, b, "agree")
					addEdge(b, a, "agree")
				} else {
					addEdge(a, b, "mention")
					addEdge(b, a, "mention")
				}
			}
		}
	}

	return edges
}

// mapRelType normalizes a relation type string to the simulation edge type.
func mapRelType(t string) string {
	switch t {
	case "rebuttal", "agree", "mention", "propose", "reply":
		return t
	default:
		return "mention"
	}
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
		prompt := BuildReportAnalystPrompt(state.Config.Topic, report, question, state.Config.Language)
		if e.llm == nil {
			return "", fmt.Errorf("no LLM client configured for replay")
		}
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
	prompt := BuildReplayPrompt(persona, state.Config.Topic, records, question, state.Config.Language)

	if e.llm == nil {
		return "", fmt.Errorf("no LLM client configured for replay")
	}
	resp, err := e.llm.Chat(ctx, agent.LLMRequest{
		Model:      e.resolveModelID(e.config.DefaultModelID),
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
	NewWorldState    map[string]any `json:"new_world_state,omitempty"`
	ExtraPersonas    []Persona      `json:"extra_personas,omitempty"`
	NewTopic         string         `json:"new_topic,omitempty"`
	NewMaxWallClockMs int           `json:"new_max_wall_clock_ms,omitempty"`
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

	if req.NewMaxWallClockMs > 0 {
		newConfig.MaxWallClockMs = req.NewMaxWallClockMs
	} else {
		newConfig.MaxWallClockMs = srcConfig.MaxWallClockMs
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

	// Preserve other config from source (only if not overridden by fork)
	if req.NewMaxWallClockMs <= 0 {
		newConfig.MaxWallClockMs = srcConfig.MaxWallClockMs
	}
	newConfig.SimulatedHours = srcConfig.SimulatedHours
	newConfig.TickIntervalMs = srcConfig.TickIntervalMs
	newConfig.TimeScale = srcConfig.TimeScale
	newConfig.EnableReflection = srcConfig.EnableReflection

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

// ─── pprof heap profiling helpers ───────────────────────────────────────────

func dumpHeapProfile(label string) {
	path := fmt.Sprintf("/tmp/sim_heap_%s_%d.prof", label, time.Now().Unix())
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	pprof.WriteHeapProfile(f)
}

func dumpGoroutineProfile(label string) {
	path := fmt.Sprintf("/tmp/sim_goroutine_%s_%d.txt", label, time.Now().Unix())
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	pprof.Lookup("goroutine").WriteTo(f, 2)
}

func logMemStats(log *logger.Logger, label string) {
	if log == nil {
		return
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	log.Info(logger.CatSimulation, "memstats",
		"label", label,
		"heap_alloc_mb", m.HeapAlloc/1024/1024,
		"heap_inuse_mb", m.HeapInuse/1024/1024,
		"heap_objects", m.HeapObjects,
		"goroutines", runtime.NumGoroutine(),
		"num_gc", m.NumGC,
		"total_alloc_mb", m.TotalAlloc/1024/1024,
	)
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

// emitViaSubscribers sends an event to all subscribers without requiring a specific events channel.
// Used by lifecycle manager for events originating outside the main event loop.
func (e *SimulationEngine) emitViaSubscribers(ev SimulationEvent) {
	ev.Timestamp = time.Now()
	e.subscribersMu.RLock()
	for ch := range e.subscribers {
		select {
		case ch <- ev:
		default:
		}
	}
	e.subscribersMu.RUnlock()
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

// runSimulation executes the Generative-Agents simulation.
func (e *SimulationEngine) runSimulation(ctx context.Context, state *SimulationState, events chan SimulationEvent) {
	simID := state.RunID
	defer func() {
		e.cancelsMu.Lock()
		delete(e.cancels, simID)
		e.cancelsMu.Unlock()
	}()
	config := state.Config

	if e.llm == nil {
		state.Lock()
		state.Status = StatusFailed
		state.Error = "simulation engine has no LLM client configured"
		state.Unlock()
		e.emit(events, SimulationEvent{Type: "error", SimulationID: simID, Error: "no LLM client configured"})
		return
	}

	e.emitProgress(events, simID, "initializing", 0, config.MaxWallClockMs)

	// [pprof] baseline before anything happens
	logMemStats(e.log, "baseline_before_any_work")
	dumpHeapProfile("01_baseline")

	// ─── Setup GA infrastructure ───────────────────────────────────────
	timeScale := float64(config.TimeScale)
	if timeScale <= 0 {
		timeScale = 600
	}
	tickDur := time.Duration(config.TickIntervalMs) * time.Millisecond
	if tickDur <= 0 {
		tickDur = 500 * time.Millisecond
	}
	clockCfg := ClockConfig{
		TimeScale: timeScale,
		TickDur:   tickDur,
	}
	clock := NewSimClock(clockCfg)

	env := NewEnvironment(clock)
	setupDefaultZones(env, config.Personas)

	bus := NewMessageBus(64)
	dialogueMgr := NewDialogueManager(bus)
	relationshipMgr := NewRelationshipManager()

	// Build name→ID mapping from personas (needed for BulkInit)
	initNameByID := make(map[string]string, len(config.Personas))
	for _, p := range config.Personas {
		initNameByID[p.ID] = p.Name
	}

	// Initialize relationships from seed extraction, if any
	if len(config.InitialRelationships) > 0 {
		if e.log != nil {
			e.log.InfoContext(ctx, logger.CatSimulation, "initializing seed relationships",
				"count", len(config.InitialRelationships))
		}
		if err := relationshipMgr.BulkInit(config.InitialRelationships, initNameByID); err != nil && e.log != nil {
			e.log.WarnContext(ctx, logger.CatSimulation, "failed to init relationships", "err", err.Error())
		}
	}

	planGen := NewPlanGenerator(e.llm, e.resolveModelID(e.config.DefaultModelID), e.config.DefaultProviderID)

	var reflectionEng *ReflectionEngine
	if config.EnableReflection {
		reflectionEng = NewReflectionEngine(e.llm, e.resolveModelID(e.config.DefaultModelID), e.config.DefaultProviderID, 50)
		if e.log != nil {
			reflectionEng.SetLogger(e.log)
		}
	}

	graph := NewRelationGraph()

	simAgents, err := e.createSimAgents(ctx, config, bus)
	if err != nil {
		state.Lock()
		state.Status = StatusFailed
		state.Error = err.Error()
		state.Unlock()
		e.emit(events, SimulationEvent{Type: "error", SimulationID: simID, Error: err.Error()})
		return
	}
	logMemStats(e.log, "after_create_agents")
	dumpHeapProfile("02_after_agents")

	defer func() {
		for _, sa := range simAgents {
			sa.Stop(10 * time.Second)
		}
	}()

	// Generate persona name mapping once
	nameByID := make(map[string]string, len(config.Personas))
	for _, p := range config.Personas {
		nameByID[p.ID] = p.Name
	}

	e.emitProgress(events, simID, "generating_plans", 0, config.MaxWallClockMs)

	// ─── Generate daily plans & system prompts for each agent ──────────
	agentPlans := make(map[string]*DailyPlan)
	for _, sa := range simAgents {
		persona := sa.Persona()
		plan, err := planGen.GenerateDailyPlan(ctx, persona, env, clock, config.Language)
		if err != nil && e.log != nil {
			e.log.WarnContext(ctx, logger.CatSimulation, "plan generation failed, using default",
				"agent_id", persona.ID, "err", err.Error())
			plan = defaultDailyPlan(persona.ID, clock.Now(), env.ZoneNames())
		}
		agentPlans[persona.ID] = plan
	}
	logMemStats(e.log, "after_plan_generation")
	dumpHeapProfile("03_after_plans")

	e.emitProgress(events, simID, "building_prompts", 0, config.MaxWallClockMs)

	// Build full system prompts and push to each agent
	for _, sa := range simAgents {
		persona := sa.Persona()
		plan := agentPlans[persona.ID]
		systemPrompt := BuildGenerativeAgentSystemPrompt(config.Language, *persona, config.Personas, env, plan, relationshipMgr, nil, nameByID, clock)
		sa.ClearCW(systemPrompt)
	}

	// ─── Create lifecycle manager ────────────────────────────────────
	lifecycleMgr := newLifecycleManager(
		e, config, clock, env, bus, dialogueMgr, relationshipMgr,
		graph, planGen, reflectionEng,
		nameByID, config.Personas, state.WorldState, state, e.log,
	)

	// ─── Create GA agent loops ────────────────────────────────────────
	for _, sa := range simAgents {
		persona := sa.Persona()
		loop := NewGAAgentLoop(
			sa, env, bus, clock, planGen, relationshipMgr,
			e.memoryEngine, reflectionEng, dialogueMgr,
			state.WorldState, nameByID, config.Personas, e.log,
			config.Language,
		)
		loop.plan = agentPlans[persona.ID]
		lifecycleMgr.registerLoop(persona.ID, loop)
	}
	logMemStats(e.log, "after_create_loops")
	dumpHeapProfile("04_after_loops")

	// ─── Fan-in all agent event channels ──────────────────────────────
	lifecycleEvts := lifecycleMgr.LifecycleEvents()
	for _, sa := range simAgents {
		go func(sa *SimAgent) {
			ch := lifecycleMgr.gaLoops[sa.PersonaID()].Events()
			for ev := range ch {
				ev.SimulationID = simID
				e.emit(events, ev)
				if ev.Type == "agent_message" {
					if rm, ok := ev.Data.(*RoundMessage); ok {
						state.Lock()
						state.Rounds = append(state.Rounds, RoundResult{
							RoundNumber: ev.Round,
							Messages:    []RoundMessage{*rm},
							CompletedAt: time.Now(),
						})
						state.Unlock()
					}
				}
				// Forward lifecycle events to the manager
				if ev.Type == "agent_death" || ev.Type == "agent_spawn" {
					select {
					case lifecycleEvts <- ev:
					default:
					}
				}
			}
		}(sa)
	}

	// Start clock in background
	go clock.Start()
	defer clock.Stop()

	// Start lifecycle manager and scheduler
	loopCtx, loopCancel := context.WithCancel(ctx)
	defer loopCancel()

	simStart := time.Now()
	seedEndCh := lifecycleMgr.StartLifecycleScheduler(loopCtx, simStart)
	go lifecycleMgr.Run(loopCtx)
	defer lifecycleMgr.Stop()

	// Start all agent loops
	lifecycleMgr.gaLoopsMu.RLock()
	for _, loop := range lifecycleMgr.gaLoops {
		go loop.Run(loopCtx)
	}
	lifecycleMgr.gaLoopsMu.RUnlock()

	// Periodic runtime stats monitor
	statsDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		defer close(statsDone)
		for {
			select {
			case <-ticker.C:
				logMemStats(e.log, "periodic")
			case <-loopCtx.Done():
				return
			}
		}
	}()
	// [pprof] snapshot after 15s of simulation running
	time.AfterFunc(15*time.Second, func() {
		logMemStats(e.log, "after_15s_running")
		dumpHeapProfile("05_after_15s")
	})

	// ─── Place agents in initial zones ────────────────────────────────
	for _, sa := range simAgents {
		plan := agentPlans[sa.PersonaID()]
		if activity := plan.GetCurrentActivity(clock.Now()); activity != nil {
			env.PlaceAgent(sa.PersonaID(), activity.Location)
		} else {
			env.PlaceAgent(sa.PersonaID(), "town_square")
		}
	}

	// Wait for max wall clock timeout OR simulated hours limit.
	timeout := time.Duration(config.MaxWallClockMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	// Monitor simulated hours: cancel if we exceed the configured limit.
	simHoursDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if clock.ElapsedHours() >= float64(config.SimulatedHours) {
					if e.log != nil {
						e.log.InfoContext(ctx, logger.CatSimulation, "simulated hours limit reached",
							"elapsed_hours", clock.ElapsedHours(),
							"configured_hours", config.SimulatedHours)
					}
					loopCancel()
					return
				}
			case <-simHoursDone:
				return
			case <-loopCtx.Done():
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
	case <-loopCtx.Done():
	case <-time.After(timeout):
	}
	close(simHoursDone)

	// [pprof] simulation just ended
	logMemStats(e.log, "after_simulation_run")
	dumpHeapProfile("06_after_run")
	dumpGoroutineProfile("after_run")

	// Stop lifecycle manager and all loops
	lifecycleMgr.gaLoopsMu.RLock()
	for _, loop := range lifecycleMgr.gaLoops {
		loop.Stop()
	}
	lifecycleMgr.gaLoopsMu.RUnlock()
	lifecycleMgr.Stop()
	loopCancel()
	// Drain seed end channel
	select {
	case <-seedEndCh:
	default:
	}

	// ─── Generate report ──────────────────────────────────────────────
	e.emitProgress(events, simID, "generating_report", config.MaxWallClockMs, config.MaxWallClockMs)

	for _, sa := range simAgents {
		graph.AddNode(sa.PersonaID())
	}

	report, err := e.generateReport(ctx, config, simAgents, graph, state.WorldState)
	if err == nil && report != "" {
		state.Lock()
		state.Report = report
		state.Unlock()
	}
	logMemStats(e.log, "after_report_generation")
	dumpHeapProfile("07_final")

	// Index to KG
	e.indexSimulationToKG(ctx, simID, config.Topic, simAgents, graph, state.WorldState, report)

	// Persist agent memories
	e.persistAgentMemories(simID, simAgents)

	// Export final relationships state
	state.Lock()
	state.Relationships = relationshipMgr.AllRelationships(nameByID)
	state.Unlock()

	e.emitProgress(events, simID, "completed", config.MaxWallClockMs, config.MaxWallClockMs)

	e.emit(events, SimulationEvent{
		Type:         "finished",
		SimulationID: simID,
		Data:         map[string]any{"report": report},
	})

	state.Lock()
	if state.Status == StatusRunning {
		state.Status = StatusCompleted
	}
	now := time.Now()
	state.CompletedAt = &now
	state.Unlock()

	// Persist final simulation rounds and results
	e.maybePersist(state)

	if err := e.store.Update(simID, state); err != nil && e.log != nil {
		e.log.Warn(logger.CatSimulation, "failed to persist final simulation state", "err", err.Error())
	}
}

// SetupDefaultZones creates a standard set of zones if none are pre-configured.
func setupDefaultZones(env *Environment, personas []Persona) {
	env.AddZone("town_square", "The central gathering place of the town.", 100)
	env.AddZone("cafe", "A cozy coffee shop serving fresh pastries and drinks.", 20)
	env.AddZone("library", "A quiet public library with reading rooms and computers.", 30)
	env.AddZone("park", "A beautiful outdoor park with walking paths and benches.", 50)
	env.AddZone("office", "A modern co-working office space.", 25)
	env.AddZone("home", "A residential area with private homes.", 10)
	env.AddZone("market", "A bustling market with various shops and stalls.", 40)
	env.AddZone("gym", "A fitness center with exercise equipment.", 15)
	env.AddZone("restaurant", "A popular restaurant serving lunch and dinner.", 20)

	// Add some interactive objects
	env.AddObject("library", &EnvObject{ID: "library_pc", Name: "Public Computer", Description: "A computer with internet access.", IsInteractive: true, State: map[string]any{"available": true}})
	env.AddObject("cafe", &EnvObject{ID: "cafe_menu", Name: "Menu Board", Description: "Today's specials are written on the board.", IsInteractive: true, State: map[string]any{}})
	env.AddObject("park", &EnvObject{ID: "park_bench", Name: "Wooden Bench", Description: "A comfortable bench for sitting and relaxing.", IsInteractive: true, State: map[string]any{}})
	env.AddObject("market", &EnvObject{ID: "market_stall", Name: "News Stand", Description: "A stall selling newspapers and magazines.", IsInteractive: true, State: map[string]any{}})
}

// defaultDailyPlan creates a minimal plan when generation fails.
func defaultDailyPlan(agentID string, now time.Time, zones []string) *DailyPlan {
	plan := &DailyPlan{AgentID: agentID, GeneratedAt: time.Now()}
	defaultZone := "town_square"
	if len(zones) > 0 {
		defaultZone = zones[0]
	}
	plan.Schedule = append(plan.Schedule, PlanItem{
		StartTime:   now,
		EndTime:     now.Add(12 * time.Hour),
		Activity:    "Go about my day",
		Location:    defaultZone,
		Description: "Live my daily life and interact with people I meet.",
		Status:      "in_progress",
	})
	return plan
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
// System prompts are NOT pushed here — that happens in runSimulation after
// plans and other context are generated.
func (e *SimulationEngine) createSimAgents(ctx context.Context, config SimulationConfig, bus *MessageBus) ([]*SimAgent, error) {
	var simAgents []*SimAgent

	for _, persona := range config.Personas {
		// Placeholder system prompt — will be replaced in runSimulation
		placeholderPrompt := "Simulation agent placeholder. System prompt will be set before simulation starts."

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
			SystemPrompt: placeholderPrompt,
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

		maxTokens := agt.Def.ContextWindow
		if maxTokens <= 0 {
			maxTokens = agent.DefaultContextWindow
		}
		cw = ctxwin.NewContextWindow(maxTokens, 2000, 0, ctxwin.NewTokenizer())
		cw.Push(ctxwin.RoleSystem, placeholderPrompt)

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

	kgContext := e.buildKGReportContext(ctx)

	prompt := BuildReportPrompt(config.Topic, memories, graph, ws, kgContext, config.Language)

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

// buildKGReportContext queries the MemoryEngine KG for entity data to enrich the report.
func (e *SimulationEngine) buildKGReportContext(ctx context.Context) string {
	if e.memoryEngine == nil {
		return ""
	}

	entities, err := e.memoryEngine.Graph().ListEntities(ctx, 50)
	if err != nil || len(entities) == 0 {
		return ""
	}

	edges, _ := e.memoryEngine.Graph().GetAllEdges(ctx, false)

	var b strings.Builder
	b.WriteString("## Knowledge Graph Context\n\n")
	b.WriteString(fmt.Sprintf("- Total entities extracted: %d\n", len(entities)))
	b.WriteString(fmt.Sprintf("- Total relationships: %d\n\n", len(edges)))

	b.WriteString("### Entities\n")
	for _, ent := range entities {
		b.WriteString(fmt.Sprintf("- %s (type: %s, mentions: %d, confidence: %.2f)\n",
			ent.Name, ent.Type, ent.MentionCount, ent.Confidence))
	}

	if len(edges) > 0 {
		b.WriteString("\n### Entity Relationships\n")
		for _, e := range edges {
			b.WriteString(fmt.Sprintf("- %s → %s (%s) weight=%.1f\n",
				e.SourceName, e.TargetName, e.RelType, e.Weight))
		}
	}

	return b.String()
}
