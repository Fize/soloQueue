package simulation

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// SQLiteStore persists simulations to a standalone SQLite database.
// Uses its own file — does NOT share the main entries.db.
type SQLiteStore struct {
	db *sql.DB
	mu sync.Mutex // serialize writes
}

// NewSQLiteStore opens or creates the simulation database at the given path.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("sqlite store: mkdir: %w", err)
	}

	dsn := path + "?_journal_mode=WAL&_busy_timeout=10000&_pragma=synchronous(normal)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite store: open: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	s := &SQLiteStore{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite store: migrate: %w", err)
	}
	return s, nil
}

func (s *SQLiteStore) migrate() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS simulations (
			id TEXT PRIMARY KEY,
			topic TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			mode TEXT NOT NULL DEFAULT 'event-driven',
			personas_json TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			world_state_json TEXT NOT NULL DEFAULT '{}',
			report TEXT NOT NULL DEFAULT '',
			error_msg TEXT NOT NULL DEFAULT '',
			current_round INTEGER NOT NULL DEFAULT 0,
			total_actions INTEGER NOT NULL DEFAULT 0,
			started_at TEXT,
			completed_at TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS simulation_rounds (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			simulation_id TEXT NOT NULL REFERENCES simulations(id) ON DELETE CASCADE,
			round_number INTEGER NOT NULL,
			messages_json TEXT NOT NULL,
			started_at TEXT NOT NULL,
			completed_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_rounds_sim ON simulation_rounds(simulation_id);

		CREATE TABLE IF NOT EXISTS agent_memories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			simulation_id TEXT NOT NULL REFERENCES simulations(id) ON DELETE CASCADE,
			persona_id TEXT NOT NULL,
			round INTEGER NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			world_state_json TEXT NOT NULL DEFAULT '{}',
			received_msgs_json TEXT NOT NULL DEFAULT '[]',
			timestamp TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_memories_sim ON agent_memories(simulation_id);
		CREATE INDEX IF NOT EXISTS idx_memories_persona ON agent_memories(simulation_id, persona_id);
	`)
	if err != nil {
		return err
	}

	// Add missing configuration columns for custom/UI-driven simulation parameters
	_, _ = s.db.Exec("ALTER TABLE simulations ADD COLUMN max_wall_clock_ms INTEGER NOT NULL DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE simulations ADD COLUMN simulated_hours INTEGER NOT NULL DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE simulations ADD COLUMN tick_interval_ms INTEGER NOT NULL DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE simulations ADD COLUMN time_scale INTEGER NOT NULL DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE simulations ADD COLUMN enable_reflection INTEGER NOT NULL DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE simulations ADD COLUMN graph_json TEXT NOT NULL DEFAULT '{}'")
	_, _ = s.db.Exec("ALTER TABLE simulations ADD COLUMN language TEXT NOT NULL DEFAULT 'zh'")
	_, _ = s.db.Exec("ALTER TABLE simulations ADD COLUMN initial_relationships_json TEXT NOT NULL DEFAULT '[]'")
	_, _ = s.db.Exec("ALTER TABLE simulations ADD COLUMN relationships_json TEXT NOT NULL DEFAULT '[]'")

	return nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// ─── SimulationStore interface ────────────────────────────────────────────────

func (s *SQLiteStore) Create(config SimulationConfig) (string, error) {
	if err := config.Validate(); err != nil {
		return "", err
	}

	id := config.ID
	if id == "" {
		id = newUUID()
	}

	pj, _ := json.Marshal(config.Personas)
	wsj, _ := json.Marshal(config.WorldState)

	nodes := make([]string, len(config.Personas))
	for i, p := range config.Personas {
		nodes[i] = p.ID
	}
	edges := config.InitialEdges
	if edges == nil {
		edges = []EdgeDTO{}
	}
	initialGraph := &SimulationRelationGraph{
		Nodes: nodes,
		Edges: edges,
	}
	gj, _ := json.Marshal(initialGraph)

	ir := config.InitialRelationships
	if ir == nil {
		ir = []InitialRelationship{}
	}
	irj, _ := json.Marshal(ir)

	s.mu.Lock()
	_, err := s.db.Exec(`INSERT INTO simulations (id, topic, description, mode, personas_json, world_state_json, max_wall_clock_ms, simulated_hours, tick_interval_ms, time_scale, enable_reflection, graph_json, language, initial_relationships_json)
		VALUES (?, ?, ?, 'event-driven', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, config.Topic, config.Description, string(pj), string(wsj), config.MaxWallClockMs, config.SimulatedHours, config.TickIntervalMs, config.TimeScale, boolToInt(config.EnableReflection), string(gj), config.Language, string(irj))
	s.mu.Unlock()

	if err != nil {
		return "", fmt.Errorf("sqlite store: create: %w", err)
	}
	return id, nil
}

func (s *SQLiteStore) Get(id string) (*SimulationState, error) {
	var (
		topic, desc, mode, pj, wsj, report, errMsg, status, graphJSON, language string
		initialRelsJSON, relationshipsJSON                                        string
		currentRound, totalActions, maxWallClockMs, simulatedHours, tickIntervalMs, timeScale, enableReflection int
		startedAt, completedAt, createdAt                    sql.NullString
	)
	err := s.db.QueryRow(`SELECT topic, description, mode, personas_json, world_state_json,
		status, report, error_msg, current_round, total_actions,
		started_at, completed_at, created_at, max_wall_clock_ms, simulated_hours, tick_interval_ms, time_scale, enable_reflection, graph_json, language, initial_relationships_json, relationships_json
		FROM simulations WHERE id = ?`, id).
		Scan(&topic, &desc, &mode, &pj, &wsj, &status, &report, &errMsg,
			&currentRound, &totalActions, &startedAt, &completedAt, &createdAt,
			&maxWallClockMs, &simulatedHours, &tickIntervalMs, &timeScale, &enableReflection, &graphJSON, &language,
			&initialRelsJSON, &relationshipsJSON)
	if err == sql.ErrNoRows {
		return nil, ErrSimNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite store: get: %w", err)
	}

	var personas []Persona
	json.Unmarshal([]byte(pj), &personas)
	var ws map[string]any
	json.Unmarshal([]byte(wsj), &ws)

	var graph *SimulationRelationGraph
	if graphJSON != "" && graphJSON != "{}" {
		json.Unmarshal([]byte(graphJSON), &graph)
	}

	var initialRels []InitialRelationship
	if initialRelsJSON != "" && initialRelsJSON != "[]" {
		json.Unmarshal([]byte(initialRelsJSON), &initialRels)
	}

	state := &SimulationState{
		Config: SimulationConfig{
			ID:                 id,
			Topic:              topic,
			Description:        desc,
			Personas:           personas,
			WorldState:         ws,
			MaxWallClockMs:     maxWallClockMs,
			SimulatedHours:     simulatedHours,
			TickIntervalMs:     tickIntervalMs,
			TimeScale:          timeScale,
			EnableReflection:   enableReflection != 0,
			Language:           language,
			InitialRelationships: initialRels,
		},
		Status:       SimulationStatus(status),
		CurrentRound: currentRound,
		WorldState:   NewWorldState(ws),
		AgentStates:  make(map[string]*AgentState),
		Rounds:       make([]RoundResult, 0),
		Report:       report,
		Error:        errMsg,
		RunID:        id,
		Graph:        graph,
	}

	for _, p := range personas {
		state.AgentStates[p.ID] = &AgentState{PersonaID: p.ID, IsActive: true}
	}

	if startedAt.Valid {
		t, _ := parseTime(startedAt.String)
		state.StartedAt = &t
	}
	if completedAt.Valid {
		t, _ := parseTime(completedAt.String)
		state.CompletedAt = &t
	}
	if createdAt.Valid {
		state.CreatedAt, _ = parseTime(createdAt.String)
	}

	// Restore runtime relationships (snapshot from simulation completion)
	if relationshipsJSON != "" && relationshipsJSON != "[]" {
		var rels []RelationshipDTO
		if err := json.Unmarshal([]byte(relationshipsJSON), &rels); err == nil {
			state.Relationships = rels
		}
	}

	// Load rounds
	rows, err := s.db.Query(`SELECT round_number, messages_json, completed_at FROM simulation_rounds WHERE simulation_id = ? ORDER BY round_number`, id)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var rn int
			var mj, ca string
			if err := rows.Scan(&rn, &mj, &ca); err != nil {
				continue
			}
			var msgs []RoundMessage
			json.Unmarshal([]byte(mj), &msgs)
			cat, _ := parseTime(ca)
			state.Rounds = append(state.Rounds, RoundResult{
				RoundNumber: rn,
				Messages:    msgs,
				CompletedAt: cat,
			})
		}
	}

	return state, nil
}

