package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/sandbox"
)

// writeFileTool 原子写入单个文件
//
// Schema:
//
//	{
//	  "path":"...",
//	  "content":"...",
//	  "overwrite":true   // 默认 true；false 时目标存在 → ErrFileExists
//	}
//
// 安全：
//   - 大小：len(content) > MaxWriteSize → ErrContentTooLarge
//   - 父目录必须已存在（不自动 MkdirAll；避免 LLM 误造目录树）
//   - 原子性：atomicWrite(tmp + rename)；失败无残留 tmp
type writeFileTool struct {
	cfg    Config
	logger *logger.Logger
}

func newWriteFileTool(cfg Config) *writeFileTool {
	ensureExecutor(&cfg)
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
	// Overwrite 用指针以区分 "字段缺失（默认 true）" vs "显式传 false"
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

// writeFileImpl 是内部实现；multi_write 直接调用以保证语义一致
func writeFileImpl(ctx context.Context, cfg Config, path, content string, overwrite bool) (string, error) {
	abs, err := absPath(path)
	if err != nil {
		return "", err
	}

	if cfg.MaxWriteSize > 0 && int64(len(content)) > cfg.MaxWriteSize {
		return "", fmt.Errorf("%w: %d bytes > %d", ErrContentTooLarge, len(content), cfg.MaxWriteSize)
	}

	// 检查父目录是否存在
	dir := filepath.Dir(abs)
	fi, err := cfg.Executor.Stat(ctx, dir)
	if err != nil || !fi.IsDir {
		// If the target path is under PlanDir, auto-create intermediate directories
		if cfg.PlanDir != "" && strings.HasPrefix(abs, cfg.PlanDir+string(filepath.Separator)) {
			if mkdirErr := ensureDir(ctx, cfg.Executor, dir); mkdirErr != nil {
				return "", fmt.Errorf("auto-create plan dir %s: %w", dir, mkdirErr)
			}
		} else {
			return "", fmt.Errorf("%w: %s", ErrParentDirMissing, dir)
		}
	}

	wr, err := cfg.Executor.WriteFile(ctx, abs, []byte(content), sandbox.WriteFileOptions{
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

// ensureDir creates the directory (and any missing parents) via the Executor.
// It uses RunCommand("mkdir -p") which works for both LocalExecutor and DockerExecutor.
func ensureDir(ctx context.Context, exec sandbox.Executor, dir string) error {
	_, err := exec.RunCommand(ctx, "mkdir -p "+shellQuoteDir(dir), sandbox.RunCommandOptions{})
	if err != nil {
		return err
	}
	return nil
}

// shellQuoteDir quotes a directory path for safe shell usage.
// Wraps in single quotes, escaping any embedded single quotes.
func shellQuoteDir(s string) string {
	// Replace ' with '\'' (end quote, escaped quote, start quote)
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// CheckConfirmation 实现 Confirmable：写文件始终需要确认。
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
		// 尝试从 raw 中至少提取 path（JSON 前半截通常包含 path 字段）
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

// ConfirmationOptions 实现 Confirmable：二元确认。
func (t *writeFileTool) ConfirmationOptions(_ string) []string { return nil }

// ConfirmArgs 实现 Confirmable：无需修改 args。
func (t *writeFileTool) ConfirmArgs(original string, choice ConfirmChoice) string {
	if choice != ChoiceApprove {
		return original
	}
	return original
}

// SupportsSessionWhitelist 实现 Confirmable：支持 allow-in-session。
func (t *writeFileTool) SupportsSessionWhitelist() bool { return true }

// Compile-time checks
var _ Tool = (*writeFileTool)(nil)
var _ Confirmable = (*writeFileTool)(nil)
