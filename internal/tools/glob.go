package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

// globTool 在沙箱目录下按 doublestar pattern 找文件
//
// Schema:
//
//	{"pattern":"**/*.go", "dir":"..."}
//
// 行为：
//   - dir 必须落在 AllowedDirs
//   - 使用 doublestar.Glob(os.DirFS(dir), pattern)；支持 **、{}、? 等
//   - 匹配数 > MaxGlobItems 时截断并返回 truncated=true
//   - 返回路径**相对 dir**（用 "/" 分隔，跨平台一致）
type globTool struct {
	cfg Config
}

func newGlobTool(cfg Config) *globTool { return &globTool{cfg: cfg} }

func (globTool) Name() string { return "glob" }

func (globTool) Description() string {
	return "Find files by doublestar glob (supports **) under dir within the sandbox. " +
		"Returns {files:[...relative paths...], truncated}."
}

func (globTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "pattern":{"type":"string","description":"doublestar glob, e.g. **/*.go"},
    "dir":{"type":"string","description":"Root dir; must be inside AllowedDirs"}
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

	absDir, err := resolveSandbox(t.cfg.AllowedDirs, a.Dir)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(absDir)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%w: dir is not a directory: %s", ErrInvalidArgs, absDir)
	}

	maxItems := t.cfg.MaxGlobItems
	if maxItems <= 0 {
		maxItems = 1000
	}

	fsys := os.DirFS(absDir)
	// doublestar validates pattern syntax; invalid → error
	matches, err := doublestar.Glob(fsys, a.Pattern)
	if err != nil {
		return "", fmt.Errorf("%w: invalid pattern: %v", ErrInvalidArgs, err)
	}

	res := globResult{}
	for _, m := range matches {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		// doublestar.Glob returns fs-relative paths with '/'; normalize to OS later if caller wants
		// we keep '/' to match doublestar convention (LLM should be robust)
		res.Files = append(res.Files, filepath.ToSlash(m))
		if len(res.Files) >= maxItems {
			res.Truncated = true
			break
		}
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}

// Compile-time check
var _ agent.Tool = (*globTool)(nil)
