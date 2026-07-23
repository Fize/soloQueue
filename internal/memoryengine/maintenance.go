package memoryengine

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type MemoryAuditReport struct {
	Total       int            `json:"total"`
	ByStatus    map[string]int `json:"by_status"`
	ByType      map[string]int `json:"by_type"`
	BySource    map[string]int `json:"by_source"`
	FTSRows     int            `json:"fts_rows"`
	VectorRows  int            `json:"vector_rows"`
	KGNodes     int            `json:"kg_nodes"`
	KGEdges     int            `json:"kg_edges"`
	OrphanEdges int            `json:"orphan_edges"`
}

type CleanupDecision struct {
	ContentHash   string  `json:"content_hash"`
	Action        string  `json:"action"`
	Reason        string  `json:"reason"`
	TargetHash    string  `json:"target_hash,omitempty"`
	MemoryType    string  `json:"memory_type"`
	ScopeType     string  `json:"scope_type"`
	ScopeID       string  `json:"scope_id"`
	CanonicalHash string  `json:"canonical_hash"`
	Confidence    float64 `json:"confidence"`
	Content       string  `json:"content"`
}

type CleanupManifest struct {
	GeneratedAt string            `json:"generated_at"`
	Decisions   []CleanupDecision `json:"decisions"`
}

func (e *Engine) Audit(ctx context.Context) (MemoryAuditReport, error) {
	report := MemoryAuditReport{
		ByStatus: make(map[string]int),
		ByType:   make(map[string]int),
		BySource: make(map[string]int),
	}
	if err := e.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mem_entries`).Scan(&report.Total); err != nil {
		return report, err
	}
	if err := countByColumn(ctx, e.db, "status", report.ByStatus); err != nil {
		return report, err
	}
	if err := countByColumn(ctx, e.db, "memory_type", report.ByType); err != nil {
		return report, err
	}
	if err := countByColumn(ctx, e.db, "source_type", report.BySource); err != nil {
		return report, err
	}
	countTable(ctx, e.db, "mem_fts", &report.FTSRows)
	countTable(ctx, e.db, "mem_vec", &report.VectorRows)
	countTable(ctx, e.db, "kg_nodes", &report.KGNodes)
	countTable(ctx, e.db, "kg_edges", &report.KGEdges)
	_ = e.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM kg_edges e
		WHERE e.source_hash != ''
		  AND NOT EXISTS (SELECT 1 FROM mem_entries m WHERE m.content_hash = e.source_hash)
	`).Scan(&report.OrphanEdges)
	return report, nil
}

func countByColumn(ctx context.Context, db *sql.DB, column string, out map[string]int) error {
	rows, err := db.QueryContext(ctx, `SELECT `+column+`, COUNT(*) FROM mem_entries GROUP BY `+column)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return err
		}
		out[key] = count
	}
	return rows.Err()
}

func countTable(ctx context.Context, db *sql.DB, table string, out *int) {
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(out)
}

func (e *Engine) PlanLegacyCleanup(ctx context.Context, projectRoot string) (CleanupManifest, error) {
	rows, err := e.db.QueryContext(ctx, `SELECT content_hash, content, tags, created_at
		FROM mem_entries
		WHERE source_type = 'legacy'
		ORDER BY created_at DESC, content_hash`)
	if err != nil {
		return CleanupManifest{}, err
	}
	defer rows.Close()

	var decisions []CleanupDecision
	seen := make(map[string]string)
	for rows.Next() {
		var hash, content, tags, createdAt string
		if err := rows.Scan(&hash, &content, &tags, &createdAt); err != nil {
			return CleanupManifest{}, err
		}
		decision := classifyLegacyMemory(hash, content, tags, projectRoot)
		key := decision.ScopeType + "\x00" + decision.ScopeID + "\x00" + decision.CanonicalHash
		if decision.Action == StatusActive {
			if target, ok := seen[key]; ok {
				decision.Action = StatusSuperseded
				decision.TargetHash = target
				decision.Reason = "normalized duplicate"
			} else {
				seen[key] = hash
			}
		}
		decisions = append(decisions, decision)
	}
	if err := rows.Err(); err != nil {
		return CleanupManifest{}, err
	}
	sort.Slice(decisions, func(i, j int) bool {
		if decisions[i].Action == decisions[j].Action {
			return decisions[i].ContentHash < decisions[j].ContentHash
		}
		return decisions[i].Action < decisions[j].Action
	})
	return CleanupManifest{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Decisions:   decisions,
	}, nil
}

var legacyProjectPathPattern = regexp.MustCompile(`/Users/[^\s]+/github\.com/[A-Za-z0-9_.-]+`)
var stockCodePattern = regexp.MustCompile(`(?:^|[^0-9])[036]\d{5}(?:[^0-9]|$)`)

