package simulation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

// PlanGenerator creates and updates daily schedules for simulation agents.
type PlanGenerator struct {
	llm        agent.LLMClient
	model      string
	providerID string
	maxTokens  int // 0 = use sensible default per call
}

// NewPlanGenerator creates a plan generator.
func NewPlanGenerator(llm agent.LLMClient, model, providerID string) *PlanGenerator {
	return &PlanGenerator{llm: llm, model: model, providerID: providerID}
}

// SetMaxTokens overrides max_tokens for LLM calls. If 0 or not called, uses
// a per-call default (2048 generation, 1024 revision).
func (pg *PlanGenerator) SetMaxTokens(n int) {
	if n > 0 {
		pg.maxTokens = n
	}
}

// GenerateDailyPlan creates a full day schedule for an agent.
// The plan is based on the persona, goals, environment layout, and simulated start time.
func (pg *PlanGenerator) GenerateDailyPlan(
	ctx context.Context,
	persona *Persona,
	env *Environment,
	clock *SimClock,
	language string,
	worldState map[string]any,
) (*DailyPlan, error) {
	now := clock.Now()
	prompt := buildPlanGenerationPrompt(persona, env, now, language, worldState)

	mt := pg.maxTokens
	if mt <= 0 {
		mt = 16384
	}
	planTokens := mt / 8
	if planTokens < 1024 {
		planTokens = 1024
	}
	resp, err := pg.llm.Chat(ctx, agent.LLMRequest{
		Model:        pg.model,
		ProviderID:   pg.providerID,
		Messages:     []agent.LLMMessage{{Role: "user", Content: prompt}},
		MaxTokens:    planTokens,
		ResponseJSON: true,
	})
	if err != nil {
		return nil, fmt.Errorf("plan generation LLM: %w", err)
	}

	plan, err := parseDailyPlan(resp.Content, persona.ID, now)
	if err != nil {
		return nil, fmt.Errorf("parse daily plan: %w", err)
	}

	return plan, nil
}

// RevisePlan adjusts an existing plan based on new observations.
func (pg *PlanGenerator) RevisePlan(
	ctx context.Context,
	persona *Persona,
	currentPlan *DailyPlan,
	observations []Observation,
	clock *SimClock,
	language string,
) (*DailyPlan, error) {
	now := clock.Now()
	prompt := buildPlanRevisionPrompt(persona, currentPlan, observations, now, language)

	reviseTokens := pg.maxTokens / 16
	if reviseTokens < 512 {
		reviseTokens = 512
	}
	if pg.maxTokens <= 0 {
		reviseTokens = 1024
	}
	resp, err := pg.llm.Chat(ctx, agent.LLMRequest{
		Model:        pg.model,
		ProviderID:   pg.providerID,
		Messages:     []agent.LLMMessage{{Role: "user", Content: prompt}},
		MaxTokens:    reviseTokens,
		ResponseJSON: true,
	})
	if err != nil {
		return nil, fmt.Errorf("plan revision LLM: %w", err)
	}

	revised, err := parseDailyPlan(resp.Content, persona.ID, now)
	if err != nil {
		return nil, fmt.Errorf("parse revised plan: %w", err)
	}

	// Preserve already completed items
	for i, item := range currentPlan.Schedule {
		if item.Status == "completed" {
			found := false
			for j := range revised.Schedule {
				if revised.Schedule[j].Activity == item.Activity {
					revised.Schedule[j].Status = "completed"
					found = true
					break
				}
			}
			if !found {
				revised.Schedule = append(revised.Schedule, item)
			}
			_ = i
		}
	}

	return revised, nil
}

// GetCurrentActivity returns the plan item for the current simulated time.
func (dp *DailyPlan) GetCurrentActivity(now time.Time) *PlanItem {
	for i := range dp.Schedule {
		if !now.Before(dp.Schedule[i].StartTime) && now.Before(dp.Schedule[i].EndTime) {
			if dp.Schedule[i].Status == "pending" {
				dp.Schedule[i].Status = "in_progress"
			}
			return &dp.Schedule[i]
		}
	}
	return nil
}

