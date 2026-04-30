# Task Router Classifier System Design

**Document Date**: April 30, 2026  
**Status**: Design Phase  
**Priority**: High (Critical for L0-L3 routing)

---

## Executive Summary

The Task Router Classifier (TRC) is a dual-channel system that intelligently routes user input to the appropriate processing level (L0, L1, L2, L3) while minimizing latency and token cost. It combines:

1. **Fast-track rules** (hardcoded local logic) for common cases
2. **LLM-based classification** (semantic understanding) for ambiguous cases
3. **Safety checks** to prevent dangerous operations
4. **Explicit overrides** (slash commands) to bypass classification

This design integrates seamlessly with the existing architecture without disrupting the current session/agent system.

---

## Table of Contents

1. [Current State Analysis](#current-state-analysis)
2. [Proposed System Architecture](#proposed-system-architecture)
3. [Classification Logic](#classification-logic)
4. [Integration Points](#integration-points)
5. [Implementation Roadmap](#implementation-roadmap)
6. [API Specifications](#api-specifications)
7. [Testing Strategy](#testing-strategy)

---

## Current State Analysis

### Existing Architecture

The SoloQueue system has:

- **L0 Placeholder**: No explicit L0 implementation; all input goes directly to L1 Agent
- **L1 Agent**: Main agent with all tools + delegation to L2 leaders
- **L2 Leaders**: Specialized agents (dev, devops, data, etc.) that can delegate to L3
- **L3 Workers**: Dynamic agents spawned by L2 supervisors for specific tasks

### Current Limitations

1. **No intelligent routing**: All input treated equally regardless of complexity
2. **No model selection based on task complexity**: Uses default model for all tasks
3. **No early safety checks**: Dangerous commands only blocked at tool execution
4. **No slash command support**: Can't override classification
5. **No context about user's request intent**: LLM must infer from natural language

---

## Proposed System Architecture

### High-Level Design

```
┌─────────────────────────────────────┐
│   User Input (raw text)             │
└────────────┬────────────────────────┘
             │
             ▼
┌─────────────────────────────────────┐
│ Input Normalizer                    │
│ - Strip leading/trailing whitespace │
│ - Detect slash commands             │
│ - Check for multi-line input        │
└────────────┬────────────────────────┘
             │
             ▼
        ┌────────────────┐
        │ Slash Command? │──YES──┐
        └────┬───────────┘       │
             │ NO                │
             ▼                   ▼
   ┌─────────────────┐  ┌──────────────────┐
   │ Fast-Track      │  │ Execute Command  │
   │ Rules Engine    │  │ (/read, /write,  │
   └────┬─────────────┘  │  /refactor)      │
        │                └──────────────────┘
        │ Classification
        ▼
   ┌─────────────────────┐
   │ Confidence Score    │
   │ (0-100)             │
   └────┬────────────────┘
        │
        ├─ Score >= 85% ────► Route to Level
        │
        ├─ Score 60-85% ────► Optional LLM validation
        │
        └─ Score < 60% ─────► LLM Semantic Classification
                                  │
                                  ▼
                            ┌──────────────┐
                            │ Route to     │
                            │ Level        │
                            └──────────────┘
                                  │
                                  ▼
                         ┌─────────────────┐
                         │ Safety Check    │
                         │ (confirm if req)│
                         └────────┬────────┘
                                  │
                                  ▼
                         ┌─────────────────┐
                         │ Route to        │
                         │ Appropriate     │
                         │ Agent/Session   │
                         └─────────────────┘
```

### Classification Levels

#### **L0: Conversation (No Tools)**
- **Model**: flash (fast, low-cost inference)
- **Thinking**: Disabled
- **Use Cases**:
  - General questions about code concepts
  - Explanations without file access
  - Brainstorming / ideation
  - Small code reviews (< 100 lines, pasted)
- **Detection Signals**:
  - No file paths mentioned
  - No request for modification
  - No tool indicators (write, delete, run, deploy)
  - Explicitly asks for explanation/discussion
  - Contains keywords: "explain", "how", "what", "why", "should", "design"
- **Confidence Threshold**: 95%+

**Example**: 
```
"Explain how closures work in JavaScript"
"What's the best way to handle errors in Go?"
"Design a caching strategy for this scenario"
```

---

#### **L1: Simple Single-File Task**
- **Model**: flash-thinking (fast + high-level reasoning)
- **Thinking**: High
- **Context Window**: 1M tokens (sufficient for most single files)
- **Use Cases**:
  - Single file read and analysis
  - Simple single-file modifications
  - Quick bug fixes in one file
  - Creating a small new file
  - Testing simple changes
- **Detection Signals**:
  - Single explicit file path OR
  - Clear intent to modify/create one file OR
  - Task is very focused and scoped
  - No directory operations
  - No cross-file dependencies implied
  - Mentions tools: read, write, or replace (singular)
  - Contains keywords: "fix", "add", "change", "update" (single)
- **Confidence Threshold**: 75%+
- **Safety Escalation**: 
  - Dangerous file operations (delete system files) → escalate to L2
  - Shell commands without explicit `/run` → require confirmation

**Example**:
```
"Fix the null pointer bug on line 42 of main.go"
"Add a type annotation to this function in service.ts"
"Read the error handler in auth.js and suggest improvements"
```

---

#### **L2: Medium Multi-File Task**
- **Model**: pro (superior capability, balanced cost)
- **Thinking**: High
- **Context Window**: 1M tokens
- **Use Cases**:
  - 2-5 files across related modules
  - Refactoring that spans multiple files
  - Adding a feature requiring changes in multiple places
  - Coordinating changes between backend/frontend
  - Creating multiple related files
  - Database migrations + app code
- **Detection Signals**:
  - Multiple file paths mentioned (2-5) OR
  - Cross-file dependencies implied OR
  - Mentions refactoring / migration / feature-add OR
  - Requires coordination between components
  - Directory operations (mkdir, create folder structure)
  - Keywords: "refactor", "add feature", "migrate", "implement", "coordinate"
  - Multi-step process implied
- **Confidence Threshold**: 70%+
- **Delegation Pattern**: May delegate specific sub-tasks to L3
- **Safety Escalation**:
  - Database operations → require schema review
  - Breaking changes → require impact analysis
  - File deletions → require confirmation

**Example**:
```
"Add user authentication. Modify auth.go, update middleware.go, and create login.tsx"
"Refactor the data layer. Update models.go, dal.go, and service.go"
"Migrate database schema and update related query functions"
```

---

#### **L3: Complex Multi-File Refactoring**
- **Model**: pro-max (expert capability, highest cost, best reasoning)
- **Thinking**: Max (deepest reasoning)
- **Context Window**: 1M tokens
- **Use Cases**:
  - Large-scale refactoring (10+ files)
  - Complete feature implementations
  - Architecture changes
  - Complex bug investigations
  - Performance optimization campaigns
  - Full codebase reorganization
- **Detection Signals**:
  - Many files mentioned (5+) OR
  - Vague scope ("refactor the whole system", "improve performance") OR
  - Keywords: "refactor entire", "redesign", "optimize performance", "architecture", "complete"
  - Complex analysis required
  - Cross-cutting concerns
  - Requires understanding large portions of codebase
- **Confidence Threshold**: 60%+
- **Delegation Pattern**: Heavy use of L2/L3 delegation for parallelization
- **Safety Escalation**:
  - Database changes → require DBA review
  - API changes → require API contract review
  - Deployment-related → require DevOps review

**Example**:
```
"Refactor the entire error handling system across all services"
"Implement a caching layer for performance optimization"
"Redesign the authentication system from scratch"
```

---

## Classification Logic

### Fast-Track Rules Engine

Located in: `internal/router/classifier.go`

```go
type FastTrackClassifier struct {
    patterns map[string]*regexp.Regexp
    keywords map[Level][]string
}

func (ftc *FastTrackClassifier) Classify(input string) (Level, float64) {
    score := 0.0
    
    // Rule 1: Detect slash commands
    if strings.HasPrefix(input, "/") {
        return parseSlashCommand(input), 100.0
    }
    
    // Rule 2: Count file paths
    filePaths := countFilePaths(input)  // regex: "/.../filename.ext" or "./file" or "file.ext"
    if filePaths == 0 {
        score += 30  // No files = likely L0
    } else if filePaths == 1 {
        score += 70  // Single file = likely L1
    } else if filePaths <= 5 {
        score += 65  // Multiple files = likely L2
    } else {
        score += 50  // Many files = likely L3
    }
    
    // Rule 3: Detect dangerous operations
    if containsDangerousOps(input) {
        return L2, 80.0  // Escalate for safety
    }
    
    // Rule 4: Keyword detection
    keywords := extractKeywords(input)
    for _, kw := range keywords {
        if isL0Keyword(kw) {
            score += 15
        } else if isL3Keyword(kw) {
            score -= 30  // Reduce confidence for simple models
        }
    }
    
    // Rule 5: Input complexity (line count, char count)
    if len(input) > 500 || strings.Count(input, "\n") > 5 {
        score -= 10  // Complex input = less confident
    }
    
    return determineLevel(score), float64(score)
}

func countFilePaths(input string) int {
    // Regex patterns for file paths
    patterns := []*regexp.Regexp{
        regexp.MustCompile(`/[\w/\-\.]+\.\w+`),        // /path/to/file.ext
        regexp.MustCompile(`\.\./[\w/\-\.]+\.\w+`),    // ../relative/path.ext
        regexp.MustCompile(`[\w\-]+\.\w+`),            // simple filename.ext (in context)
    }
    
    matches := 0
    for _, p := range patterns {
        matches += len(p.FindAllString(input, -1))
    }
    return matches
}

func containsDangerousOps(input string) bool {
    dangerous := []string{
        `rm\s+-rf`, `rm\s+/`, `dd\s+of=/`,
        `mkfs`, `format\s+`, `diskpart`,
        `DELETE\s+FROM`, `DROP\s+TABLE`,
    }
    
    for _, op := range dangerous {
        if regexp.MustCompile(op).MatchString(strings.ToLower(input)) {
            return true
        }
    }
    return false
}
```

### LLM-Based Semantic Classification

Located in: `internal/router/semantic_classifier.go`

Used when fast-track confidence is < 75%.

```go
type SemanticClassifier struct {
    llm    agent.LLMClient
    cache  map[string]Level  // Cache recent classifications
}

func (sc *SemanticClassifier) Classify(ctx, input string) (Level, error) {
    // Check cache first
    if cached, ok := sc.cache[hashInput(input)]; ok {
        return cached, nil
    }
    
    // Build classification prompt
    prompt := buildClassificationPrompt(input)
    
    // Call LLM with structured response
    response, err := sc.llm.Chat(ctx, &LLMRequest{
        Messages: []Message{
            {Role: "system", Content: systemPrompt},
            {Role: "user", Content: prompt},
        },
        Temperature: 0.1,        // Low temperature for consistency
        MaxTokens:  200,         // Short response
    })
    
    if err != nil {
        return L1, err  // Default to L1 on LLM error
    }
    
    // Parse structured response
    level := parseClassificationResponse(response.Content)
    
    // Cache result
    sc.cache[hashInput(input)] = level
    
    return level, nil
}

func buildClassificationPrompt(input string) string {
    return fmt.Sprintf(`You are a task router. Classify this user input into ONE category:

USER INPUT:
"""%s"""

Classification Categories:
- L0: Pure conversation (no files, no modifications, explanations only)
- L1: Single-file task (read one file, modify one file, create one file)
- L2: Multi-file task (2-5 files, feature work, coordinated changes)
- L3: Complex refactoring (many files, architecture changes, large scope)

IMPORTANT: Only output ONE of: L0, L1, L2, L3. Add brief reasoning (one sentence).

Classification:`, input)
}

func parseClassificationResponse(content string) Level {
    content = strings.ToUpper(strings.TrimSpace(content))
    
    if strings.Contains(content, "L3") {
        return L3
    } else if strings.Contains(content, "L2") {
        return L2
    } else if strings.Contains(content, "L1") {
        return L1
    }
    return L0  // Default
}
```

### Safety Check Module

Located in: `internal/router/safety.go`

```go
type SafetyChecker struct {
    blockRules []BlockRule
    confirmRules []ConfirmRule
}

type BlockRule struct {
    Name        string
    Pattern     *regexp.Regexp
    Message     string
}

type ConfirmRule struct {
    Name        string
    Pattern     *regexp.Regexp
    Question    string
}

func (sc *SafetyChecker) Check(input string) (action SafetyAction, err error) {
    // Check for blocked operations
    for _, rule := range sc.blockRules {
        if rule.Pattern.MatchString(input) {
            return SafetyAction{
                Action:  ActionBlock,
                Rule:    rule.Name,
                Message: rule.Message,
            }, nil
        }
    }
    
    // Check for operations requiring confirmation
    for _, rule := range sc.confirmRules {
        if rule.Pattern.MatchString(input) {
            return SafetyAction{
                Action:   ActionConfirm,
                Rule:     rule.Name,
                Question: rule.Question,
            }, nil
        }
    }
    
    return SafetyAction{Action: ActionAllow}, nil
}
```

---

## Integration Points

### 1. HTTP Server Integration

**File**: `internal/server/server.go`

```go
func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
    var req CreateSessionRequest
    json.NewDecoder(r.Body).Decode(&req)
    
    // NEW: Route the initial prompt
    level, err := s.router.ClassifyAndRoute(r.Context(), req.Prompt)
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    
    // Select appropriate session factory
    factory := s.selectSessionFactory(level)
    
    // Create session with selected model
    session, err := factory.Build(r.Context(), req.TeamID)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    // Continue as before
    ...
}
```

### 2. Session Factory Enhancement

**File**: `cmd/soloqueue/main.go`

Currently creates a single session factory. Will be enhanced to create multiple:

```go
type SessionFactoryPool struct {
    l0Factory  SessionBuilder  // Flash model, no thinking
    l1Factory  SessionBuilder  // Flash-thinking model
    l2Factory  SessionBuilder  // Pro model, high thinking
    l3Factory  SessionBuilder  // Pro-max model, max thinking
}

func (sfp *SessionFactoryPool) SelectFactory(level Level) SessionBuilder {
    switch level {
    case L0:
        return sfp.l0Factory
    case L1:
        return sfp.l1Factory
    case L2:
        return sfp.l2Factory
    case L3:
        return sfp.l3Factory
    default:
        return sfp.l1Factory
    }
}
```

### 3. Agent Definition Enhancement

**File**: `internal/agent/types.go`

```go
type Definition struct {
    // ... existing fields ...
    
    // NEW: Task routing metadata
    Level           Level           // L0, L1, L2, or L3
    EstimatedLevel  Level           // Initial classification
    RoutedAt        time.Time       // When routing decision was made
}
```

### 4. Configuration Extension

**File**: `internal/config/schema.go`

```go
type RouterConfig struct {
    // Enable/disable routing
    Enabled bool `json:"enabled"`
    
    // Classification confidence thresholds
    FastTrackThreshold   float64 `json:"fastTrackThreshold"`    // 0-100, default 75
    SemanticThreshold    float64 `json:"semanticThreshold"`     // 0-100, default 60
    
    // LLM classification settings
    UseSemanticClassifier bool   `json:"useSemanticClassifier"` // Use LLM if low confidence
    ClassificationCache   bool   `json:"classificationCache"`   // Cache recent classifications
    CacheTTL              int    `json:"cacheTTL"`              // Cache duration in seconds
    
    // Safety settings
    BlockDangerousOps    bool          `json:"blockDangerousOps"`
    ConfirmDangerousOps  bool          `json:"confirmDangerousOps"`
    BlockPatterns        []string      `json:"blockPatterns"`      // Regex patterns to block
    ConfirmPatterns      []string      `json:"confirmPatterns"`    // Regex patterns to confirm
}
```

---

## API Specifications

### Router Service

```go
// internal/router/router.go

type Router interface {
    // Classify input and return routing decision
    ClassifyAndRoute(ctx context.Context, input string) (*RoutingDecision, error)
    
    // Get routing statistics (for monitoring)
    GetStats() RoutingStats
    
    // Reset cache
    ResetCache()
}

type RoutingDecision struct {
    Level               Level           // L0, L1, L2, or L3
    Confidence          float64         // 0-100
    Reason              string          // Explanation
    SafetyAction        SafetyAction    // Block/Confirm/Allow
    ModelID             string          // Selected model
    ThinkingLevel       string          // "disabled", "high", "max"
    EstimatedTokens     int             // Estimated tokens needed
    RecommendedTimeout  time.Duration   // Suggested timeout
}

type SafetyAction struct {
    Action      Action  // ActionAllow, ActionConfirm, ActionBlock
    Rule        string  // Rule name that triggered
    Message     string  // Human-readable message
    Question    string  // Confirmation question (if ActionConfirm)
}

type Level string

const (
    L0 Level = "L0"  // Conversation
    L1 Level = "L1"  // Simple single-file
    L2 Level = "L2"  // Multi-file medium
    L3 Level = "L3"  // Complex refactoring
)

type RoutingStats struct {
    TotalClassifications  int                   // Total inputs classified
    ClassificationByLevel map[Level]int         // Count per level
    AvgConfidence         float64               // Average confidence score
    CacheHitRate          float64               // Cache hit percentage
    FastTrackRate         float64               // % using fast-track vs LLM
}
```

### Slash Commands

```
/l0 <prompt>           - Force L0 (conversation)
/l1 <prompt>           - Force L1 (single-file)
/l2 <prompt>           - Force L2 (multi-file)
/l3 <prompt>           - Force L3 (expert)
/read <file>           - Shorthand for reading a file
/write <file> <code>   - Shorthand for writing a file
/refactor <scope>      - Shorthand for L2/L3 refactoring
/run <command>         - Shorthand for running shell command
/debug <description>   - Shorthand for debugging (L2/L3)
/classify              - Show classification of previous input
/stats                 - Show router statistics
```

---

## Implementation Roadmap

### Phase 1: Foundation (Week 1)

- [ ] Create `internal/router/` package structure
- [ ] Implement fast-track classification with basic patterns
- [ ] Add Level constants and types
- [ ] Create basic safety checks
- [ ] Unit tests for classification logic

**Files to create**:
- `internal/router/router.go` — Main Router interface
- `internal/router/classifier.go` — FastTrackClassifier
- `internal/router/safety.go` — SafetyChecker
- `internal/router/types.go` — Types and constants
- `internal/router/router_test.go` — Tests

### Phase 2: LLM Integration (Week 2)

- [ ] Implement SemanticClassifier
- [ ] Add LLM-based classification for low-confidence cases
- [ ] Implement classification caching
- [ ] Integration tests with mock LLM
- [ ] Performance benchmarks

**Files to create**:
- `internal/router/semantic_classifier.go`
- `internal/router/cache.go` (if separate)
- `internal/router/semantic_classifier_test.go`

### Phase 3: HTTP Server Integration (Week 3)

- [ ] Update `internal/server/server.go` to use router
- [ ] Enhance session creation to select model by level
- [ ] Update session factory pool
- [ ] Integration tests end-to-end
- [ ] Add routing decision to session metadata

**Files to modify**:
- `internal/server/server.go`
- `cmd/soloqueue/main.go` (session factories)
- `internal/session/session.go` (add level field)

### Phase 4: Slash Commands (Week 4)

- [ ] Implement slash command parser
- [ ] Add slash command handlers
- [ ] Route slash commands to appropriate levels
- [ ] Update TUI and HTTP handlers
- [ ] Documentation

**Files to create**:
- `internal/router/slash_commands.go`
- `internal/router/slash_commands_test.go`

**Files to modify**:
- `internal/server/server.go`
- `internal/tui/app.go`

### Phase 5: Monitoring & Refinement (Week 5)

- [ ] Add routing statistics collection
- [ ] Metrics for Prometheus export
- [ ] Dashboard/logging for classification decisions
- [ ] Fine-tune confidence thresholds based on real usage
- [ ] Performance optimization

---

## Testing Strategy

### Unit Tests

1. **FastTrackClassifier**
   - Test file path counting (0, 1, 2-5, 5+ files)
   - Test keyword detection
   - Test dangerous operation detection
   - Test confidence scoring

2. **SafetyChecker**
   - Test blocking rules
   - Test confirmation rules
   - Test rule ordering and priority

3. **SemanticClassifier**
   - Test LLM response parsing
   - Test cache hit/miss
   - Test error handling

### Integration Tests

1. **Router end-to-end**
   - Test fast-track → LLM fallback
   - Test routing decision accuracy
   - Test timing/performance

2. **HTTP server integration**
   - Test session creation with routing
   - Test model selection per level
   - Test thinking parameter injection

3. **Slash command handling**
   - Test explicit level override
   - Test shorthand commands
   - Test error cases

### Benchmarks

- Fast-track classification: < 5ms
- LLM classification: < 1s (with retries)
- Overall routing decision: < 2s

---

## Risk & Mitigation

### Risk 1: Misclassification Leading to Underutilized Models

**Mitigation**:
- Start with conservative thresholds (escalate when uncertain)
- Allow explicit slash command overrides
- Monitor classification accuracy and adjust over time
- Provide user feedback on classification reasoning

### Risk 2: LLM Classification Adds Latency

**Mitigation**:
- Use fast-track for 80%+ of cases
- Cache classification results
- Run LLM classification in parallel with session creation (if possible)
- Set aggressive timeout on LLM (fallback to L1)

### Risk 3: Safety Checks Block Legitimate Operations

**Mitigation**:
- Use confirmation (not blocking) for most operations
- Whitelist trusted commands in configuration
- Log blocked operations for review
- Provide clear error messages with workarounds

### Risk 4: Incompatibility with Existing Sessions

**Mitigation**:
- Router is optional (config.Enabled flag)
- Existing sessions use default model if router disabled
- Backward compatible: no breaking changes to agent/session APIs

---

## Success Criteria

1. **Classification Accuracy**: > 85% match between fast-track and semantic classifier
2. **Performance**: Routing decision in < 100ms (p95)
3. **User Experience**: 
   - Slash commands work intuitively
   - Clear feedback on routing decisions
   - No disruption to existing flows
4. **Reliability**: 99%+ uptime (no errors in classification)
5. **Cost Optimization**: 30% reduction in token usage for L0/L1 tasks

---

## Appendix: Example Classification Traces

### Example 1: L0 Classification

```
Input: "Explain how closures work in JavaScript"

Fast-Track Analysis:
- File paths: 0 → +30 points
- Keywords: "explain", "how" → +15 points  (L0 keywords)
- Dangerous ops: No → +0
- Input complexity: Short → +0
- Total Score: 45 → L0 (but low confidence)

LLM Validation:
- LLM confirms: L0 (pure conversation)
- Final: L0, Confidence: 95%

Routing Decision:
- Level: L0
- Model: flash (fast, low-cost)
- Thinking: Disabled
- Timeout: 30s
```

### Example 2: L1 Classification

```
Input: "Fix the null pointer bug on line 42 of services/auth.go"

Fast-Track Analysis:
- File paths: 1 (services/auth.go) → +70 points
- Keywords: "fix", "bug" → +10 points (L1 keywords)
- Dangerous ops: No → +0
- Input complexity: Short → +0
- Total Score: 80 → L1 (high confidence)

No LLM validation needed (> 75% threshold)

Routing Decision:
- Level: L1
- Model: flash-thinking (fast + reasoning)
- Thinking: High
- Timeout: 60s
```

### Example 3: L2 Classification (with Semantic Validation)

```
Input: "Add user authentication. Modify auth.go, update middleware.go, and create login.tsx"

Fast-Track Analysis:
- File paths: 3 → +65 points (multiple files)
- Keywords: "add", "feature", "modify", "update" → +15 points (L2 keywords)
- Dangerous ops: No → +0
- Input complexity: Medium → -5
- Total Score: 75 → L2 (borderline confidence)

LLM Validation (< 85% confidence):
- LLM confirms: L2 (multi-file feature work)
- Confidence boost: 85%

Routing Decision:
- Level: L2
- Model: pro (superior capability)
- Thinking: High
- Timeout: 300s
```

### Example 4: L3 with Safety Escalation

```
Input: "Refactor entire error handling system across all services"

Fast-Track Analysis:
- File paths: 0 (vague scope) → +0
- Keywords: "refactor entire", "system" → +25 points (L3 keywords)
- Dangerous ops: No → +0
- Input complexity: High → -10
- Total Score: 15 → L1 (but very low confidence)

LLM Classification:
- LLM indicates: L3 (complex refactoring)
- Confidence: 90%

Safety Check:
- No dangerous operations detected
- No confirmation needed

Routing Decision:
- Level: L3
- Model: pro-max (expert capability)
- Thinking: Max
- Timeout: 600s
```

---

## Conclusion

The Task Router Classifier system provides intelligent, efficient routing while maintaining user control through explicit commands and safety guardrails. By combining fast-track rules with semantic classification, it minimizes latency and cost while ensuring appropriate model selection for task complexity.

Integration with existing architecture is minimal and non-breaking, allowing for gradual rollout and refinement based on real-world usage patterns.

