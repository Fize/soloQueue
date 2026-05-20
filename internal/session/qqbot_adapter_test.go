package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/qqbot"
	"github.com/xiaobaitu/soloqueue/internal/timeline"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

func TestParseSendFileResult(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *sendFileToolResult
	}{
		{
			name:  "valid image url",
			input: `{"status":"success","file_name":"a.png","file_type":"image","url":"http://example.com/a.png"}`,
			expected: &sendFileToolResult{
				Status:   "success",
				FileName: "a.png",
				FileType: "image",
				URL:      "http://example.com/a.png",
			},
		},
		{
			name:  "valid file base64",
			input: `{"status":"success","file_name":"report.txt","file_type":"file","base64_data":"aGVsbG8="}`,
			expected: &sendFileToolResult{
				Status:     "success",
				FileName:   "report.txt",
				FileType:   "file",
				Base64Data: "aGVsbG8=",
			},
		},
		{
			name:     "status failure",
			input:    `{"status":"failed","error":"something went wrong"}`,
			expected: nil,
		},
		{
			name:     "invalid json",
			input:    `{invalid`,
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := parseSendFileResult(tc.input)
			if tc.expected == nil {
				if res != nil {
					t.Errorf("expected nil result, got %+v", res)
				}
			} else {
				if res == nil {
					t.Fatalf("expected non-nil result, got nil")
				}
				if res.Status != tc.expected.Status ||
					res.FileName != tc.expected.FileName ||
					res.FileType != tc.expected.FileType ||
					res.Base64Data != tc.expected.Base64Data ||
					res.URL != tc.expected.URL {
					t.Errorf("got %+v, want %+v", res, tc.expected)
				}
			}
		})
	}
}

func TestQQBotAdapter_AskStream_InterceptsSendFile(t *testing.T) {
	// 1. Configure FakeLLM to return a tool call to SendFile.
	fake := &agent.FakeLLM{}
	fake.ToolCallsByTurn = [][]llm.ToolCall{
		{
			{
				ID: "call_send_file",
				Function: llm.FunctionCall{
					Name:      "SendFile",
					Arguments: `{"path":"report.txt"}`,
				},
			},
		},
	}
	// The second turn of the LLM will reply with content.
	fake.StreamDeltas = [][]string{
		{}, // turn 0: tool calls
		{"Here is your report."}, // turn 1: text response
	}

	// Register SendFile tool to agent
	testTool := &mockSendFileTool{
		result: `{"status":"success","file_name":"report.txt","file_type":"file","base64_data":"aGVsbG8="}`,
	}
	
	log, err := logger.New(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	// Create agent with mock tool
	agentDef := agent.Definition{
		ID: "test-agent",
	}
	aWithTool := agent.NewAgent(agentDef, fake, log, agent.WithTools(testTool))
	if err := aWithTool.Start(context.Background()); err != nil {
		t.Fatalf("failed to start agent: %v", err)
	}
	t.Cleanup(func() { _ = aWithTool.Stop(time.Second) })

	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	mgr := NewSessionManager(func(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		return aWithTool, cw, nil, nil
	}, log)

	_, err = mgr.Init(context.Background(), "t1")
	if err != nil {
		t.Fatalf("Init session failed: %v", err)
	}

	adapter := NewQQBotAdapter(mgr, log)

	// Run AskStream
	result, err := adapter.AskStream(context.Background(), "please send report", nil)
	if err != nil {
		t.Fatalf("AskStream failed: %v", err)
	}

	if result.Content != "Here is your report." {
		t.Errorf("content = %q, want 'Here is your report.'", result.Content)
	}

	// Verify MediaList contains the file attachment
	if len(result.MediaList) != 1 {
		t.Fatalf("MediaList size = %d, want 1", len(result.MediaList))
	}

	media := result.MediaList[0]
	if media.FileType != 4 { // FileTypeFile is 4
		t.Errorf("media.FileType = %d, want 4", media.FileType)
	}
	if media.Base64Data != "aGVsbG8=" {
		t.Errorf("media.Base64Data = %q, want 'aGVsbG8='", media.Base64Data)
	}
	if media.URL != "" {
		t.Errorf("media.URL = %q, want empty", media.URL)
	}
}

// mockSendFileTool matches the tools.Tool interface
type mockSendFileTool struct {
	result string
}

func (m *mockSendFileTool) Name() string { return "SendFile" }
func (m *mockSendFileTool) Description() string { return "Mock SendFile" }
func (m *mockSendFileTool) Parameters() json.RawMessage { return nil }
func (m *mockSendFileTool) Execute(ctx context.Context, args string) (string, error) {
	return m.result, nil
}

// Ensure qqbot and tools are imported and used
var _ qqbot.PendingMedia
var _ tools.Tool = (*mockSendFileTool)(nil)
