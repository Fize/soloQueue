package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// sendFileTool shares a local file (by path) or a public URL with the user.
// It returns metadata about the file; actual delivery is handled by the
// consumer layer (QQ bot adapter, web UI, etc.).
type sendFileTool struct {
	cfg    Config
	logger *logger.Logger
}

func newSendFileTool(cfg Config) *sendFileTool {
	ensureExecutor(&cfg)
	return &sendFileTool{cfg: cfg, logger: cfg.Logger}
}

func (sendFileTool) Name() string { return "SendFile" }

func (sendFileTool) Description() string {
	return "Send a local file or a public URL to the user via the chat channel (e.g., images, CSV files, text logs)."
}

func (sendFileTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "path":{"type":"string","description":"Local path to the file in the workspace (e.g. plot.png, logs/build.txt)."},
    "url":{"type":"string","description":"Public URL of the file to send (alternative to path)."},
    "file_type":{"type":"string","enum":["image","video","voice","file"],"description":"The type of file. If omitted, it will be automatically detected from the file extension."}
  }
}`)
}

type sendFileArgs struct {
	Path     string `json:"path,omitempty"`
	URL      string `json:"url,omitempty"`
	FileType string `json:"file_type,omitempty"`
}

type sendFileResult struct {
	Status   string `json:"status"`
	FileName string `json:"file_name"`
	FileType string `json:"file_type"`
	Path     string `json:"path,omitempty"`
	URL      string `json:"url,omitempty"`
}

func (t *sendFileTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	if !iface.MediaDeliveryFromContext(ctx) && !iface.IsQBotFromContext(ctx) {
		return "", fmt.Errorf("sendfile tool is only available for QQ Bot channel")
	}

	var a sendFileArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}

	if a.Path == "" && a.URL == "" {
		return "", fmt.Errorf("%w: must provide either 'path' or 'url'", ErrInvalidArgs)
	}
	if a.Path != "" && a.URL != "" {
		return "", fmt.Errorf("%w: cannot provide both 'path' and 'url'", ErrInvalidArgs)
	}

	var fileType string
	var fileName string
	var path string
	var url string

	if a.Path != "" {
		abs, err := absPath(a.Path, t.cfg.WorkDir)
		if err != nil {
			return "", err
		}

		exported, err := t.cfg.Executor.ExportFile(ctx, abs)
		if err != nil {
			return "", fmt.Errorf("failed to export file %s: %v", abs, err)
		}

		fileName = filepath.Base(abs)
		path, err = t.previewableExport(ctx, exported)
		if err != nil {
			return "", fmt.Errorf("failed to prepare file preview %s: %v", abs, err)
		}

		if a.FileType != "" {
			fileType = a.FileType
		} else {
			fileType = detectFileType(fileName)
		}

		out := sendFileResult{
			Status:   "success",
			FileName: fileName,
			FileType: fileType,
			Path:     path,
		}
		if t.logger != nil {
			t.logger.InfoContext(ctx, logger.CatTool, "send_file: completed",
				"file_name", fileName, "file_type", fileType)
		}
		b, _ := json.Marshal(out)
		return string(b), nil
	} else {
		url = a.URL
		fileName = filepath.Base(url)
		// Clean query parameters from filename if any
		if idx := strings.Index(fileName, "?"); idx != -1 {
			fileName = fileName[:idx]
		}

		if a.FileType != "" {
			fileType = a.FileType
		} else {
			fileType = detectFileType(fileName)
		}
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "send_file: completed",
			"file_name", fileName, "file_type", fileType)
	}

	out := sendFileResult{
		Status:   "success",
		FileName: fileName,
		FileType: fileType,
		Path:     path,
		URL:      url,
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func (t *sendFileTool) previewableExport(ctx context.Context, source string) (string, error) {
	root := workRootFromPlanDir(t.cfg.PlanDir)
	if t.cfg.PlanDir == "" || isPreviewablePath(source, root, t.cfg.WorkDir) {
		return source, nil
	}
	dir := filepath.Join(root, "artifacts", "sendfile")
	if err := t.cfg.Executor.MkdirAll(ctx, dir); err != nil {
		return "", err
	}
	readResult, err := t.cfg.Executor.ReadFile(ctx, source, ReadFileOptions{})
	if err != nil {
		return "", err
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	finalPath := filepath.Join(dir, "sendfile-"+hex.EncodeToString(nonce[:])+"-"+filepath.Base(source))
	if _, err := t.cfg.Executor.WriteFile(ctx, finalPath, readResult.Data, WriteFileOptions{}); err != nil {
		return "", err
	}
	return finalPath, nil
}

func workRootFromPlanDir(planDir string) string {
	current := filepath.Clean(planDir)
	for current != "." && current != string(filepath.Separator) {
		if filepath.Base(current) == "plan" {
			return filepath.Dir(current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return filepath.Dir(filepath.Clean(planDir))
}

func isPreviewablePath(path, root, workDir string) bool {
	knownRoots := []string{
		filepath.Join(root, "plan"), filepath.Join(root, "downloads"), filepath.Join(root, "workspace"),
		filepath.Join(root, "images"), filepath.Join(root, "artifacts"), filepath.Join(root, "explore"), filepath.Join(root, "design"),
	}
	if cleanedWorkDir := filepath.Clean(workDir); workDir != "" && cleanedWorkDir != filepath.Clean(root) {
		knownRoots = append(knownRoots, cleanedWorkDir)
	}
	cleanedPath := filepath.Clean(path)
	for _, candidate := range knownRoots {
		rel, err := filepath.Rel(filepath.Clean(candidate), cleanedPath)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func detectFileType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return "image"
	case ".mp4", ".avi", ".mov", ".mkv", ".webm":
		return "video"
	case ".silk", ".wav", ".mp3", ".flac", ".amr":
		return "voice"
	default:
		return "file"
	}
}

// Compile-time check
var _ Tool = (*sendFileTool)(nil)
