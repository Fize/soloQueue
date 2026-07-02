package router

import (
	"regexp"
	"strings"
)

// ─── Weighted Pattern Scoring Classifier ─────────────────────────────────────
//
// FastTrackClassifier uses multi-pattern matching with weighted scoring to
// classify user prompts into processing levels (L0-L3).
//
// Architecture:
//   - Each level has a dictionary of weighted phrases (Chinese + English)
//   - Escalation/de-escalation modifiers adjust the final level
//   - Slash commands provide absolute overrides (confidence=100)
//   - When scoring is ambiguous (low score or close levels), returns low
//     confidence → signals the caller to invoke LLM fallback
//
// The classifier is designed for ALL task types (coding, writing, translation,
// deployment, design, etc.), not just code-related tasks.

// PatternRule defines a weighted phrase pattern for classification.
// English/ASCII patterns use precompiled \b-bounded regex; Chinese patterns use
// substring matching via strings.Contains (re == nil).
type PatternRule struct {
	Pattern string              // original pattern string
	Level   ClassificationLevel // which level this pattern votes for
	Weight  float64             // scoring weight: 0.5=weak, 1.0=normal, 1.5=medium, 2.0=strong
	re      *regexp.Regexp      // precompiled \b regex for ASCII patterns (nil = use Contains)
}

// EscalationRule defines a modifier that bumps classification up/down.
// Same dual-path matching as PatternRule.
type EscalationRule struct {
	Pattern string         // original pattern string
	Delta   int            // +1, +2, -1 etc.
	Weight  float64        // for compound scoring (used in confidence)
	re      *regexp.Regexp // precompiled \b regex for ASCII patterns (nil = use Contains)
}

// FastTrackClassifier implements rule-based classification using weighted scoring
type FastTrackClassifier struct {
	// Pattern dictionaries per level
	rules []PatternRule

	// Escalation/de-escalation modifiers
	escalationRules []EscalationRule

	// Slash command regex
	slashCommandRegex *regexp.Regexp

	// File path detection
	filePathRegex *regexp.Regexp

	// Scoring thresholds
	highConfidenceMinScore float64 // minimum score to be "confident" (default 3.0)
	highConfidenceRatio    float64 // max/second ratio for high confidence (default 2.0)
	medConfidenceMinScore  float64 // minimum score for medium confidence (default 2.0)
	medConfidenceRatio     float64 // max/second ratio for medium confidence (default 1.5)
}

// NewFastTrackClassifier creates and initializes a FastTrackClassifier
// with comprehensive multi-language pattern dictionaries
func NewFastTrackClassifier() *FastTrackClassifier {
	ftc := &FastTrackClassifier{
		slashCommandRegex: regexp.MustCompile(`^/([a-z][a-z0-9-]*)(?:\s|$)`),
		filePathRegex:     regexp.MustCompile(`(?:^|\s)((?:\.{0,2}/[a-zA-Z0-9_.\-/]+(?:\.[a-zA-Z0-9]+)?|[a-zA-Z0-9_][a-zA-Z0-9_.\-/]*\.[a-zA-Z0-9]+))`),

		highConfidenceMinScore: 3.0,
		highConfidenceRatio:    2.0,
		medConfidenceMinScore:  2.0,
		medConfidenceRatio:     1.5,
	}

	ftc.rules = buildPatternRules()
	ftc.escalationRules = buildEscalationRules()

	return ftc
}