// NextPendingActivity returns the next pending plan item.
func (dp *DailyPlan) NextPendingActivity(now time.Time) *PlanItem {
	for i := range dp.Schedule {
		if dp.Schedule[i].StartTime.After(now) && dp.Schedule[i].Status == "pending" {
			return &dp.Schedule[i]
		}
	}
	return nil
}

// FormatForPrompt renders the plan as markdown for system prompt injection.
func (dp *DailyPlan) FormatForPrompt(now time.Time, language string) string {
	var b strings.Builder
	if language == "zh" {
		b.WriteString("## Your Daily Schedule\n\n")
		b.WriteString("| Time | Activity | Location | Status |\n")
		b.WriteString("|------|----------|----------|--------|\n")
	} else {
		b.WriteString("## Your Daily Schedule\n\n")
		b.WriteString("| Time | Activity | Location | Status |\n")
		b.WriteString("|------|----------|----------|--------|\n")
	}

	for _, item := range dp.Schedule {
		timeRange := fmt.Sprintf("%s-%s", item.StartTime.Format("15:04"), item.EndTime.Format("15:04"))
		if item.Status == "pending" && !item.StartTime.After(now) && now.Before(item.EndTime) {
			item.Status = "in_progress"
		}
		
		statusStr := item.Status
		if language == "zh" {
			statusStr = map[string]string{
				"pending":     "Pending",
				"in_progress": "In Progress",
				"completed":   "Completed",
				"cancelled":   "Cancelled",
			}[item.Status]
		}
		
		statusIcon := map[string]string{
			"pending":    "⏳",
			"in_progress": "▶️",
			"completed":  "✅",
			"cancelled":  "❌",
		}[item.Status]
		if statusIcon == "" {
			statusIcon = item.Status
		}
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %s %s |\n", timeRange, item.Activity, item.Location, statusIcon, statusStr))
	}

	if current := dp.GetCurrentActivity(now); current != nil {
		if language == "zh" {
			b.WriteString(fmt.Sprintf("\n**Current activity**: %s at %s. %s\n", current.Activity, current.Location, current.Description))
		} else {
			b.WriteString(fmt.Sprintf("\n**Current activity**: %s at %s. %s\n", current.Activity, current.Location, current.Description))
		}
	}

	return b.String()
}

