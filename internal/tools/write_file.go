package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// writeFileTool atomically writes a single file.
//
// Schema:
//
//	{
//	  "path":"...",
//	  "content":"...",
//	  "overwrite":true   // default true; when false and the target exists → ErrFileExists
//	}
//
// Safety:
//   - Size: len(content) > MaxWriteSize → ErrContentTooLarge
//   - The parent directory must already exist (no automatic MkdirAll to avoid the LLM accidentally creating directory trees)
//   - Atomicity: atomicWrite(tmp + rename); failures leave no temporary file behind.
type writeFileTool struct {
	cfg    Config
	logger *logger.Logger
}

func newWriteFileTool(cfg Config) *writeFileTool {
	ensureSandbox(&cfg)
	return &writeFileTool{cfg: cfg, logger: cfg.Logger}
}

func (writeFileTool) Name() string { return "Write" }

func (writeFileTool) Description() string {
	return "Atomically write a UTF-8 text file. " +
		"Fails if the parent directory doesn't exist. " +
		"Returns {path,size,created}."
}

func (writeFileTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "path":{"type":"string"},
    "content":{"type":"string"},
    "overwrite":{"type":"boolean","default":true,"description":"If false, fail when target exists"}
  },
  "required":["path","content"]
}`)
}

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	// Overwrite is a pointer so we can distinguish "field missing (default true)" from "explicit false"
	Overwrite *bool `json:"overwrite,omitempty"`
}

type writeFileResult struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Created bool   `json:"created"`
}

func (t *writeFileTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	var a writeFileArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("path", a.Path); err != nil {
		return "", err
	}

	overwrite := true
	if a.Overwrite != nil {
		overwrite = *a.Overwrite
	}

	result, err := writeFileImpl(ctx, t.cfg, a.Path, a.Content, overwrite)
	if err != nil {
		return "", err
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "write_file: completed",
			"path", a.Path, "size", len(a.Content))
	}
	return result, nil
}

// writeFileImpl is the internal implementation; multi_write calls it directly to keep semantics consistent.
func writeFileImpl(ctx context.Context, cfg Config, path, content string, overwrite bool) (string, error) {
	abs, err := absPath(path)
	if err != nil {
		return "", err
	}

	if cfg.MaxWriteSize > 0 && int64(len(content)) > cfg.MaxWriteSize {
		return "", fmt.Errorf("%w: %d bytes > %d", ErrContentTooLarge, len(content), cfg.MaxWriteSize)
	}

	// Check whether the parent directory exists
	dir := filepath.Dir(abs)
	fi, err := cfg.Sandbox.Stat(ctx, dir)
	if err != nil || !fi.IsDir {
		// If the target path is under PlanDir, auto-create intermediate directories
		if cfg.PlanDir != "" && strings.HasPrefix(abs, cfg.PlanDir+string(filepath.Separator)) {
			if mkdirErr := ensureDir(dir); mkdirErr != nil {
				return "", fmt.Errorf("auto-create plan dir %s: %w", dir, mkdirErr)
			}
		} else {
			return "", fmt.Errorf("%w: %s", ErrParentDirMissing, dir)
		}
	}

	wr, err := cfg.Sandbox.WriteFile(ctx, abs, []byte(content), WriteFileOptions{
		Overwrite: overwrite,
		MaxSize:   cfg.MaxWriteSize,
	})
	if err != nil {
		return "", err
	}

	out := writeFileResult{
		Path:    abs,
		Size:    int64(len(content)),
		Created: wr.Created,
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// ensureDir creates the directory (and any missing parents).
func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

// CheckConfirmation implements Confirmable: writing files always requires confirmation.
func (t *writeFileTool) CheckConfirmation(raw string) (bool, string) {
	var a writeFileArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		if t.logger != nil {
			preview := raw
			if len(preview) > 200 {
				preview = preview[:200] + "..."
			}
			t.logger.Error(logger.CatTool, "CheckConfirmation: json.Unmarshal failed",
				"error", err.Error(),
				"raw_len", len(raw),
				"raw_preview", preview,
			)
		}
		// Try to extract at least the path from raw (the first half of the JSON usually contains it)
		var partial struct {
			Path string `json:"path"`
		}
		if e := json.Unmarshal([]byte(raw), &partial); e == nil && partial.Path != "" {
			return true, fmt.Sprintf("Write to %q (truncated, %d bytes). Allow?", partial.Path, len(raw))
		}
		return true, fmt.Sprintf("Write file (truncated args, %d bytes: %v). Allow?", len(raw), err)
	}
	size := len(a.Content)
	var sizeStr string
	if size >= 1<<10 {
		sizeStr = fmt.Sprintf("%.1fKB", float64(size)/float64(1<<10))
	} else {
		sizeStr = fmt.Sprintf("%dB", size)
	}
	return true, fmt.Sprintf("Write %s to %q. Allow?", sizeStr, a.Path)
}

// ConfirmationOptions implements Confirmable: binary confirmation.
func (t *writeFileTool) ConfirmationOptions(_ string) []string { return nil }

// ConfirmArgs implements Confirmable: no args modification needed.
func (t *writeFileTool) ConfirmArgs(original string, choice ConfirmChoice) string {
	if choice != ChoiceApprove {
		return original
	}
	return original
}

// SupportsSessionWhitelist implements Confirmable: supports allow-in-session.
func (t *writeFileTool) SupportsSessionWhitelist() bool { return true }

// Compile-time checks
var _ Tool = (*writeFileTool)(nil)
var _ Confirmable = (*writeFileTool)(nil)
