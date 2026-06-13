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

	req := httptest.NewRequest("POST", "/api/session/upload", &buf)
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