func buildPlanGenerationPrompt(persona *Persona, env *Environment, now time.Time, language string, worldState map[string]any) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Generate a daily schedule for the following person.\n\n"))

	b.WriteString(fmt.Sprintf("Name: %s\n", persona.Name))
	b.WriteString(fmt.Sprintf("Role: %s\n", persona.Role))
	if persona.Bio != "" {
		b.WriteString(fmt.Sprintf("Bio: %s\n", persona.Bio))
	}
	if len(persona.Goals) > 0 {
		b.WriteString("Goals: " + strings.Join(persona.Goals, "; ") + "\n")
	}
	if persona.MBTI != "" {
		b.WriteString(fmt.Sprintf("MBTI: %s\n", persona.MBTI))
	}

	b.WriteString(fmt.Sprintf("\nCurrent time: %s. Plan from this moment to %s.\n\n",
		now.Format("15:04"), now.Add(12*time.Hour).Format("15:04")))

	b.WriteString("Available locations:\n")
	for _, name := range env.ZoneNames() {
		b.WriteString(fmt.Sprintf("- %s\n", name))
	}

	// World context from seed (key topics, conflict areas, setting)
	if worldState != nil {
		keys := make([]string, 0, len(worldState))
		for k := range worldState {
			if !strings.HasPrefix(k, "_seed_") {
				keys = append(keys, k)
			}
		}
		if len(keys) > 0 {
			sort.Strings(keys)
			b.WriteString("\nWorld setting:\n")
			for _, k := range keys {
				v := worldState[k]
				b.WriteString(fmt.Sprintf("- %s: %s\n", k, formatValue(v)))
			}
		}
	}

	b.WriteString("\nGenerate a JSON schedule with 6-12 items. Each item must include:\n")
	b.WriteString("- start_time: \"HH:MM\" format\n")
	b.WriteString("- end_time: \"HH:MM\" format\n")
	b.WriteString("- activity: short name of the activity\n")
	b.WriteString("- location: one of the available zones\n")
	b.WriteString("- description: brief explanation (1 sentence)\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Schedule realistic, varied activities\n")
	b.WriteString("- Include meals, work/hobby, social time, reflection\n")
	b.WriteString("- Place yourself in locations where you might encounter others\n")
	b.WriteString("- Allow time for spontaneous interactions\n")

	if language == "zh" {
		b.WriteString("\nIMPORTANT: Since the simulation language is Chinese, you MUST generate the daily schedule using Chinese. All activities and descriptions must be in Chinese. The location must match the exact available zone names provided.\n")
	}

	b.WriteString("\nOutput ONLY valid JSON:\n")
	b.WriteString(`{"schedule": [{"start_time": "07:00", "end_time": "07:30", "activity": "Morning routine", "location": "home", "description": "Wake up and get ready."}]}`)

	return b.String()
}

func buildPlanRevisionPrompt(persona *Persona, plan *DailyPlan, observations []Observation, now time.Time, language string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("You are %s. Your current plan was disrupted or needs adjustment.\n\n", persona.Name))

	b.WriteString("Current plan:\n")
	b.WriteString(plan.FormatForPrompt(now, language))
	b.WriteString("\n")

	b.WriteString("Recent observations:\n")
	for _, o := range observations {
		b.WriteString(fmt.Sprintf("- %s\n", o.Content))
	}
	b.WriteString("\n")

	b.WriteString(fmt.Sprintf("Current time: %s. Revise your remaining schedule (keep completed items).\n", now.Format("15:04")))
	if language == "zh" {
		b.WriteString("IMPORTANT: Since the simulation language is Chinese, you MUST generate the revised schedule using Chinese.\n")
	}
	b.WriteString("Output ONLY valid JSON with the revised schedule.\n")

	return b.String()
}

func parseDailyPlan(content string, agentID string, now time.Time) (*DailyPlan, error) {
	cleaned := cleanJSONResponse(content)

	var result struct {
		Schedule []struct {
			StartTime   string `json:"start_time"`
			EndTime     string `json:"end_time"`
			Activity    string `json:"activity"`
			Location    string `json:"location"`
			Description string `json:"description"`
		} `json:"schedule"`
	}

	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}

	plan := &DailyPlan{
		GeneratedAt: time.Now(),
		AgentID:     agentID,
	}

	baseDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	for _, item := range result.Schedule {
		start, err := parseTimeString(item.StartTime, baseDay)
		if err != nil {
			continue
		}
		end, err := parseTimeString(item.EndTime, baseDay)
		if err != nil {
			continue
		}
		if end.Before(start) || end.Equal(start) {
			end = start.Add(30 * time.Minute)
		}

		plan.Schedule = append(plan.Schedule, PlanItem{
			StartTime:   start,
			EndTime:     end,
			Activity:    item.Activity,
			Location:    item.Location,
			Description: item.Description,
			Status:      "pending",
		})
	}

	return plan, nil
}

func parseTimeString(s string, baseDay time.Time) (time.Time, error) {
	parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("invalid time format: %s", s)
	}
	var h, m int
	if _, err := fmt.Sscanf(parts[0], "%d", &h); err != nil {
		return time.Time{}, err
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &m); err != nil {
		return time.Time{}, err
	}
	return time.Date(baseDay.Year(), baseDay.Month(), baseDay.Day(), h, m, 0, 0, baseDay.Location()), nil
}

