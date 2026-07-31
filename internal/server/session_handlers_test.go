package server

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/timeline"
)

func TestHTTP_UploadFile(t *testing.T) {
	workDir := t.TempDir()
	log, _ := logger.System(workDir, logger.WithConsole(false), logger.WithFile(false))

	// Mock Agent
	def := agent.Definition{
		Name: "test-agent",
	}
	fakeLLM := &agent.FakeLLM{}
	a := agent.NewAgent(def, fakeLLM, log, agent.WithAgentWorkDir(workDir))

	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())

	// Create SessionManager and mock the Session
	factory := func(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		return a, cw, nil, nil
	}
	mgr := session.NewSessionManager(factory, log)
	// Initialize L1 session in mgr
	_, err := mgr.Init(context.Background(), "default")
	if err != nil {
		t.Fatalf("Init manager: %v", err)
	}

	mux := NewMux(workDir, log, WithSessionManager(mgr))
	defer mux.Close()

	// Prepare multipart form file
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "test_file.txt")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	_, _ = part.Write([]byte("hello world"))
	writer.Close()

	req := newLocalhostRequest("POST", "/api/session/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if resp.Name != "test_file.txt" {
		t.Errorf("expected test_file.txt, got %s", resp.Name)
	}
	if resp.Size != 11 {
		t.Errorf("expected 11 bytes, got %d", resp.Size)
	}

	// Verify file was saved in workspace/downloads
	expectedPath := filepath.Join(workDir, "downloads", "test_file.txt")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("expected file to exist at %s, but it does not", expectedPath)
	}
}

