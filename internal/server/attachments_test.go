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
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/transcript"
)

// newAttachmentTestMux builds a mux whose L1 session is initialized and whose
// timeline directory exists on disk.
func newAttachmentTestMux(t *testing.T) (*Mux, string) {
	t.Helper()
	workDir := t.TempDir()
	log, _ := logger.System(workDir, logger.WithConsole(false), logger.WithFile(false))
	def := agent.Definition{Name: "test-agent"}
	a := agent.NewAgent(def, &agenttest.FakeLLM{}, log, agent.WithAgentWorkDir(workDir))
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	factory := func(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		return a, cw, nil, nil
	}
	mgr := session.NewSessionManager(factory, log)
	if _, err := mgr.Init(context.Background(), "default"); err != nil {
		t.Fatalf("Init manager: %v", err)
	}
	mux := NewMux(workDir, log, WithSessionManager(mgr))
	t.Cleanup(func() { _ = mux.Close() })
	return mux, workDir
}

// uploadOnce posts one file to the upload endpoint and returns the response.
func uploadOnce(t *testing.T, mux *Mux, sessionID, name, content string) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if sessionID != "" {
		if err := writer.WriteField("session_id", sessionID); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close: %v", err)
	}

	req := newLocalhostRequest("POST", "/api/session/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return resp
}

func TestUploadUsesUniquePathsPerUpload(t *testing.T) {
	mux, workDir := newAttachmentTestMux(t)

	first := uploadOnce(t, mux, "", "image.png", "first")
	second := uploadOnce(t, mux, "", "image.png", "second")

	if first["path"] == second["path"] {
		t.Fatalf("repeated uploads must not share a path: %v", first["path"])
	}
	if first["name"] != "image.png" || second["name"] != "image.png" {
		t.Errorf("upload keeps the original name: %v %v", first["name"], second["name"])
	}
	attachmentsDir := transcript.AttachmentsDir(filepath.Join(workDir, "logs", "timelines", "default"))
	for _, resp := range []map[string]any{first, second} {
		path, _ := resp["path"].(string)
		if filepath.Dir(path) != attachmentsDir {
			t.Errorf("path %s is not in %s", path, attachmentsDir)
		}
	}
	if data, _ := os.ReadFile(first["path"].(string)); string(data) != "first" {
		t.Errorf("first upload content = %q", data)
	}
	if data, _ := os.ReadFile(second["path"].(string)); string(data) != "second" {
		t.Errorf("second upload content = %q", data)
	}
}

func TestDraftUploadsStageOutsideSessions(t *testing.T) {
	mux, workDir := newAttachmentTestMux(t)

	resp := uploadOnce(t, mux, "draft:abc", "image.png", "draft")
	path, _ := resp["path"].(string)
	pendingDir := transcript.AttachmentsDir(filepath.Join(workDir, "logs", "timelines", pendingTimelineDirName))
	if filepath.Dir(path) != pendingDir {
		t.Fatalf("draft upload landed in %s, want %s", filepath.Dir(path), pendingDir)
	}
}

func TestMoveFilesIntoItemBindsAttachmentsToMessage(t *testing.T) {
	mux, workDir := newAttachmentTestMux(t)

	uploaded := uploadOnce(t, mux, "", "image.png", "payload")
	staged, _ := uploaded["path"].(string)

	files := mux.moveFilesIntoItem("l1", "req-42", []ClientFile{{Name: "image.png", Path: staged}})
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	wantDir := transcript.ItemAttachmentsDir(filepath.Join(workDir, "logs", "timelines", "default"), transcript.InputItemID("req-42"))
	if filepath.Dir(files[0].Path) != wantDir {
		t.Fatalf("bound path = %s, want dir %s", files[0].Path, wantDir)
	}
	if data, err := os.ReadFile(files[0].Path); err != nil || string(data) != "payload" {
		t.Fatalf("moved file unreadable: %q err=%v", data, err)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Errorf("staged upload should be gone after binding, err=%v", err)
	}

	// Re-sending the same message must not duplicate or lose the file.
	again := mux.moveFilesIntoItem("l1", "req-42", files)
	if again[0].Path != files[0].Path {
		t.Fatalf("binding is not idempotent: %s != %s", again[0].Path, files[0].Path)
	}
}

func TestValidatePathServesAttachmentsOnly(t *testing.T) {
	mux, workDir := newAttachmentTestMux(t)

	itemDir := transcript.ItemAttachmentsDir(filepath.Join(workDir, "logs", "timelines", "default"), "in:req-1")
	if err := os.MkdirAll(itemDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	attachment := filepath.Join(itemDir, "image.png")
	if err := os.WriteFile(attachment, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := mux.validatePath(attachment); err != nil {
		t.Errorf("attachment should be servable: %v", err)
	}

	timelineFile := filepath.Join(workDir, "logs", "timelines", "default", "timeline-2026-11-26.jsonl")
	if err := os.WriteFile(timelineFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := mux.validatePath(timelineFile); err == nil {
		t.Error("timeline rows must not be servable as files")
	}
}