// Classify performs fast-track classification on a user prompt
func (ftc *FastTrackClassifier) Classify(prompt string) ClassificationResult {
	result := ClassificationResult{
		Level:                LevelSimpleSingleFile, // safe default
		Confidence:           0,
		RequiresConfirmation: false,
	}

	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		result.Level = LevelConversation
		result.Confidence = 95
		result.Reason = "Empty prompt"
		return result
	}

	normalized := strings.ToLower(trimmed)

	// ── Phase 1: Slash command override (highest priority, confidence=100) ──
	if slashMatch := ftc.slashCommandRegex.FindStringSubmatch(normalized); len(slashMatch) > 1 {
		cmd := slashMatch[1]
		result.SlashCommand = cmd
		if r, ok := ftc.classifySlashCommand(cmd, prompt); ok {
			return r
		}
		// Unknown slash command: fall through to scoring
	}

	// ── Phase 2: Detect file paths (used as contextual signal) ──
	filePaths := ftc.extractFilePaths(prompt)
	result.DetectedFilePaths = filePaths

	// ── Phase 3: Weighted pattern scoring ──
	scores := ftc.scorePrompt(normalized)

	// ── Phase 4: Apply file count as boost signal ──
	fileCount := len(filePaths)
	if fileCount == 1 {
		scores[LevelSimpleSingleFile] += 1.0
	} else if fileCount >= 2 && fileCount <= 4 {
		scores[LevelMediumMultiFile] += 2.5 // strong signal: multiple files = multi-step task
	} else if fileCount >= 5 {
		scores[LevelComplexRefactoring] += 3.0
	}

	// ── Phase 5: Apply escalation/de-escalation modifiers ──
	escalation := ftc.computeEscalation(normalized)

	// ── Phase 6: Determine level from scores ──
	maxLevel, maxScore, secondScore := ftc.pickTopLevel(scores)

	// Apply escalation
	finalLevel := int(maxLevel) + escalation
	finalLevel = max(finalLevel, int(LevelConversation))
	finalLevel = min(finalLevel, int(LevelComplexRefactoring))
	result.Level = ClassificationLevel(finalLevel)

	// ── Phase 7: Compute confidence ──
	result.Confidence = ftc.computeConfidence(maxScore, secondScore, escalation)
	result.Reason = ftc.buildReason(result.Level, maxScore, fileCount, escalation)

	// ── Phase 8: Check dangerous patterns ──
	if ftc.containsDangerousPattern(normalized) {
		result.RequiresConfirmation = true
		result.ConfirmationMessage = "This command appears to involve destructive operations. Proceed?"
	}

	return result
}

// ─── Slash Command Handling ──────────────────────────────────────────────────

// classifySlashCommand handles explicit level-force commands.
// Returns (result, true) for known commands, (zero, false) for unknown.
func (ftc *FastTrackClassifier) classifySlashCommand(cmd string, fullPrompt string) (ClassificationResult, bool) {
	result := ClassificationResult{
		SlashCommand: cmd,
		Confidence:   100,
	}

	switch cmd {
	// ── Explicit level overrides ──
	case "l0", "chat":
		result.Level = LevelConversation
		result.Reason = "Explicit /l0 or /chat: force conversation level"
	case "l1":
		result.Level = LevelSimpleSingleFile
		result.Reason = "Explicit /l1: force simple task level"
	case "l2":
		result.Level = LevelMediumMultiFile
		result.Reason = "Explicit /l2: force multi-step task level"
	case "l3", "max", "expert":
		result.Level = LevelComplexRefactoring
		result.Reason = "Explicit level override: force expert level"
	case "fast":
		result.Level = LevelConversation
		result.Reason = "Explicit /fast: force fast model (no thinking)"

	// ── Contextual slash commands (confidence < 100, scored by heuristic) ──
	case "read", "cat", "view":
		result.Level = LevelSimpleSingleFile
		result.Confidence = 95
		result.Reason = "Slash command /read: single-file read operation"
	case "write", "edit", "modify":
		filePaths := ftc.extractFilePaths(fullPrompt)
		if len(filePaths) <= 1 {
			result.Level = LevelSimpleSingleFile
			result.Confidence = 90
			result.Reason = "Slash command /write: single file modification"
		} else {
			result.Level = LevelMediumMultiFile
			result.Confidence = 85
			result.Reason = "Slash command /write: multiple files"
		}
	case "refactor":
		filePaths := ftc.extractFilePaths(fullPrompt)
		if len(filePaths) >= 5 {
			result.Level = LevelComplexRefactoring
			result.Confidence = 90
			result.Reason = "Slash command /refactor: many files → complex"
		} else {
			result.Level = LevelMediumMultiFile
			result.Confidence = 85
			result.Reason = "Slash command /refactor: multi-file changes"
		}
	case "implement":
		result.Level = LevelMediumMultiFile
		result.Confidence = 80
		result.Reason = "Slash command /implement: multi-step implementation"
	case "test", "debug":
		filePaths := ftc.extractFilePaths(fullPrompt)
		if len(filePaths) <= 2 {
			result.Level = LevelSimpleSingleFile
			result.Confidence = 85
			result.Reason = "Slash command /test or /debug: limited scope"
		} else {
			result.Level = LevelMediumMultiFile
			result.Confidence = 80
			result.Reason = "Slash command /test or /debug: multiple files"
		}
	default:
		return ClassificationResult{}, false
	}

	return result, true
}

