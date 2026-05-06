package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/sandbox"
)

// grepTool 在目录下按 Go 正则搜索
//
// Schema:
//
//	{
//	  "pattern":"...",           // Go regexp
//	  "dir":"...",               // 搜索根目录
//	  "glob":"**/*.go"           // 可选文件名过滤（doublestar）
//	}
//
// 行为：
//   - Walk dir；对每个 regular file 逐行 scan；命中 Pattern 记一条
//   - 单行超 MaxLineLen 时内容截断（尾部追加 "…"）
//   - 累计匹配超 MaxMatches 时提前返回，truncated=true
//   - 每 256 次循环检查 ctx.Err()；长目录下也能及时响应 cancel
//   - 二进制文件跳过（前 512 字节含 NUL 即视为 binary）
//
// 限制：
//   - 不支持 -C/-B 上下文（LLM 通常可用同 tool 再读文件拿上下文）
//   - 仅匹配 UTF-8 文本；非 UTF-8 文件按字节跑正则（可能乱但不崩）
type grepTool struct {
	cfg    Config
	logger *logger.Logger
}

func newGrepTool(cfg Config) *grepTool {
	ensureExecutor(&cfg)
	return &grepTool{cfg: cfg, logger: cfg.Logger}
}

func (grepTool) Name() string { return "Grep" }

func (grepTool) Description() string {
	return "Search for a Go-regex pattern across files under dir. " +
		"Optionally filter file names with a doublestar glob. " +
		"Returns {matches:[{file,line,text}],truncated}. Binary files are skipped."
}

func (grepTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "pattern":{"type":"string","description":"Go regexp pattern"},
    "dir":{"type":"string","description":"Directory to search"},
    "glob":{"type":"string","description":"Optional file-name pattern (doublestar, supports **)"}
  },
  "required":["pattern","dir"]
}`)
}

type grepArgs struct {
	Pattern string `json:"pattern"`
	Dir     string `json:"dir"`
	Glob    string `json:"glob,omitempty"`
}

type grepMatch struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type grepResult struct {
	Matches   []grepMatch `json:"matches"`
	Truncated bool        `json:"truncated"`
}

func (t *grepTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	var a grepArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("pattern", a.Pattern); err != nil {
		return "", err
	}
	if err := validateNotZeroLen("dir", a.Dir); err != nil {
		return "", err
	}

	_, err := regexp.Compile(a.Pattern)
	if err != nil {
		return "", fmt.Errorf("%w: invalid pattern: %v", ErrInvalidArgs, err)
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

	maxMatches := t.cfg.MaxMatches
	if maxMatches <= 0 {
		maxMatches = 100
	}
	maxLine := t.cfg.MaxLineLen
	if maxLine <= 0 {
		maxLine = 500
	}

	grepMatches, err := t.cfg.Executor.Grep(ctx, absDir, a.Pattern, sandbox.GrepOptions{
		MaxMatches:  maxMatches,
		MaxLineLen:  maxLine,
		GlobPattern: a.Glob,
	})
	if err != nil {
		return "", err
	}

	res := grepResult{}
	for _, m := range grepMatches {
		res.Matches = append(res.Matches, grepMatch{
			File: m.File,
			Line: m.Line,
			Text: m.Content,
		})
	}
	if len(res.Matches) >= maxMatches {
		res.Truncated = true
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "grep: completed",
			"pattern", a.Pattern, "dir", absDir,
			"matches", len(res.Matches), "truncated", res.Truncated)
	}

	b, _ := json.Marshal(res)
	return string(b), nil
}

// Compile-time check
var _ Tool = (*grepTool)(nil)