func (s *SQLiteStore) List() []*SimulationState {
	rows, err := s.db.Query(`SELECT id FROM simulations ORDER BY created_at DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []*SimulationState
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		state, err := s.Get(id)
		if err != nil {
			continue
		}
		out = append(out, state)
	}
	return out
}

func (s *SQLiteStore) Update(id string, state *SimulationState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pj, _ := json.Marshal(state.Config.Personas)
	wsj, _ := json.Marshal(state.Config.WorldState)
	var startedAt, completedAt any
	if state.StartedAt != nil {
		startedAt = state.StartedAt.Format(timeFormat)
	}
	if state.CompletedAt != nil {
		completedAt = state.CompletedAt.Format(timeFormat)
	}

	graphJSON := "{}"
	if state.Graph != nil {
		if gj, err := json.Marshal(state.Graph); err == nil {
			graphJSON = string(gj)
		}
	}

	irJSON := "[]"
	if state.Config.InitialRelationships != nil {
		if irj, err := json.Marshal(state.Config.InitialRelationships); err == nil {
			irJSON = string(irj)
		}
	}

	relsJSON := "[]"
	if state.Relationships != nil {
		if rj, err := json.Marshal(state.Relationships); err == nil {
			relsJSON = string(rj)
		}
	}

	_, err := s.db.Exec(`UPDATE simulations SET status=?, world_state_json=?, report=?, error_msg=?,
		current_round=?, started_at=?, completed_at=?, topic=?, description=?, personas_json=?,
		max_wall_clock_ms=?, simulated_hours=?, tick_interval_ms=?, time_scale=?, enable_reflection=?, graph_json=?, language=?, initial_relationships_json=?, relationships_json=? WHERE id=?`,
		string(state.Status), string(wsj), state.Report, state.Error,
		state.CurrentRound, startedAt, completedAt, state.Config.Topic, state.Config.Description, string(pj),
		state.Config.MaxWallClockMs, state.Config.SimulatedHours, state.Config.TickIntervalMs, state.Config.TimeScale, boolToInt(state.Config.EnableReflection), graphJSON, state.Config.Language,
		irJSON, relsJSON, id)
	return err
}

func (s *SQLiteStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM simulations WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite store: delete: %w", err)
	}
	return nil
}

// SaveResults persists round results after simulation completes.
func (s *SQLiteStore) SaveResults(simID string, rounds []RoundResult, report string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, r := range rounds {
		mj, _ := json.Marshal(r.Messages)
		_, err := tx.Exec(`INSERT OR REPLACE INTO simulation_rounds (simulation_id, round_number, messages_json, started_at, completed_at)
			VALUES (?, ?, ?, ?, ?)`,
			simID, r.RoundNumber, string(mj), r.StartedAt.Format(timeFormat), r.CompletedAt.Format(timeFormat))
		if err != nil {
			return err
		}
	}

	if report != "" {
		_, err := tx.Exec(`UPDATE simulations SET report=? WHERE id=?`, report, simID)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// SaveAgentMemories persists agent memory records to the SQLite database.
func (s *SQLiteStore) SaveAgentMemories(simID string, personaID string, records []MemoryRecord) error {
	if len(records) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO agent_memories (simulation_id, persona_id, round, role, content, world_state_json, received_msgs_json, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, rec := range records {
		wsj, _ := json.Marshal(rec.WorldState)
		rmj, _ := json.Marshal(rec.ReceivedMsgs)
		_, err := stmt.Exec(simID, personaID, rec.Round, rec.Role, rec.Content, string(wsj), string(rmj), rec.Timestamp.Format(timeFormat))
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetAgentMemories retrieves agent memory records from the SQLite database.
func (s *SQLiteStore) GetAgentMemories(simID string, personaID string) ([]MemoryRecord, error) {
	rows, err := s.db.Query(`SELECT round, role, content, world_state_json, received_msgs_json, timestamp
		FROM agent_memories WHERE simulation_id = ? AND persona_id = ? ORDER BY round, timestamp`, simID, personaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []MemoryRecord
	for rows.Next() {
		var (
			round    int
			role     string
			content  string
			wsj, rmj string
			ts       string
		)
		if err := rows.Scan(&round, &role, &content, &wsj, &rmj, &ts); err != nil {
			continue
		}
		rec := MemoryRecord{
			Round:   round,
			Role:    role,
			Content: content,
		}
		json.Unmarshal([]byte(wsj), &rec.WorldState)
		json.Unmarshal([]byte(rmj), &rec.ReceivedMsgs)
		rec.Timestamp, _ = parseTime(ts)
		records = append(records, rec)
	}

	return records, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func newUUID() string {
	return uuid.NewString()
}

const timeFormat = "2006-01-02 15:04:05"

func parseTime(s string) (time.Time, error) {
	// Accept various formats
	formats := []string{timeFormat, "2006-01-02T15:04:05Z", "2006-01-02T15:04:05-07:00"}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time: %s", s)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
