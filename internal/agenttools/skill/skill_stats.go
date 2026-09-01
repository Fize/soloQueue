package skill

import (
	"context"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/db"
)

// ─── Invocation Statistics ──────────────────────────────────────────────────
//
// Skill invocation telemetry; consumed by listing ordering and governance
// reports.

// InvocationResult is the outcome of a Skill tool call.
type InvocationResult string

const (
	InvocationOK       InvocationResult = "ok"
	InvocationNotFound InvocationResult = "not_found"
	InvocationFork     InvocationResult = "fork"
	InvocationError    InvocationResult = "error"
)

// InvocationEvent is one Skill tool call.
type InvocationEvent struct {
	AgentID  string
	SkillID  string
	Args     string
	Result   InvocationResult
	Duration time.Duration
}

// InvocationStats records and aggregates Skill invocations.
type InvocationStats interface {
	Record(ctx context.Context, e InvocationEvent) error
	// Counts returns per-skill invocation counts since the cutoff (inclusive).
	Counts(ctx context.Context, since time.Time) (map[string]int, error)
}

// SQLiteInvocationStats persists events in the shared SQLite database
// (table skill_invocations, migrated in internal/infra/db).
type SQLiteInvocationStats struct {
	db *db.DB
}

// NewSQLiteInvocationStats wraps a shared DB connection.
func NewSQLiteInvocationStats(d *db.DB) *SQLiteInvocationStats {
	return &SQLiteInvocationStats{db: d}
}

// Record writes one event. Failures are logged by callers — telemetry must
// never block skill execution.
func (s *SQLiteInvocationStats) Record(ctx context.Context, e InvocationEvent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO skill_invocations (agent_id, skill_id, args, result, duration_ms) VALUES (?, ?, ?, ?, ?)`,
		e.AgentID, e.SkillID, e.Args, string(e.Result), e.Duration.Milliseconds())
	return err
}

func (s *SQLiteInvocationStats) Counts(ctx context.Context, since time.Time) (map[string]int, error) {
	// Only successful invocations (inline ok or fork) count as usage: errors
	// are execution failures, not evidence the skill was used, and would push
	// broken skills up the listing order. Governance's never-invoked list then
	// also surfaces skills that only ever failed.
	// created_at is stored as UTC datetime('now'); compare in the same textual
	// format to keep the index on (skill_id, created_at) usable.
	rows, err := s.db.QueryContext(ctx,
		`SELECT skill_id, COUNT(*) FROM skill_invocations WHERE result IN ('ok','fork') AND created_at >= ? GROUP BY skill_id`,
		since.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		counts[id] = n
	}
	return counts, rows.Err()
}
