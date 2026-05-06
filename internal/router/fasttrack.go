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

// PatternRule defines a weighted phrase pattern for classification
type PatternRule struct {
	Pattern string              // phrase to match (case-insensitive for English)
	Level   ClassificationLevel // which level this pattern votes for
	Weight  float64             // scoring weight: 0.5=weak, 1.0=normal, 1.5=medium, 2.0=strong
}

// EscalationRule defines a modifier that bumps classification up/down
type EscalationRule struct {
	Pattern string  // phrase to match
	Delta   int     // +1, +2, -1 etc.
	Weight  float64 // for compound scoring (used in confidence)
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

// scorePrompt scans the normalized prompt and accumulates weighted scores per level
func (ftc *FastTrackClassifier) scorePrompt(normalized string) map[ClassificationLevel]float64 {
	scores := map[ClassificationLevel]float64{
		LevelConversation:       0,
		LevelSimpleSingleFile:   0,
		LevelMediumMultiFile:    0,
		LevelComplexRefactoring: 0,
	}

	for _, rule := range ftc.rules {
		if strings.Contains(normalized, rule.Pattern) {
			scores[rule.Level] += rule.Weight
		}
	}

	return scores
}

// computeEscalation scans for escalation/de-escalation modifiers and returns net delta
func (ftc *FastTrackClassifier) computeEscalation(normalized string) int {
	totalDelta := 0
	for _, rule := range ftc.escalationRules {
		if strings.Contains(normalized, rule.Pattern) {
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

// ─── Pattern Dictionary ──────────────────────────────────────────────────────

func buildPatternRules() []PatternRule {
	var rules []PatternRule

	// ── L0: Conversation / Q&A / Information Exchange ──
	// These patterns indicate the user is asking a question, not requesting action
	l0 := []struct {
		p string
		w float64
	}{
		// Chinese question patterns
		{"是什么", 1.5}, {"为什么", 1.5}, {"什么意思", 1.5}, {"怎么理解", 1.5},
		{"有什么区别", 1.5}, {"原理是", 1.5}, {"概念", 1.0}, {"定义", 1.0},
		{"解释一下", 2.0}, {"说明一下", 1.5}, {"介绍一下", 1.5}, {"描述一下", 1.5},
		{"你觉得", 1.5}, {"你认为", 1.5}, {"对比一下", 1.5}, {"优缺点", 1.5},
		{"怎么看", 1.5}, {"有没有推荐", 1.5}, {"哪个更好", 1.5},
		{"能解释", 1.5}, {"帮我理解", 1.5}, {"教我", 1.5},
		{"什么时候用", 1.0}, {"适合什么场景", 1.0}, {"举个例子", 1.5},
		{"区别是什么", 1.5}, {"含义", 1.0}, {"概览", 1.0},
		{"谁是", 1.0}, {"哪里", 1.0}, {"什么是", 1.5},
		{"如何理解", 1.5}, {"讲讲", 1.5}, {"科普", 1.5},
		{"总结一下", 1.0}, {"归纳", 1.0},
		// English question patterns
		{"what is", 1.5}, {"what are", 1.5}, {"what does", 1.5},
		{"why", 1.0}, {"how does", 1.5}, {"how do", 1.0},
		{"explain", 2.0}, {"describe", 1.5}, {"tell me about", 1.5},
		{"difference between", 1.5}, {"compare", 1.5}, {"pros and cons", 1.5},
		{"what do you think", 1.5}, {"recommend", 1.0}, {"which is better", 1.5},
		{"meaning of", 1.5}, {"definition of", 1.5}, {"example of", 1.5},
		{"when to use", 1.0}, {"overview", 1.0}, {"summary", 1.0},
		{"can you explain", 2.0}, {"help me understand", 1.5},
		{"what's the", 1.0}, {"how to understand", 1.5},
		{"teach me", 1.5}, {"walk me through", 1.5},
		// Greetings and casual conversation
		{"hello", 1.5}, {"hi", 1.0}, {"hey", 1.0},
		{"how are you", 2.0}, {"good morning", 1.5}, {"good evening", 1.5},
		{"thank you", 1.5}, {"thanks", 1.0},
		{"你好", 1.5}, {"谢谢", 1.5}, {"早上好", 1.5},
	}
	for _, p := range l0 {
		rules = append(rules, PatternRule{Pattern: p.p, Level: LevelConversation, Weight: p.w})
	}

	// ── L1: Simple / Single-Step Tasks ──
	// Clear single action with clear target
	l1 := []struct {
		p string
		w float64
	}{
		// Chinese action patterns
		{"修复", 1.5}, {"修改", 1.0}, {"改一下", 1.5}, {"改个", 1.5},
		{"加一个", 1.5}, {"添加", 1.0}, {"新增", 1.0}, {"补充", 1.0},
		{"删掉", 1.5}, {"删除", 1.0}, {"去掉", 1.5}, {"移除", 1.0},
		{"更新", 1.0}, {"替换", 1.0}, {"换成", 1.5}, {"改成", 1.5},
		{"写一个", 1.0}, {"生成", 1.0}, {"创建", 1.0}, {"新建", 1.0},
		{"格式化", 1.5}, {"排序", 1.5}, {"重命名", 1.5}, {"移动到", 1.5},
		{"翻译", 1.5}, {"转换", 1.0}, {"提取", 1.0}, {"拷贝", 1.5},
		{"看看", 1.0}, {"检查", 1.0}, {"验证", 1.0}, {"测试一下", 1.0},
		{"运行", 1.0}, {"执行", 1.0}, {"启动", 1.0}, {"停止", 1.0},
		{"安装", 1.0}, {"卸载", 1.0}, {"升级", 1.0},
		{"查找", 1.0}, {"搜索", 1.0}, {"定位", 1.0},
		{"打印", 1.0}, {"输出", 1.0}, {"显示", 1.0},
		{"清理", 1.0}, {"清除", 1.0},
		{"复制", 1.0}, {"粘贴", 1.0},
		// English action patterns
		{"fix", 1.5}, {"fix bug", 2.0}, {"fix the", 1.5},
		{"add", 1.0}, {"create", 1.0}, {"make", 0.8},
		{"update", 1.0}, {"delete", 1.0}, {"remove", 1.0}, {"replace", 1.0},
		{"rename", 1.5}, {"move", 1.0}, {"copy", 1.0}, {"format", 1.5},
		{"translate", 1.5}, {"convert", 1.0}, {"extract", 1.0},
		{"write a", 1.0}, {"generate", 1.0}, {"change to", 1.5},
		{"typo", 2.0}, {"spelling", 1.5}, {"syntax error", 1.5},
		{"run", 1.0}, {"execute", 1.0}, {"start", 1.0}, {"stop", 1.0},
		{"install", 1.0}, {"uninstall", 1.0}, {"upgrade", 1.0},
		{"find", 1.0}, {"search", 1.0}, {"locate", 1.0},
		{"print", 1.0}, {"output", 1.0}, {"display", 1.0},
		{"clean up", 1.0}, {"clear", 1.0},
		{"check", 1.0}, {"verify", 1.0}, {"validate", 1.0},
	}
	for _, p := range l1 {
		rules = append(rules, PatternRule{Pattern: p.p, Level: LevelSimpleSingleFile, Weight: p.w})
	}

	// ── L2: Multi-Step / Coordination Tasks ──
	// Requires planning, multiple related changes, cross-concern coordination
	l2 := []struct {
		p string
		w float64
	}{
		// Chinese multi-step patterns
		{"重构", 2.0}, {"迁移", 2.0}, {"集成", 1.5}, {"对接", 1.5},
		{"实现功能", 1.5}, {"开发功能", 1.5}, {"新功能", 1.5},
		{"设计方案", 1.5}, {"制定计划", 1.5}, {"规划", 1.5},
		{"多个文件", 2.0}, {"跨模块", 2.0}, {"跨组件", 2.0},
		{"批量", 1.5}, {"统一", 1.0}, {"标准化", 1.5}, {"规范化", 1.5},
		{"联调", 1.5}, {"适配", 1.5}, {"兼容", 1.0},
		{"接口设计", 1.5}, {"数据模型", 1.5}, {"表结构", 1.0},
		{"工作流", 1.5}, {"流程", 1.0}, {"管道", 1.0},
		{"部署", 1.0}, {"环境搭建", 1.5}, {"配置环境", 1.5},
		{"测试覆盖", 1.5}, {"补充测试", 1.0}, {"单元测试", 1.0},
		{"同时", 1.0}, {"还要", 1.0}, {"并且还", 1.0},
		{"封装", 1.5}, {"抽象", 1.5}, {"模块化", 1.5},
		{"拆分", 1.5}, {"合并", 1.5}, {"组合", 1.0},
		{"优化", 1.0}, {"改进", 1.0}, {"升级到", 1.5},
		{"数据库", 1.0}, {"缓存", 1.0}, {"消息队列", 1.5},
		{"自动化", 1.5}, {"脚本化", 1.5},
		{"文档", 1.0}, {"注释", 0.8},
		// English multi-step patterns
		{"refactor", 2.0}, {"migrate", 2.0}, {"integrate", 1.5},
		{"implement", 1.5}, {"implement feature", 2.0}, {"new feature", 1.5},
		{"design", 1.5}, {"plan", 1.0}, {"architect", 1.5},
		{"multiple files", 2.0}, {"across", 1.5}, {"cross-module", 2.0},
		{"batch", 1.5}, {"standardize", 1.5}, {"normalize", 1.5},
		{"workflow", 1.5}, {"pipeline", 1.5}, {"deploy", 1.0},
		{"end-to-end", 1.5}, {"full-stack", 1.5},
		{"test coverage", 1.5}, {"ci/cd", 1.5}, {"ci cd", 1.5},
		{"modularize", 1.5}, {"abstract", 1.5}, {"encapsulate", 1.5},
		{"split", 1.5}, {"merge", 1.0}, {"combine", 1.0},
		{"optimize", 1.0}, {"improve", 1.0}, {"upgrade to", 1.5},
		{"database", 1.0}, {"caching", 1.0}, {"message queue", 1.5},
		{"automate", 1.5}, {"scripting", 1.5},
		{"documentation", 1.0},
		{"set up", 1.0}, {"configure", 1.0}, {"scaffold", 1.5},
		{"both", 0.8}, {"and also", 0.8}, {"as well as", 0.8},
	}
	for _, p := range l2 {
		rules = append(rules, PatternRule{Pattern: p.p, Level: LevelMediumMultiFile, Weight: p.w})
	}

	// ── L3: Complex / Deep-Reasoning Tasks ──
	// High uncertainty, architectural decisions, deep debugging, systemic changes
	l3 := []struct {
		p string
		w float64
	}{
		// Chinese complex patterns
		{"重写", 2.0}, {"重新设计", 2.0}, {"重新架构", 2.0},
		{"架构", 1.5}, {"系统设计", 2.0}, {"技术选型", 1.5},
		{"全局", 1.5}, {"整个系统", 2.0}, {"全面", 1.5}, {"彻底", 1.5},
		{"从零开始", 2.0}, {"从头开始", 2.0}, {"推倒重来", 2.0},
		{"性能优化", 1.5}, {"安全审计", 2.0}, {"安全加固", 1.5},
		{"根因分析", 2.0}, {"根本原因", 1.5}, {"深度排查", 2.0},
		{"疑难", 1.5}, {"诡异", 1.5}, {"复杂", 1.0},
		{"大规模", 1.5}, {"高并发", 1.5}, {"分布式", 1.5},
		{"数据一致性", 1.5}, {"事务", 1.0}, {"死锁", 1.5},
		{"内存泄漏", 1.5}, {"竞态条件", 2.0}, {"并发问题", 1.5},
		{"技术债", 1.5}, {"遗留代码", 1.5}, {"遗留系统", 2.0},
		{"全量重构", 2.0}, {"底层改造", 2.0}, {"核心模块", 1.5},
		{"体系", 1.5}, {"框架", 1.0}, {"基础设施", 1.5},
		{"高可用", 1.5}, {"容灾", 1.5}, {"降级", 1.0},
		{"全链路", 2.0}, {"端到端", 1.5},
		{"设计模式", 1.5}, {"架构模式", 2.0},
		// English complex patterns
		{"rewrite", 2.0}, {"redesign", 2.0}, {"rearchitect", 2.0},
		{"architecture", 1.5}, {"system design", 2.0}, {"from scratch", 2.0},
		{"entire system", 2.0}, {"entire app", 2.0}, {"entire application", 2.0},
		{"comprehensive", 1.5}, {"overhaul", 2.0},
		{"performance optimization", 1.5}, {"security audit", 2.0},
		{"root cause", 2.0}, {"deep dive", 1.5}, {"investigate", 1.5},
		{"large scale", 1.5}, {"distributed", 1.5}, {"concurrency", 1.5},
		{"memory leak", 1.5}, {"race condition", 2.0}, {"deadlock", 1.5},
		{"technical debt", 1.5}, {"legacy", 1.5}, {"legacy system", 2.0},
		{"high availability", 1.5}, {"fault tolerance", 1.5},
		{"design pattern", 1.5}, {"architecture pattern", 2.0},
		{"infrastructure", 1.5}, {"foundation", 1.0},
		{"end to end", 1.5}, {"full rewrite", 2.0},
		{"system-wide", 2.0}, {"globally", 1.5}, {"everywhere", 1.5},
		{"ground up", 2.0}, {"fundamental", 1.5},
	}
	for _, p := range l3 {
		rules = append(rules, PatternRule{Pattern: p.p, Level: LevelComplexRefactoring, Weight: p.w})
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
		// Chinese escalation
		{"仔细想", 1}, {"深入分析", 1}, {"深度思考", 1}, {"仔细分析", 1},
		{"认真考虑", 1}, {"慎重", 1}, {"全面考虑", 1}, {"详细分析", 1},
		{"彻底解决", 2}, {"一劳永逸", 2}, {"根治", 2},
		{"好好想想", 1}, {"多想想", 1}, {"反复推敲", 1},
		// English escalation
		{"think carefully", 1}, {"think hard", 1}, {"be thorough", 1},
		{"in depth", 1}, {"in-depth", 1}, {"deeply", 1},
		{"once and for all", 2}, {"definitive", 1}, {"exhaustive", 1},
		{"take your time", 1}, {"no rush", 1},
	}
	for _, e := range escalation {
		rules = append(rules, EscalationRule{Pattern: e.p, Delta: e.d, Weight: 1.5})
	}

	// ── De-escalation: user signals simplicity ──
	deescalation := []struct {
		p string
		d int
	}{
		// Chinese de-escalation
		{"简单", -1}, {"快速", -1}, {"随便", -1}, {"大概", -1},
		{"简单改改", -1}, {"随手", -1}, {"顺便", -1},
		{"不用太复杂", -1}, {"简单点", -1}, {"差不多就行", -1},
		// English de-escalation
		{"just", -1}, {"quick", -1}, {"briefly", -1}, {"rough", -1},
		{"simple", -1}, {"easy", -1}, {"straightforward", -1},
		{"don't overthink", -1}, {"keep it simple", -1},
	}
	for _, e := range deescalation {
		rules = append(rules, EscalationRule{Pattern: e.p, Delta: e.d, Weight: 1.0})
	}

	return rules
}
