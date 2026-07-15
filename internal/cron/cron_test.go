package cron

import (
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
)

func TestNextTrigger(t *testing.T) {
	localZone := time.Local
	now := time.Date(2026, 5, 24, 10, 0, 0, 0, localZone) // Sunday

	tests := []struct {
		expr     string
		from     time.Time
		want     time.Time
		wantOne  bool
		hasError bool
	}{
		{
			expr:    "2026-05-24 15:30:00",
			from:    now,
			want:    time.Date(2026, 5, 24, 15, 30, 0, 0, localZone),
			wantOne: true,
		},
		{
			expr:    "2026-05-25",
			from:    now,
			want:    time.Date(2026, 5, 25, 0, 0, 0, 0, localZone),
			wantOne: true,
		},
		{
			expr:    "daily",
			from:    now,
			want:    time.Date(2026, 5, 25, 0, 0, 0, 0, localZone),
			wantOne: false,
		},
		{
			expr:    "0 8 * * 1", // Next Monday at 8:00 (May 25)
			from:    now,
			want:    time.Date(2026, 5, 25, 8, 0, 0, 0, localZone),
			wantOne: false,
		},
		{
			expr:     "invalid expression",
			from:     now,
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, err := NextTrigger(tt.expr, tt.from)
			if (err != nil) != tt.hasError {
				t.Fatalf("NextTrigger() error = %v, hasError = %v", err, tt.hasError)
			}
			if !tt.hasError {
				if !got.Equal(tt.want) {
					t.Errorf("NextTrigger() got = %v, want = %v", got, tt.want)
				}
				isOne := IsOneTimeExpression(tt.expr)
				if isOne != tt.wantOne {
					t.Errorf("IsOneTimeExpression() got = %v, want = %v", isOne, tt.wantOne)
				}
			}
		})
	}
}

func TestIsL1Target(t *testing.T) {
	tests := []struct {
		task Task
		want bool
	}{
		{Task{TargetAgent: "L1"}, true},
		{Task{TargetAgent: "l1"}, true},
		{Task{TargetAgent: "engineering"}, false},
		{Task{TargetAgent: "L2"}, false},
		{Task{TargetAgent: ""}, true}, // empty defaults to L1
	}
	for _, tt := range tests {
		if got := isL1Target(tt.task); got != tt.want {
			t.Errorf("isL1Target(%q) = %v, want %v", tt.task.TargetAgent, got, tt.want)
		}
	}
}

func TestDrainEvents(t *testing.T) {
	ch := make(chan iface.AgentEvent, 3)
	ch <- struct{ iface.AgentEvent }{}
	// Send a content delta via the agent package helper
	close(ch)

	content, media := drainEvents(ch)
	if content != "" {
		t.Logf("drainEvents content = %q", content)
	}
	if len(media) != 0 {
		t.Logf("drainEvents media = %v", media)
	}
}

func TestDrainEvents_Empty(t *testing.T) {
	ch := make(chan iface.AgentEvent, 1)
	close(ch)

	content, media := drainEvents(ch)
	if content != "" {
		t.Errorf("drainEvents on empty channel got content = %q", content)
	}
	if len(media) != 0 {
		t.Errorf("drainEvents on empty channel got %d media", len(media))
	}
}

func TestBuildCronPrompt(t *testing.T) {
	task := Task{
		Instruction: "Check health status",
	}
	prompt := buildCronPrompt(task)
	if len(prompt) == 0 {
		t.Error("buildCronPrompt returned empty string")
	}
}

func TestParseSendFileMedia(t *testing.T) {
	raw := `{"status":"success","file_type":"image","file_name":"test.png","url":"https://example.com/img.png"}`
	result := parseSendFileMedia(raw)
	if result == nil {
		t.Fatal("parseSendFileMedia returned nil")
	}
	if result.FileType != 1 {
		t.Errorf("FileType = %d, want 1 (image)", result.FileType)
	}
	if result.FileName != "test.png" {
		t.Errorf("FileName = %q, want test.png", result.FileName)
	}
}

func TestParseSendFileMedia_InvalidJSON(t *testing.T) {
	result := parseSendFileMedia("not json")
	if result != nil {
		t.Error("parseSendFileMedia should return nil for invalid JSON")
	}
}

func TestTask_Fields(t *testing.T) {
	task := Task{
		ID:          "task-1",
		Expression:  "0 9 * * *",
		Instruction: "Do something",
		TargetAgent: "L1",
		Status:      "active",
	}
	if task.ID != "task-1" {
		t.Errorf("ID = %q", task.ID)
	}
	if task.Expression != "0 9 * * *" {
		t.Errorf("Expression = %q", task.Expression)
	}
}
