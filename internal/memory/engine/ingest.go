package engine

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const maxAutomaticMemoryChars = 1200

var (
	markdownHeadingPattern = regexp.MustCompile(`(?m)^\s*#{1,6}\s+`)
	spacePattern           = regexp.MustCompile(`\s+`)
	entityTypePattern      = regexp.MustCompile(`[^a-z0-9_]+`)
)

var allowedEntityTypes = map[string]bool{
	"entity": true, "person": true, "project": true, "concept": true,
	"tool": true, "file": true, "decision": true, "configuration": true,
	"metric": true, "component": true, "event": true, "location": true,
	"organization": true,
}

// Ingest applies the long-term memory write policy and stores accepted
// candidates. Routine task logs stay in the timeline and short-term memory.
func (e *Engine) Ingest(ctx context.Context, candidate MemoryCandidate) (IngestResult, error) {
	normalized, reason := normalizeCandidate(candidate)
	if reason != "" {
		return IngestResult{Action: "skip", Reason: reason}, nil
	}
	if !normalized.ExplicitUserRequest &&
		(normalized.SourceType == SourceAgent || normalized.SourceType == SourceCompaction) &&
		isRoutineTaskRecord(normalized.Content) {
		return IngestResult{Action: "skip", Reason: "routine task result"}, nil
	}

	canonicalHash := hashContent(canonicalizeMemory(normalized.Content))
	hash, isNew, err := e.store.saveCandidate(ctx, normalized, canonicalHash)
	if err != nil {
		return IngestResult{}, err
	}
	if isNew && len(normalized.Entities) > 0 {
		e.indexEntities(ctx, normalized.Content, hash, normalized.EventTime, normalized.Entities)
	}
	action := "insert"
	if !isNew {
		action = "skip"
		reason = "duplicate canonical memory in scope"
	}
	return IngestResult{
		Action:      action,
		ContentHash: hash,
		IsNew:       isNew,
		Reason:      reason,
	}, nil
}

func normalizeCandidate(candidate MemoryCandidate) (MemoryCandidate, string) {
	candidate.Content = strings.TrimSpace(candidate.Content)
	if candidate.Content == "" {
		return candidate, "empty content"
	}
	if !candidate.ExplicitUserRequest && candidate.SourceType != SourceSimulation &&
		len([]rune(candidate.Content)) > maxAutomaticMemoryChars {
		return candidate, "automatic memory exceeds size limit"
	}

	switch candidate.MemoryType {
	case MemoryTypePreference, MemoryTypeDecision, MemoryTypeStableFact, MemoryTypeReusableSolution:
	default:
		return candidate, "unsupported memory type"
	}
	switch candidate.ScopeType {
	case ScopeGlobal:
		candidate.ScopeID = ""
	case ScopeProject, ScopeTeam, ScopeSimulation:
		candidate.ScopeID = strings.TrimSpace(candidate.ScopeID)
		if candidate.ScopeID == "" {
			return candidate, "non-global memory requires scope id"
		}
	default:
		return candidate, "unsupported scope type"
	}
	switch candidate.SourceType {
	case SourceExplicit, SourceAgent, SourceCompaction, SourceMigration, SourceSimulation:
	default:
		return candidate, "unsupported source type"
	}

	now := time.Now()
	if candidate.Date == "" {
		candidate.Date = now.Format("2006-01-02")
	}
	if candidate.EventTime == "" {
		candidate.EventTime = now.Format(time.RFC3339)
	}
	if candidate.Confidence <= 0 {
		candidate.Confidence = 1.0
	}
	if candidate.Confidence > 1 {
		candidate.Confidence = 1
	}
	if candidate.ExpiresAt != "" {
		if _, err := time.Parse(time.RFC3339, candidate.ExpiresAt); err != nil {
			return candidate, fmt.Sprintf("invalid expires_at: %v", err)
		}
	}
	return candidate, ""
}

func canonicalizeMemory(content string) string {
	content = markdownHeadingPattern.ReplaceAllString(content, "")
	content = strings.ToLower(strings.TrimSpace(content))
	return spacePattern.ReplaceAllString(content, " ")
}

func isRoutineTaskRecord(content string) bool {
	lower := strings.ToLower(content)
	routineMarkers := []string{
		"排版完成", "报告完成", "任务完成", "completed:", "completed ",
		"build completed", "tests passed", "test passed", "commit and push",
		"git commit", "输出 300dpi", "output file:", "午间复盘", "晚间复盘",
		"盘前准备报告",
	}
	for _, marker := range routineMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func normalizeEntityType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = entityTypePattern.ReplaceAllString(value, "_")
	value = strings.Trim(value, "_")
	if !allowedEntityTypes[value] {
		return "entity"
	}
	return value
}