// ─── Pattern Scoring Engine ──────────────────────────────────────────────────

// scorePrompt scans the normalized prompt and accumulates weighted scores per level.
// English patterns use word-boundary regex; Chinese patterns use substring matching.
func (ftc *FastTrackClassifier) scorePrompt(normalized string) map[ClassificationLevel]float64 {
	scores := map[ClassificationLevel]float64{
		LevelConversation:       0,
		LevelSimpleSingleFile:   0,
		LevelMediumMultiFile:    0,
		LevelComplexRefactoring: 0,
	}

	for _, rule := range ftc.rules {
		if matchPattern(rule, normalized) {
			scores[rule.Level] += rule.Weight
		}
	}

	return scores
}

// computeEscalation scans for escalation/de-escalation modifiers and returns net delta.
// English patterns use word-boundary regex; Chinese patterns use substring matching.
func (ftc *FastTrackClassifier) computeEscalation(normalized string) int {
	totalDelta := 0
	for _, rule := range ftc.escalationRules {
		if matchEscalation(rule, normalized) {
			totalDelta += rule.Delta
		}
	}
	// Clamp to [-2, +2]
	totalDelta = max(totalDelta, -2)
	totalDelta = min(totalDelta, 2)
	return totalDelta
}

// pickTopLevel finds the level with highest score and the second-highest score
func (ftc *FastTrackClassifier) pickTopLevel(scores map[ClassificationLevel]float64) (ClassificationLevel, float64, float64) {
	type entry struct {
		level ClassificationLevel
		score float64
	}
	ordered := []entry{
		{LevelConversation, scores[LevelConversation]},
		{LevelSimpleSingleFile, scores[LevelSimpleSingleFile]},
		{LevelMediumMultiFile, scores[LevelMediumMultiFile]},
		{LevelComplexRefactoring, scores[LevelComplexRefactoring]},
	}

	// Find max and second max
	maxIdx := 0
	for i := 1; i < len(ordered); i++ {
		if ordered[i].score > ordered[maxIdx].score {
			maxIdx = i
		}
	}

	secondMax := 0.0
	for i, e := range ordered {
		if i != maxIdx && e.score > secondMax {
			secondMax = e.score
		}
	}

	// If all scores are 0, default to L1
	if ordered[maxIdx].score == 0 {
		return LevelSimpleSingleFile, 0, 0
	}

	return ordered[maxIdx].level, ordered[maxIdx].score, secondMax
}

// computeConfidence determines confidence percentage based on scoring clarity
func (ftc *FastTrackClassifier) computeConfidence(maxScore, secondScore float64, escalation int) int {
	if maxScore == 0 {
		return 30 // Very uncertain, needs LLM
	}

	// Ratio of separation between top and second
	ratio := 0.0
	if secondScore > 0 {
		ratio = maxScore / secondScore
	} else {
		ratio = maxScore * 3 // No competition = very clear
	}

	var confidence int
	switch {
	case maxScore >= ftc.highConfidenceMinScore && ratio >= ftc.highConfidenceRatio:
		confidence = 90 + int(maxScore) // 90-100
	case maxScore >= ftc.medConfidenceMinScore && ratio >= ftc.medConfidenceRatio:
		confidence = 75 + int(maxScore*3) // 75-89
	case maxScore >= 1.5:
		confidence = 60 + int(maxScore*5) // 60-74
	default:
		confidence = 30 + int(maxScore*20) // 30-59
	}

	// Escalation keywords boost confidence (user expressed clear intent)
	if escalation != 0 {
		confidence += 10
	}

	if confidence > 100 {
		confidence = 100
	}
	if confidence < 0 {
		confidence = 0
	}
	return confidence
}

