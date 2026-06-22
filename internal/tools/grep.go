package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// grepTool searches a directory using Go regular expressions.
//
// Schema:
//
//	{
//	  "pattern":"...",           // Go regexp
//	  "dir":"...",               // search root directory
//	  "glob":"**/*.go"           // optional filename filter (doublestar)
//	}
//
// Behavior:
//   - Walks the directory and scans each regular file line by line, recording one match per hit.
//   - If a single line exceeds MaxLineLen, the content is truncated and ends with "…".
//   - If the cumulative match count exceeds MaxMatches, execution stops early and truncated=true is returned.
//   - Checks ctx.Err() every 256 iterations so long directory walks can respond promptly to cancellation.
//   - Binary files are skipped (the first 512 bytes containing NUL is treated as binary).
//
// Constraints:
//   - Does not support -C/-B context lines (the LLM can usually read the file again with the same tool to get context).
//   - Only matches UTF-8 text; non-UTF-8 files are scanned as bytes (which may be noisy but will not crash).
type grepTool struct {
	cfg    Config
	logger *logger.Logger
}

func newGrepTool(cfg Config) *grepTool {
	ensureSandbox(&cfg)
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

	fi, err := t.cfg.Sandbox.Stat(ctx, absDir)
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

	grepMatches, err := t.cfg.Sandbox.Grep(ctx, absDir, a.Pattern, GrepOptions{
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