func classifyLegacyMemory(hash, content, tags, projectRoot string) CleanupDecision {
	canonicalHash := hashContent(canonicalizeMemory(content))
	decision := CleanupDecision{
		ContentHash:   hash,
		Action:        StatusQuarantined,
		Reason:        "ambiguous legacy memory",
		MemoryType:    MemoryTypeLegacy,
		ScopeType:     ScopeGlobal,
		CanonicalHash: canonicalHash,
		Confidence:    0.5,
		Content:       content,
	}
	lower := strings.ToLower(content)
	if path := legacyProjectPathPattern.FindString(content); path != "" {
		decision.ScopeType = ScopeProject
		decision.ScopeID = filepath.Clean(path)
	} else if projectRoot != "" &&
		strings.Contains(lower, strings.ToLower(filepath.Base(projectRoot))) {
		decision.ScopeType = ScopeProject
		decision.ScopeID = filepath.Clean(projectRoot)
	}
	if isRoutineTaskRecord(content) || isTimeSensitiveReport(lower) ||
		strings.Contains(lower, "output:") || strings.Contains(lower, "构建通过") ||
		strings.Contains(lower, "测试通过") || strings.Contains(lower, "created ") ||
		strings.Contains(lower, "implemented ") || strings.Contains(lower, "修复了") ||
		strings.Contains(lower, "user requested evaluation") || stockCodePattern.MatchString(content) {
		decision.Action = StatusArchived
		decision.Reason = "routine or time-sensitive task record"
		decision.Confidence = 0.95
		return decision
	}
	if strings.Contains(lower, "用户偏好") || strings.Contains(lower, "user prefers") ||
		strings.Contains(lower, "user preference") || strings.Contains(lower, "用户喜欢") {
		decision.Action = StatusActive
		decision.Reason = "durable user preference"
		decision.MemoryType = MemoryTypePreference
		decision.Confidence = 0.9
		return decision
	}
	if strings.Contains(lower, "明确决定") || strings.Contains(lower, "决定不") ||
		strings.Contains(lower, "选用") || strings.Contains(lower, "chosen") ||
		strings.Contains(lower, "must use") {
		if len([]rune(content)) > 600 || strings.Count(content, "\n") > 8 {
			return decision
		}
		decision.Action = StatusActive
		decision.Reason = "project decision"
		decision.MemoryType = MemoryTypeDecision
		decision.Confidence = 0.75
		return decision
	}
	if strings.Contains(lower, "根因") || strings.Contains(lower, "root cause") ||
		strings.Contains(lower, "解决方案") || strings.Contains(lower, "修复方法") {
		if len([]rune(content)) > 800 {
			return decision
		}
		decision.Action = StatusActive
		decision.Reason = "reusable solution"
		decision.MemoryType = MemoryTypeReusableSolution
		decision.Confidence = 0.8
		return decision
	}
	if tags == "auto-compact,memory" && isStableLegacyFact(lower, content) {
		decision.Action = StatusActive
		decision.Reason = "explicit or compacted durable memory"
		decision.MemoryType = MemoryTypeStableFact
		decision.Confidence = 0.7
	}
	return decision
}

func isTimeSensitiveReport(lower string) bool {
	markers := []string{
		"investlab", "market regime", "market environment", "pre-market",
		"evening review", "risk assessment", "portfolio", "stock analysis",
		"上证", "深成", "创业板", "涨停", "跌停", "盘前", "复盘",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isStableLegacyFact(lower, content string) bool {
	if len([]rune(content)) > 300 || strings.Count(content, "\n") > 4 {
		return false
	}
	markers := []string{
		"使用", "位于", "存储", "持久化", "配置", "约束", "架构",
		" uses ", " located ", " stores ", " persistence", " configuration",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func (e *Engine) ApplyLegacyCleanup(ctx context.Context, manifest CleanupManifest) error {
	if len(manifest.Decisions) == 0 {
		return fmt.Errorf("cleanup manifest is empty")
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, decision := range manifest.Decisions {
		result, err := tx.ExecContext(ctx, `
			UPDATE mem_entries
			SET memory_type = ?, scope_type = ?, scope_id = ?,
			    source_type = 'migration', status = ?, confidence = ?,
			    supersedes_hash = ?, canonical_hash = ?, updated_at = ?
			WHERE content_hash = ? AND source_type = 'legacy'`,
			decision.MemoryType, decision.ScopeType, decision.ScopeID,
			decision.Action, decision.Confidence, decision.TargetHash,
			decision.CanonicalHash, now, decision.ContentHash,
		)
		if err != nil {
			return fmt.Errorf("apply cleanup %s: %w", decision.ContentHash, err)
		}
		affected, _ := result.RowsAffected()
		if affected != 1 {
			return fmt.Errorf("apply cleanup %s: expected one legacy row, updated %d", decision.ContentHash, affected)
		}
	}
	return tx.Commit()
}

// VacuumInto creates a consistent SQLite backup.
func (e *Engine) VacuumInto(ctx context.Context, backupPath string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	escaped := strings.ReplaceAll(backupPath, "'", "''")
	if _, err := e.db.ExecContext(ctx, `VACUUM INTO '`+escaped+`'`); err != nil {
		return fmt.Errorf("backup memory database: %w", err)
	}
	return nil
}