func TestHTTP_SessionHistory_Delegation(t *testing.T) {
	workDir := t.TempDir()
	log, _ := logger.System(workDir, logger.WithConsole(false), logger.WithFile(false))

	// Create mock timeline directory and file
	timelineDir := filepath.Join(workDir, "logs", "timelines", "default")
	if err := os.MkdirAll(timelineDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	timelinePath := filepath.Join(timelineDir, "timeline-"+time.Now().Format("2006-01-02")+".jsonl")

	// Write the exact 3 events from the user's real log
	events := []string{
		`{"ts":"2026-06-19T09:03:39.426975+08:00","type":"message","msg":{"role":"user","content":"Based on this latest news, analyze the possible market trends after the holiday."}}`,
		`{"ts":"2026-06-19T09:03:49.08664+08:00","type":"message","msg":{"role":"assistant","content":"","reasoning":"thinking...","tool_calls":[{"id":"call_00_CK1ys6vCGZLpb9JPW7S42530","type":"function","name":"delegate_ray-dalio","arguments":"{\"task\":\"## Task: Post-Holiday Market Outlook\",\"work_dir\":\"/InvestLab\"}"}]}}`,
		`{"ts":"2026-06-19T09:03:49.086726+08:00","type":"message","msg":{"role":"tool","content":"","name":"delegate_ray-dalio","tool_call_id":"call_00_CK1ys6vCGZLpb9JPW7S42530","ephemeral":true}}`,
	}

	f, err := os.Create(timelinePath)
	if err != nil {
		t.Fatalf("Create timeline file: %v", err)
	}
	for _, ev := range events {
		_, _ = f.WriteString(ev + "\n")
	}
	f.Close()

	mux := NewMux(workDir, log)
	defer mux.Close()

	req := newLocalhostRequest("GET", "/api/session/history?session_id=l1", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Messages []struct {
			Role     string `json:"role"`
			Segments []struct {
				Type   string `json:"type"`
				Name   string `json:"name"`
				Done   bool   `json:"done"`
				Result string `json:"result"`
			} `json:"segments"`
		} `json:"messages"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	foundToolCall := false
	for _, msg := range resp.Messages {
		if msg.Role == "assistant" {
			for _, seg := range msg.Segments {
				if seg.Type == "tool_call" && seg.Name == "delegate_ray-dalio" {
					foundToolCall = true
					if seg.Done {
						t.Errorf("Expected tool_call segment 'delegate_ray-dalio' to be Done = false, but got true")
					}
				}
			}
		}
	}

	if !foundToolCall {
		t.Errorf("Expected tool_call segment not found in history")
	}
}

func TestHTTP_SessionHistory_DedupPartialFlush(t *testing.T) {
	workDir := t.TempDir()
	log, _ := logger.System(workDir, logger.WithConsole(false), logger.WithFile(false))

	timelineDir := filepath.Join(workDir, "logs", "timelines", "default")
	if err := os.MkdirAll(timelineDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	timelinePath := filepath.Join(timelineDir, "timeline-"+time.Now().Format("2006-01-02")+".jsonl")
	// Reproduces the production duplicate: assistant content written once by
	// the agent's per-iteration push hook (with tool_calls), then again by the
	// session's partial flush (same content, no tool_calls) after a cancel.
	events := []string{
		`{"ts":"2026-07-31T16:37:49+08:00","type":"message","msg":{"role":"user","content":"哈哈"}}`,
		`{"ts":"2026-07-31T16:37:57+08:00","type":"message","msg":{"role":"assistant","content":"哈哈哈别笑","reasoning":"thinking...","tool_calls":[{"id":"call_1","type":"function","name":"delegate","arguments":"{}"}]}}`,
		`{"ts":"2026-07-31T16:37:57+08:00","type":"message","msg":{"role":"tool","content":"","name":"delegate","tool_call_id":"call_1","ephemeral":true}}`,
		`{"ts":"2026-07-31T16:38:23+08:00","type":"message","msg":{"role":"assistant","content":"哈哈哈别笑","reasoning":"thinking..."}}`,
	}

	f, err := os.Create(timelinePath)
	if err != nil {
		t.Fatalf("Create timeline file: %v", err)
	}
	for _, ev := range events {
		_, _ = f.WriteString(ev + "\n")
	}
	f.Close()

	mux := NewMux(workDir, log)
	defer mux.Close()

	req := newLocalhostRequest("GET", "/api/session/history?session_id=l1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Messages []struct {
			Segments []struct {
				Text string `json:"text"`
			} `json:"segments"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	var contents []string
	for _, msg := range resp.Messages {
		for _, seg := range msg.Segments {
			if seg.Text != "" {
				contents = append(contents, seg.Text)
			}
		}
	}
	// "哈哈哈别笑" must appear exactly once (dedup), not twice.
	var dup int
	for _, c := range contents {
		if c == "哈哈哈别笑" {
			dup++
		}
	}
	if dup != 1 {
		t.Errorf("content rendered %d times, want 1; contents=%q", dup, contents)
	}
}

func TestHTTP_SessionHistory_DoesNotDedupLegitRepeats(t *testing.T) {
	workDir := t.TempDir()
	log, _ := logger.System(workDir, logger.WithConsole(false), logger.WithFile(false))

	timelineDir := filepath.Join(workDir, "logs", "timelines", "default")
	if err := os.MkdirAll(timelineDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	timelinePath := filepath.Join(timelineDir, "timeline-"+time.Now().Format("2006-01-02")+".jsonl")
	// Two consecutive assistant rows with identical content but NO tool_calls
	// are not a partial-flush duplicate signature (that requires the previous
	// row to carry tool_calls). Both segments must survive — the dedup guard
	// must not drop legitimate repeated text.
	events := []string{
		`{"ts":"2026-07-31T10:00:00+08:00","type":"message","msg":{"role":"user","content":"hi"}}`,
		`{"ts":"2026-07-31T10:00:05+08:00","type":"message","msg":{"role":"assistant","content":"ok","ts":"2026-07-31T10:00:05+08:00"}}`,
		`{"ts":"2026-07-31T10:00:10+08:00","type":"message","msg":{"role":"assistant","content":"ok","ts":"2026-07-31T10:00:10+08:00"}}`,
	}

	f, err := os.Create(timelinePath)
	if err != nil {
		t.Fatalf("Create timeline file: %v", err)
	}
	for _, ev := range events {
		_, _ = f.WriteString(ev + "\n")
	}
	f.Close()

	mux := NewMux(workDir, log)
	defer mux.Close()

	req := newLocalhostRequest("GET", "/api/session/history?session_id=l1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Messages []struct {
			Segments []struct {
				Text string `json:"text"`
			} `json:"segments"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	var contents []string
	for _, msg := range resp.Messages {
		for _, seg := range msg.Segments {
			if seg.Text != "" {
				contents = append(contents, seg.Text)
			}
		}
	}
	// Both "ok" segments must render (merged into one assistant message).
	var n int
	for _, c := range contents {
		if c == "ok" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("content rendered %d times, want 2 (legit repeat must not be deduped); contents=%q", n, contents)
	}
}

func TestHTTP_SessionHistory_RewindKeepsLaterMessages(t *testing.T) {
	workDir := t.TempDir()
	log, _ := logger.System(workDir, logger.WithConsole(false), logger.WithFile(false))

	timelineDir := filepath.Join(workDir, "logs", "timelines", "default")
	if err := os.MkdirAll(timelineDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	timelinePath := filepath.Join(timelineDir, "timeline-"+time.Now().Format("2006-01-02")+".jsonl")
	events := []string{
		`{"ts":"2026-07-22T18:40:00+08:00","type":"message","msg":{"role":"assistant","content":"earlier reply","ts":"2026-07-22T18:40:00+08:00"}}`,
		`{"ts":"2026-07-22T18:40:12+08:00","type":"message","msg":{"role":"user","content":"typo","ts":"2026-07-22T18:40:12+08:00"}}`,
		`{"ts":"2026-07-22T18:40:18+08:00","type":"control","ctrl":{"action":"rewind","target_ts":["2026-07-22T18:40:12+08:00"]}}`,
		`{"ts":"2026-07-22T18:40:51+08:00","type":"message","msg":{"role":"user","content":"corrected question","ts":"2026-07-22T18:40:51+08:00"}}`,
		`{"ts":"2026-07-22T18:41:45+08:00","type":"message","msg":{"role":"assistant","content":"later reply","ts":"2026-07-22T18:41:45+08:00"}}`,
	}

	f, err := os.Create(timelinePath)
	if err != nil {
		t.Fatalf("Create timeline file: %v", err)
	}
	for _, event := range events {
		_, _ = f.WriteString(event + "\n")
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close timeline file: %v", err)
	}

	mux := NewMux(workDir, log)
	defer mux.Close()

	req := newLocalhostRequest("GET", "/api/session/history?session_id=l1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Messages []struct {
			Segments []struct {
				Text string `json:"text"`
			} `json:"segments"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	var contents []string
	for _, msg := range resp.Messages {
		for _, seg := range msg.Segments {
			if seg.Text != "" {
				contents = append(contents, seg.Text)
			}
		}
	}
	want := []string{"earlier reply", "corrected question", "later reply"}
	if len(contents) != len(want) {
		t.Fatalf("contents = %q, want %q", contents, want)
	}
	for i := range want {
		if contents[i] != want[i] {
			t.Fatalf("contents = %q, want %q", contents, want)
		}
	}
}

func TestHTTP_SessionHistory_Delegation_Completed(t *testing.T) {
	workDir := t.TempDir()
	log, _ := logger.System(workDir, logger.WithConsole(false), logger.WithFile(false))

	// Create mock timeline directory and file
	timelineDir := filepath.Join(workDir, "logs", "timelines", "default")
	if err := os.MkdirAll(timelineDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	timelinePath := filepath.Join(timelineDir, "timeline-"+time.Now().Format("2006-01-02")+".jsonl")

	// Write the exact 4 events (with completion)
	events := []string{
		`{"ts":"2026-06-19T09:03:39.426975+08:00","type":"message","msg":{"role":"user","content":"Based on this latest news, analyze the possible market trends after the holiday."}}`,
		`{"ts":"2026-06-19T09:03:49.08664+08:00","type":"message","msg":{"role":"assistant","content":"","reasoning":"thinking...","tool_calls":[{"id":"call_00_CK1ys6vCGZLpb9JPW7S42530","type":"function","name":"delegate_ray-dalio","arguments":"{\"task\":\"## Task: Post-Holiday Market Outlook\",\"work_dir\":\"/InvestLab\"}"}]}}`,
		`{"ts":"2026-06-19T09:03:49.086726+08:00","type":"message","msg":{"role":"tool","content":"","name":"delegate_ray-dalio","tool_call_id":"call_00_CK1ys6vCGZLpb9JPW7S42530","ephemeral":true}}`,
		`{"ts":"2026-06-19T09:04:12.123456+08:00","type":"message","msg":{"role":"user","content":"[Delegation Completed]\n\nTask: ## Task: Post-Holiday Market Outlook\nCallID: call_00_CK1ys6vCGZLpb9JPW7S42530\nResult:\nHere is the market outlook: bullish.\n\n","ephemeral":true}}`,
	}

	f, err := os.Create(timelinePath)
	if err != nil {
		t.Fatalf("Create timeline file: %v", err)
	}
	for _, ev := range events {
		_, _ = f.WriteString(ev + "\n")
	}
	f.Close()

	mux := NewMux(workDir, log)
	defer mux.Close()

	req := newLocalhostRequest("GET", "/api/session/history?session_id=l1", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Messages []struct {
			Role     string `json:"role"`
			Segments []struct {
				Type   string `json:"type"`
				Name   string `json:"name"`
				Done   bool   `json:"done"`
				Result string `json:"result"`
			} `json:"segments"`
		} `json:"messages"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	foundToolCall := false
	for _, msg := range resp.Messages {
		if msg.Role == "assistant" {
			for _, seg := range msg.Segments {
				if seg.Type == "tool_call" && seg.Name == "delegate_ray-dalio" {
					foundToolCall = true
					if !seg.Done {
						t.Errorf("Expected tool_call 'delegate_ray-dalio' to be Done = true, but got false")
					}
					expectedResult := "Here is the market outlook: bullish."
					if seg.Result != expectedResult {
						t.Errorf("Expected result %q, got %q", expectedResult, seg.Result)
					}
				}
			}
		}
	}

	if !foundToolCall {
		t.Errorf("Expected tool_call segment not found in history")
	}
}

func TestHTTP_SessionHistory_Delegation_MultilineTask(t *testing.T) {
	workDir := t.TempDir()
	log, _ := logger.System(workDir, logger.WithConsole(false), logger.WithFile(false))

	timelineDir := filepath.Join(workDir, "logs", "timelines", "default")
	if err := os.MkdirAll(timelineDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	timelinePath := filepath.Join(timelineDir, "timeline-"+time.Now().Format("2006-01-02")+".jsonl")

	events := []string{
		`{"ts":"2026-06-19T10:00:00+08:00","type":"message","msg":{"role":"user","content":"Fix the login bug"}}`,
		`{"ts":"2026-06-19T10:00:05+08:00","type":"message","msg":{"role":"assistant","content":"","tool_calls":[{"id":"call_multiline_001","type":"function","name":"delegate_fixer","arguments":"{\"task\":\"Fix the login bug\\nDetails: CSS broken on line 42\",\"work_dir\":\"/app\"}"}]}}`,
		`{"ts":"2026-06-19T10:00:06+08:00","type":"message","msg":{"role":"tool","content":"","name":"delegate_fixer","tool_call_id":"call_multiline_001","ephemeral":true}}`,
		`{"ts":"2026-06-19T10:01:00+08:00","type":"message","msg":{"role":"user","content":"[Delegation Completed]\n\nTask: Fix the login bug\nDetails: CSS broken on line 42\nCallID: call_multiline_001\nResult:\nFixed by reverting commit abc\n\n","ephemeral":true}}`,
	}

	f, err := os.Create(timelinePath)
	if err != nil {
		t.Fatalf("Create timeline file: %v", err)
	}
	for _, ev := range events {
		_, _ = f.WriteString(ev + "\n")
	}
	f.Close()

	mux := NewMux(workDir, log)
	defer mux.Close()

	req := newLocalhostRequest("GET", "/api/session/history?session_id=l1", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Messages []struct {
			Role     string `json:"role"`
			Segments []struct {
				Type   string `json:"type"`
				Name   string `json:"name"`
				Done   bool   `json:"done"`
				Result string `json:"result"`
			} `json:"segments"`
		} `json:"messages"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	foundToolCall := false
	for _, msg := range resp.Messages {
		if msg.Role == "assistant" {
			for _, seg := range msg.Segments {
				if seg.Type == "tool_call" && seg.Name == "delegate_fixer" {
					foundToolCall = true
					if !seg.Done {
						t.Errorf("Expected tool_call 'delegate_fixer' (multiline task) to be Done = true, but got false")
					}
					expectedResult := "Fixed by reverting commit abc"
					if seg.Result != expectedResult {
						t.Errorf("Expected result %q, got %q", expectedResult, seg.Result)
					}
				}
			}
		}
	}

	if !foundToolCall {
		t.Errorf("Expected tool_call segment 'delegate_fixer' not found in history")
	}
}

func TestHTTP_SessionHistory_Delegation_Synchronous(t *testing.T) {
	workDir := t.TempDir()
	log, _ := logger.System(workDir, logger.WithConsole(false), logger.WithFile(false))

	timelineDir := filepath.Join(workDir, "logs", "timelines", "default")
	if err := os.MkdirAll(timelineDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	timelinePath := filepath.Join(timelineDir, "timeline-"+time.Now().Format("2006-01-02")+".jsonl")

	events := []string{
		`{"ts":"2026-06-20T11:44:55.315425+08:00","type":"message","msg":{"role":"user","content":"Perform a simple arithmetic task."}}`,
		`{"ts":"2026-06-20T11:45:03.704948+08:00","type":"message","msg":{"role":"assistant","content":"","tool_calls":[{"id":"call_00_inbtvPNrbk6b2fdC2Bq77560","type":"function","name":"delegate_agent","arguments":"{\"name\": \"arithmetic-agent\", \"task\": \"Calculate: (37 × 24) + (156 ÷ 12) − 89\", \"work_dir\": \"/Users/xiaobaitu/.soloqueue\"}"}]}}`,
		`{"ts":"2026-06-20T11:45:03.70509+08:00","type":"message","msg":{"role":"tool","content":"**Calculation**: (37 × 24) = 888, (156 ÷ 12) = 13, 888 + 13 = 901, 901 − 89 = 812  \n**Result**: 812","name":"delegate_agent","tool_call_id":"call_00_inbtvPNrbk6b2fdC2Bq77560","ephemeral":true}}`,
	}

	f, err := os.Create(timelinePath)
	if err != nil {
		t.Fatalf("Create timeline file: %v", err)
	}
	for _, ev := range events {
		_, _ = f.WriteString(ev + "\n")
	}
	f.Close()

	mux := NewMux(workDir, log)
	defer mux.Close()

	req := newLocalhostRequest("GET", "/api/session/history?session_id=l1", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Messages []struct {
			Role     string `json:"role"`
			Segments []struct {
				Type   string `json:"type"`
				Name   string `json:"name"`
				Done   bool   `json:"done"`
				Result string `json:"result"`
			} `json:"segments"`
		} `json:"messages"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	foundToolCall := false
	for _, msg := range resp.Messages {
		if msg.Role == "assistant" {
			for _, seg := range msg.Segments {
				if seg.Type == "tool_call" && seg.Name == "delegate_agent" {
					foundToolCall = true
					if !seg.Done {
						t.Errorf("Expected tool_call 'delegate_agent' (synchronous) to be Done = true, but got false")
					}
					expectedResult := "**Calculation**: (37 × 24) = 888, (156 ÷ 12) = 13, 888 + 13 = 901, 901 − 89 = 812  \n**Result**: 812"
					if seg.Result != expectedResult {
						t.Errorf("Expected result %q, got %q", expectedResult, seg.Result)
					}
				}
			}
		}
	}

	if !foundToolCall {
		t.Errorf("Expected tool_call segment 'delegate_agent' not found in history")
	}
}

func TestHTTP_SessionHistory_DeduplicateUserInputs(t *testing.T) {
	workDir := t.TempDir()
	log, _ := logger.System(workDir, logger.WithConsole(false), logger.WithFile(false))

	// Create mock timeline directory and file
	timelineDir := filepath.Join(workDir, "logs", "timelines", "default")
	if err := os.MkdirAll(timelineDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	timelinePath := filepath.Join(timelineDir, "timeline-"+time.Now().Format("2006-01-02")+".jsonl")

	// Write two duplicate user inputs with timestamp diff less than 5 seconds
	events := []string{
		`{"ts":"2026-06-29T14:50:24.000000+08:00","type":"message","msg":{"role":"user","content":"Analyze GigaDevice, how is it doing?"}}`,
		`{"ts":"2026-06-29T14:50:24.300000+08:00","type":"message","msg":{"role":"user","content":"Analyze GigaDevice, how is it doing?"}}`,
		`{"ts":"2026-06-29T14:50:28.000000+08:00","type":"message","msg":{"role":"user","content":"Different question."}}`,
	}

	f, err := os.Create(timelinePath)
	if err != nil {
		t.Fatalf("Create timeline file: %v", err)
	}
	for _, ev := range events {
		_, _ = f.WriteString(ev + "\n")
	}
	f.Close()

	mux := NewMux(workDir, log)
	defer mux.Close()

	req := newLocalhostRequest("GET", "/api/session/history?session_id=l1", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Messages []struct {
			Role     string `json:"role"`
			Segments []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"segments"`
		} `json:"messages"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// We expect 2 user messages (the first duplicate is deduplicated, the third is kept because the content is different)
	userMsgCount := 0
	for _, msg := range resp.Messages {
		if msg.Role == "user" {
			userMsgCount++
		}
	}

	if userMsgCount != 2 {
		t.Errorf("Expected 2 user messages after deduplication, but got %d", userMsgCount)
	}
}
