package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/sandbox"
)

// globTool 在目录下按 doublestar pattern 找文件
//
// Schema:
//
//	{"pattern":"**/*.go", "dir":"..."}
//
// 行为：
//   - 使用 doublestar.Glob(os.DirFS(dir), pattern)；支持 **、{}、? 等
//   - 匹配数 > MaxGlobItems 时截断并返回 truncated=true
//   - 返回路径**相对 dir**（用 "/" 分隔，跨平台一致）
type globTool struct {
	cfg    Config
	logger *logger.Logger
}

func newGlobTool(cfg Config) *globTool {
	ensureExecutor(&cfg)
	return &globTool{cfg: cfg, logger: cfg.Logger}
}

func (globTool) Name() string { return "Glob" }

func (globTool) Description() string {
	return "Find files by doublestar glob (supports **) under dir. " +
		"Returns {files:[...relative paths...], truncated}."
}

func (globTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "pattern":{"type":"string","description":"doublestar glob, e.g. **/*.go"},
    "dir":{"type":"string","description":"Root directory to search"}
  },
  "required":["pattern","dir"]
}`)
}

type globArgs struct {
	Pattern string `json:"pattern"`
	Dir     string `json:"dir"`
}

type globResult struct {
	Files     []string `json:"files"`
	Truncated bool     `json:"truncated"`
}

func (t *globTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	var a globArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("pattern", a.Pattern); err != nil {
		return "", err
	}
	if err := validateNotZeroLen("dir", a.Dir); err != nil {
		return "", err
	}

	absDir, err := absPath(a.Dir)
	if err != nil {
		return "", err
	}

	fi, err := t.cfg.Executor.Stat(ctx, absDir)
	if err != nil {
		return "", err
	}
	if !fi.IsDir {
		return "", fmt.Errorf("%w: dir is not a directory: %s", ErrInvalidArgs, absDir)
	}

	maxItems := t.cfg.MaxGlobItems
	if maxItems <= 0 {
		maxItems = 1000
	}

	matches, err := t.cfg.Executor.Glob(ctx, absDir, a.Pattern, sandbox.GlobOptions{
		MaxItems: maxItems,
		Timeout:  globTimeout(ctx),
	})
	if err != nil {
		return "", fmt.Errorf("%w: invalid pattern: %v", ErrInvalidArgs, err)
	}

	res := globResult{}
	truncated := false
	for _, m := range matches {
		// Convert absolute path back to relative
		rel, rerr := filepath.Rel(absDir, m)
		if rerr != nil {
			rel = m
		}
		res.Files = append(res.Files, filepath.ToSlash(rel))
		if len(res.Files) >= maxItems {
			truncated = true
			break
		}
	}
	res.Truncated = truncated
	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "glob: completed",
			"pattern", a.Pattern, "dir", absDir,
			"files", len(res.Files), "truncated", res.Truncated)
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}

// Compile-time check
var _ Tool = (*globTool)(nil)

func globTimeout(ctx context.Context) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 {
			return remaining
		}
	}
	return 0
}