// ─── Activity Classification & Dynamic Step Size ────────────────────────────

// ActivityDensity classifies how "dense" an agent's current activity is.
// Used by the barrier loop to dynamically adjust the SimClock step size.
type ActivityDensity int

const (
	DensitySleep       ActivityDensity = iota // rest/sleep → 60-120min ticks
	DensitySolo                                // solo routine work → 20-30min ticks
	DensitySocial                              // social/public areas → 10-15min ticks
	DensityInteractive                         // active interaction → 5-10min ticks
	DensityConflict                            // conflict/chase → 2-5min ticks
)

// ClassifyActivity categorizes a PlanItem by its activity and location
// using keyword matching. Returns a density level that informs step size.
func ClassifyActivity(item *PlanItem) ActivityDensity {
	if item == nil {
		return DensitySolo
	}

	act := strings.ToLower(item.Activity)
	loc := strings.ToLower(item.Location)
	combined := act + " " + loc

	// Sleep/rest keywords → lowest density
	sleepKeys := []string{"sleep", "rest", "slumber", "meditate", "meditation", "nap"}
	for _, kw := range sleepKeys {
		if strings.Contains(combined, kw) {
			return DensitySleep
		}
	}

	// Conflict/fight keywords → highest density
	conflictKeys := []string{"conflict", "fight", "battle", "chase", "pursue", "duel", "siege"}
	for _, kw := range conflictKeys {
		if strings.Contains(combined, kw) {
			return DensityConflict
		}
	}

	// Interactive/collaborative keywords → high density
	interactKeys := []string{"discuss", "consult", "confront", "cooperate", "collaborate", "negotiate", "plan", "deliberate", "plot"}
	for _, kw := range interactKeys {
		if strings.Contains(combined, kw) {
			return DensityInteractive
		}
	}

	// Social/public area keywords → medium density
	socialKeys := []string{"chat", "eat", "shop", "party", "cafe", "market", "restaurant", "pub", "social", "lunch", "dinner"}
	for _, kw := range socialKeys {
		if strings.Contains(combined, kw) {
			return DensitySocial
		}
	}

	// Default: solo routine work
	return DensitySolo
}

// densityStepMax maps each density level to its maximum recommended step size.
var densityStepMax = map[ActivityDensity]time.Duration{
	DensitySleep:       120 * time.Minute,
	DensitySolo:        30 * time.Minute,
	DensitySocial:      15 * time.Minute,
	DensityInteractive: 10 * time.Minute,
	DensityConflict:    5 * time.Minute,
}

// ResolveNextStepSize examines all agents' current plan activities and returns
// the minimum required step size across all agents. Called by the barrier loop
// to dynamically adjust the SimClock's step size.
//
// The rule: take the most conservative (smallest) step across all agents,
// because if anyone needs fine granularity, everyone should get it.
func ResolveNextStepSize(plans []*DailyPlan, clock *SimClock) time.Duration {
	if len(plans) == 0 {
		return 30 * time.Minute
	}

	minStep := 120 * time.Minute // start with largest
	now := clock.Now()

	for _, plan := range plans {
		if plan == nil {
			minStep = minDuration(minStep, 15*time.Minute) // no plan → finer granularity
			continue
		}
		current := plan.GetCurrentActivity(now)
		if current == nil {
			minStep = minDuration(minStep, 15*time.Minute) // between activities → finer
			continue
		}
		density := ClassifyActivity(current)
		maxStep := densityStepMax[density]
		minStep = minDuration(minStep, maxStep)
	}

	// Clamp to sane bounds
	if minStep < 2*time.Minute {
		minStep = 2 * time.Minute
	}
	if minStep > 120*time.Minute {
		minStep = 120 * time.Minute
	}

	return minStep
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}