// buildReason constructs a human-readable reason for the classification
func (ftc *FastTrackClassifier) buildReason(level ClassificationLevel, _ float64, fileCount int, escalation int) string {
	var parts []string

	switch level {
	case LevelConversation:
		parts = append(parts, "Conversation/Q&A patterns detected")
	case LevelSimpleSingleFile:
		parts = append(parts, "Simple single-step task")
	case LevelMediumMultiFile:
		parts = append(parts, "Multi-step coordination task")
	case LevelComplexRefactoring:
		parts = append(parts, "Complex deep-reasoning task")
	}

	if fileCount > 0 {
		parts = append(parts, formatFileCount(fileCount))
	}

	if escalation > 0 {
		parts = append(parts, "[escalated by intent keywords]")
	} else if escalation < 0 {
		parts = append(parts, "[de-escalated by simplicity keywords]")
	}

	return strings.Join(parts, "; ")
}

func formatFileCount(n int) string {
	if n == 1 {
		return "1 file detected"
	}
	return strings.Replace("N files detected", "N", strings.TrimSpace(strings.Repeat(" ", 0)+itoa(n)), 1)
}

func itoa(n int) string {
	if n < 0 {
		return "-" + itoa(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

// ─── Utility Methods ─────────────────────────────────────────────────────────

// extractFilePaths finds all file paths in the prompt
func (ftc *FastTrackClassifier) extractFilePaths(prompt string) []string {
	matches := ftc.filePathRegex.FindAllStringSubmatch(prompt, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var paths []string
	for _, m := range matches {
		if len(m) > 1 {
			clean := strings.TrimSpace(m[1])
			if !seen[clean] && clean != "" {
				paths = append(paths, clean)
				seen[clean] = true
			}
		}
	}
	return paths
}

// containsDangerousPattern checks if the prompt contains potentially dangerous operations
func (ftc *FastTrackClassifier) containsDangerousPattern(normalized string) bool {
	dangerousPatterns := []string{
		"rm -rf", "rm -r ", "rmdir",
		"drop table", "drop database", "delete from",
		"truncate table",
		"format c:", "mkfs", "dd if=",
		":(){ :|:& };:", // fork bomb
	}
	for _, p := range dangerousPatterns {
		if strings.Contains(normalized, p) {
			return true
		}
	}
	return false
}

// ─── Compilation Helpers ─────────────────────────────────────────────────────

// isASCIIOnly returns true when every rune in s is ASCII (≤ 127).
func isASCIIOnly(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

// compilePattern precompiles an ASCII pattern into a word-boundary regex.
// Non-ASCII (Chinese) patterns return nil so the engine falls back to strings.Contains.
func compilePattern(p string) *regexp.Regexp {
	if !isASCIIOnly(p) {
		return nil
	}
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(p) + `\b`)
}

// matchPattern returns true when the rule fires against the lowercased prompt.
func matchPattern(r PatternRule, normalized string) bool {
	if r.re != nil {
		return r.re.MatchString(normalized)
	}
	return strings.Contains(normalized, r.Pattern)
}

// matchEscalation returns true when the escalation rule fires.
func matchEscalation(r EscalationRule, normalized string) bool {
	if r.re != nil {
		return r.re.MatchString(normalized)
	}
	return strings.Contains(normalized, r.Pattern)
}

// ─── Pattern Dictionary ──────────────────────────────────────────────────────

func buildPatternRules() []PatternRule {
	var rules []PatternRule

	// ── L0: Conversation / Q&A / Information Exchange ──
	// These patterns indicate the user is asking a question, not requesting action
	l0 := []struct {
		p string
		w float64
	}{
		{"what does it mean", 1.5},
		{"what is the difference", 1.5},
		{"principle is", 1.5},
		{"concept", 1.0},
		{"definition", 1.0},
		{"explain it", 1.5},
		{"introduce", 1.5},
		{"do you think", 1.5},
		{"do you consider", 1.5},
		{"how to see", 1.5},
		{"any recommendations", 1.5},
		{"can explain", 1.5},
		{"suitable for what scenario", 1.0},
		{"give an example", 1.5},
		{"meaning", 1.0},
		{"who is", 1.0},
		{"where", 1.0},
		{"talk about", 1.5},
		{"science popularization", 1.5},
		{"summarize", 1.0},
		{"induction", 1.0},
		{"what is", 1.5},
		{"what are", 1.5},
		{"what does", 1.5},
		{"why", 1.0},
		{"how does", 1.5},
		{"how do", 1.0},
		{"explain", 2.0},
		{"describe", 1.5},
		{"tell me about", 1.5},
		{"difference between", 1.5},
		{"compare", 1.5},
		{"pros and cons", 1.5},
		{"what do you think", 1.5},
		{"recommend", 1.0},
		{"which is better", 1.5},
		{"meaning of", 1.5},
		{"definition of", 1.5},
		{"example of", 1.5},
		{"when to use", 1.0},
		{"overview", 1.0},
		{"summary", 1.0},
		{"can you explain", 2.0},
		{"help me understand", 1.5},
		{"what's the", 1.0},
		{"how to understand", 1.5},
		{"teach me", 1.5},
		{"walk me through", 1.5},
		{"hello", 1.5},
		{"hi", 1.0},
		{"hey", 1.0},
		{"how are you", 2.0},
		{"good morning", 1.5},
		{"good evening", 1.5},
		{"thank you", 1.5},
		{"thanks", 1.0},
	}
	for _, p := range l0 {
		rules = append(rules, PatternRule{Pattern: p.p, Level: LevelConversation, Weight: p.w, re: compilePattern(p.p)})
	}

	// ── L1: Simple / Single-Step Tasks ──
	// Clear single action with clear target
	l1 := []struct {
		p string
		w float64
	}{
		{"modify", 1.0},
		{"modify it", 1.5},
		{"change", 1.5},
		{"add one", 1.5},
		{"newly add", 1.0},
		{"supplement", 1.0},
		{"take out", 1.5},
		{"swap out", 1.5},
		{"tweak it", 1.5},
		{"fine-tune", 1.5},
		{"fine-tune it", 1.5},
		{"adjust", 1.0},
		{"rerun", 1.5},
		{"rerun it", 1.5},
		{"write one", 1.0},
		{"new", 1.0},
		{"sort", 1.5},
		{"move to", 1.5},
		{"take a look", 1.0},
		{"test it", 1.0},
		{"paste", 1.0},
		{"fix", 1.5},
		{"fix bug", 2.0},
		{"fix the", 1.5},
		{"add", 1.0},
		{"create", 1.0},
		{"make", 0.8},
		{"update", 1.0},
		{"delete", 1.0},
		{"remove", 1.0},
		{"replace", 1.0},
		{"rename", 1.5},
		{"move", 1.0},
		{"copy", 1.0},
		{"format", 1.5},
		{"translate", 1.5},
		{"convert", 1.0},
		{"extract", 1.0},
		{"write a", 1.0},
		{"generate", 1.0},
		{"change to", 1.5},
		{"typo", 2.0},
		{"spelling", 1.5},
		{"syntax error", 1.5},
		{"run", 1.0},
		{"execute", 1.0},
		{"start", 1.0},
		{"stop", 1.0},
		{"install", 1.0},
		{"uninstall", 1.0},
		{"upgrade", 1.0},
		{"find", 1.0},
		{"search", 1.0},
		{"locate", 1.0},
		{"print", 1.0},
		{"output", 1.0},
		{"display", 1.0},
		{"clean up", 1.0},
		{"clear", 1.0},
		{"check", 1.0},
		{"verify", 1.0},
		{"validate", 1.0},
	}
	for _, p := range l1 {
		rules = append(rules, PatternRule{Pattern: p.p, Level: LevelSimpleSingleFile, Weight: p.w, re: compilePattern(p.p)})
	}

	// ── L2: Multi-Step / Coordination Tasks ──
	// Requires planning, multiple related changes, cross-concern coordination
	l2 := []struct {
		p string
		w float64
	}{
		{"interface with", 1.5},
		{"develop feature", 1.5},
		{"design plan", 1.5},
		{"make a plan", 1.5},
		{"planning", 1.5},
		{"cross-component", 2.0},
		{"unified", 1.0},
		{"standardization", 1.5},
		{"normalization", 1.5},
		{"joint debugging", 1.5},
		{"adaptation", 1.5},
		{"compatibility", 1.0},
		{"interface design", 1.5},
		{"data model", 1.5},
		{"table structure", 1.0},
		{"process", 1.0},
		{"deployment", 1.0},
		{"environment setup", 1.5},
		{"configure environment", 1.5},
		{"supplement tests", 1.0},
		{"unit tests", 1.0},
		{"at the same time", 1.0},
		{"also", 1.0},
		{"encapsulation", 1.5},
		{"abstraction", 1.5},
		{"modularization", 1.5},
		{"optimization", 1.0},
		{"improvement", 1.0},
		{"cache", 1.0},
		{"automation", 1.5},
		{"comments", 0.8},
		{"refactor", 2.0},
		{"migrate", 2.0},
		{"integrate", 1.5},
		{"implement", 1.5},
		{"implement feature", 2.0},
		{"new feature", 1.5},
		{"design", 1.5},
		{"plan", 1.0},
		{"architect", 1.5},
		{"multiple files", 2.0},
		{"across", 1.5},
		{"cross-module", 2.0},
		{"batch", 1.5},
		{"standardize", 1.5},
		{"normalize", 1.5},
		{"workflow", 1.5},
		{"pipeline", 1.5},
		{"deploy", 1.0},
		{"end-to-end", 1.5},
		{"full-stack", 1.5},
		{"test coverage", 1.5},
		{"ci/cd", 1.5},
		{"ci cd", 1.5},
		{"modularize", 1.5},
		{"abstract", 1.5},
		{"encapsulate", 1.5},
		{"split", 1.5},
		{"merge", 1.0},
		{"combine", 1.0},
		{"optimize", 1.0},
		{"improve", 1.0},
		{"upgrade to", 1.5},
		{"database", 1.0},
		{"caching", 1.0},
		{"message queue", 1.5},
		{"automate", 1.5},
		{"scripting", 1.5},
		{"documentation", 1.0},
		{"set up", 1.0},
		{"configure", 1.0},
		{"scaffold", 1.5},
		{"both", 0.8},
		{"and also", 0.8},
		{"as well as", 0.8},
	}
	for _, p := range l2 {
		rules = append(rules, PatternRule{Pattern: p.p, Level: LevelMediumMultiFile, Weight: p.w, re: compilePattern(p.p)})
	}

	// ── L3: Complex / Deep-Reasoning Tasks ──
	// High uncertainty, architectural decisions, deep debugging, systemic changes
	l3 := []struct {
		p string
		w float64
	}{
		{"re-architect", 2.0},
		{"tech stack selection", 1.5},
		{"global", 1.5},
		{"thorough", 1.5},
		{"start over", 2.0},
		{"rebuild from ground up", 2.0},
		{"security hardening", 1.5},
		{"root cause analysis", 2.0},
		{"in-depth investigation", 2.0},
		{"difficult", 1.5},
		{"bizarre", 1.5},
		{"complex", 1.0},
		{"high concurrency", 1.5},
		{"data consistency", 1.5},
		{"transaction", 1.0},
		{"concurrency issue", 1.5},
		{"tech debt", 1.5},
		{"legacy code", 1.5},
		{"full refactor", 2.0},
		{"underlying overhaul", 2.0},
		{"core module", 1.5},
		{"system", 1.5},
		{"framework", 1.0},
		{"disaster recovery", 1.5},
		{"degradation", 1.0},
		{"full link", 2.0},
		{"end-to-end", 1.5},
		{"design patterns", 1.5},
		{"architecture patterns", 2.0},
		{"rewrite", 2.0},
		{"redesign", 2.0},
		{"rearchitect", 2.0},
		{"architecture", 1.5},
		{"system design", 2.0},
		{"from scratch", 2.0},
		{"entire system", 2.0},
		{"entire app", 2.0},
		{"entire application", 2.0},
		{"comprehensive", 1.5},
		{"overhaul", 2.0},
		{"performance optimization", 1.5},
		{"security audit", 2.0},
		{"root cause", 2.0},
		{"deep dive", 1.5},
		{"investigate", 1.5},
		{"large scale", 1.5},
		{"distributed", 1.5},
		{"concurrency", 1.5},
		{"memory leak", 1.5},
		{"race condition", 2.0},
		{"deadlock", 1.5},
		{"technical debt", 1.5},
		{"legacy", 1.5},
		{"legacy system", 2.0},
		{"high availability", 1.5},
		{"fault tolerance", 1.5},
		{"design pattern", 1.5},
		{"architecture pattern", 2.0},
		{"infrastructure", 1.5},
		{"foundation", 1.0},
		{"end to end", 1.5},
		{"full rewrite", 2.0},
		{"system-wide", 2.0},
		{"globally", 1.5},
		{"everywhere", 1.5},
		{"ground up", 2.0},
		{"fundamental", 1.5},
	}
	for _, p := range l3 {
		rules = append(rules, PatternRule{Pattern: p.p, Level: LevelComplexRefactoring, Weight: p.w, re: compilePattern(p.p)})
	}

	return rules
}

// buildEscalationRules creates the escalation/de-escalation modifier rules
func buildEscalationRules() []EscalationRule {
	var rules []EscalationRule

	// ── Escalation: user explicitly requests deeper thinking ──
	// These OVERRIDE physical heuristics and bump the level up
	escalation := []struct {
		p string
		d int
	}{
		{"in-depth analysis", 1},
		{"deep thinking", 1},
		{"careful analysis", 1},
		{"serious consideration", 1},
		{"prudent", 1},
		{"comprehensive consideration", 1},
		{"detailed analysis", 1},
		{"thoroughly solve", 2},
		{"root cause fix", 2},
		{"think it over", 1},
		{"think more", 1},
		{"repeatedly deliberate", 1},
		{"then", 1},
		{"continue", 1},
		{"again", 1},
		{"once more", 1},
		{"think carefully", 1},
		{"think hard", 1},
		{"be thorough", 1},
		{"in depth", 1},
		{"in-depth", 1},
		{"deeply", 1},
		{"once and for all", 2},
		{"definitive", 1},
		{"exhaustive", 1},
		{"take your time", 1},
		{"no rush", 1},
	}
	for _, e := range escalation {
		rules = append(rules, EscalationRule{Pattern: e.p, Delta: e.d, Weight: 1.5, re: compilePattern(e.p)})
	}

	// ── De-escalation: user signals simplicity ──
	deescalation := []struct {
		p string
		d int
	}{
		{"casual", -1},
		{"just tweak it", -1},
		{"offhand", -1},
		{"by the way", -1},
		{"no need to be too complex", -1},
		{"good enough", -1},
		{"just", -1},
		{"quick", -1},
		{"briefly", -1},
		{"rough", -1},
		{"simple", -1},
		{"easy", -1},
		{"straightforward", -1},
		{"don't overthink", -1},
		{"keep it simple", -1},
		{"simply", -1},
	}
	for _, e := range deescalation {
		rules = append(rules, EscalationRule{Pattern: e.p, Delta: e.d, Weight: 1.0, re: compilePattern(e.p)})
	}

	return rules
}
