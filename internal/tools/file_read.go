package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/sandbox"
)

// fileReadTool 读取单个文件并返回 JSON payload
//
// Schema:
//
//	{
//	  "path": "...absolute or relative path..."
//	}
//
// 沙箱：路径必须落在 Config.AllowedDirs 任一根之内。
// 限制：MaxFileSize（超出返回 ErrFileTooLarge）；含 NUL 字节返回 ErrBinaryContent。
type fileReadTool struct {
	cfg    Config
	logger *logger.Logger
}

func newFileReadTool(cfg Config) *fileReadTool { ensureExecutor(&cfg); return &fileReadTool{cfg: cfg, logger: cfg.Logger} }

func (fileReadTool) Name() string { return "Read" }

func (fileReadTool) Description() string {
	return "Read a UTF-8 text file within the workspace sandbox. Returns {path,size,content}. " +
		"Binary files and files larger than the configured limit are rejected."
}

func (fileReadTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "path":{"type":"string","description":"Absolute path, or relative to the process CWD; must resolve inside AllowedDirs"}
  },
  "required":["path"]
}`)
}

type fileReadArgs struct {
	Path string `json:"path"`
}

type fileReadResult struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Content string `json:"content"`
}

func (t *fileReadTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	var a fileReadArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("path", a.Path); err != nil {
		return "", err
	}

	abs, err := resolveSandbox(t.cfg.AllowedDirs, a.Path)
	if err != nil {
		return "", err
	}

	res, err := t.cfg.Executor.ReadFile(ctx, abs, sandbox.ReadFileOptions{
		MaxSize: t.cfg.MaxFileSize,
	})
	if err != nil {
		return "", err
	}
	data := res.Data
	if looksBinary(data) {
		return "", fmt.Errorf("%w: %s", ErrBinaryContent, abs)
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "file_read: completed",
			"path", abs, "size", len(data))
	}

	out := fileReadResult{
		Path:    abs,
		Size:    int64(len(data)),
		Content: string(data),
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// Compile-time check
var _ Tool = (*fileReadTool)(nil)
