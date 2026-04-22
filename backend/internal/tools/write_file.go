package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/agent"
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
//   - 沙箱：path 必须落在 AllowedDirs
//   - 大小：len(content) > MaxWriteSize → ErrContentTooLarge
//   - 父目录必须已存在（不自动 MkdirAll；避免 LLM 误造目录树）
//   - 原子性：atomicWrite(tmp + rename)；失败无残留 tmp
type writeFileTool struct {
	cfg Config
}

func newWriteFileTool(cfg Config) *writeFileTool { return &writeFileTool{cfg: cfg} }

func (writeFileTool) Name() string { return "write_file" }

func (writeFileTool) Description() string {
	return "Atomically write a UTF-8 text file within the sandbox. " +
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

	return writeFileImpl(t.cfg, a.Path, a.Content, overwrite)
}

// writeFileImpl 是内部实现；multi_write 直接调用以保证语义一致
func writeFileImpl(cfg Config, path, content string, overwrite bool) (string, error) {
	abs, err := resolveSandbox(cfg.AllowedDirs, path)
	if err != nil {
		return "", err
	}

	if cfg.MaxWriteSize > 0 && int64(len(content)) > cfg.MaxWriteSize {
		return "", fmt.Errorf("%w: %d bytes > %d", ErrContentTooLarge, len(content), cfg.MaxWriteSize)
	}

	created, err := atomicWrite(abs, []byte(content), overwrite)
	if err != nil {
		return "", err
	}

	out := writeFileResult{
		Path:    abs,
		Size:    int64(len(content)),
		Created: created,
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// Compile-time check
var _ agent.Tool = (*writeFileTool)(nil)
