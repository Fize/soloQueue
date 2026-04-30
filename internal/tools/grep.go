package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// grepTool 在沙箱目录下按 Go 正则搜索
//
// Schema:
//
//	{
//	  "pattern":"...",           // Go regexp
//	  "dir":"...",               // 必须落在 AllowedDirs
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

func newGrepTool(cfg Config) *grepTool { return &grepTool{cfg: cfg, logger: cfg.Logger} }

func (grepTool) Name() string { return "Grep" }

func (grepTool) Description() string {
	return "Search for a Go-regex pattern across files under dir (within the sandbox). " +
		"Optionally filter file names with a doublestar glob. " +
		"Returns {matches:[{file,line,text}],truncated}. Binary files are skipped."
}

func (grepTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "pattern":{"type":"string","description":"Go regexp pattern"},
    "dir":{"type":"string","description":"Directory to search; must be inside AllowedDirs"},
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

	re, err := regexp.Compile(a.Pattern)
	if err != nil {
		return "", fmt.Errorf("%w: invalid pattern: %v", ErrInvalidArgs, err)
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

	maxMatches := t.cfg.MaxMatches
	if maxMatches <= 0 {
		maxMatches = 100
	}
	maxLine := t.cfg.MaxLineLen
	if maxLine <= 0 {
		maxLine = 500
	}

	res := grepResult{}
	var visitCount int

	walkErr := filepath.WalkDir(absDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// 权限错误 / 消失的条目：记一次并继续
			return nil
		}
		visitCount++
		if visitCount&0xFF == 0 { // every 256 items
			if cerr := ctx.Err(); cerr != nil {
				return cerr
			}
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		// glob 过滤（相对 absDir）
		if a.Glob != "" {
			rel, rerr := filepath.Rel(absDir, path)
			if rerr != nil {
				return nil
			}
			// doublestar uses '/' separators; normalize
			relSlash := filepath.ToSlash(rel)
			ok, _ := doublestar.PathMatch(a.Glob, relSlash)
			if !ok {
				return nil
			}
		}

		// 上限预检
		if len(res.Matches) >= maxMatches {
			res.Truncated = true
			return filepath.SkipAll
		}

		// 打开 + peek 二进制检测
		f, ferr := os.Open(path)
		if ferr != nil {
			return nil
		}
		defer f.Close()

		// peek first 512
		head := make([]byte, 512)
		n, _ := f.Read(head)
		if looksBinary(head[:n]) {
			return nil
		}
		// 回到起点
		if _, serr := f.Seek(0, 0); serr != nil {
			return nil
		}

		scanner := bufio.NewScanner(f)
		// 给 scanner 放宽默认 token 上限，避免长行直接报 bufio.ErrTooLong
		// 取 max(maxLine*2, 1MB) 作为缓冲，单行仍被截断到 maxLine
		buf := make([]byte, 0, 64*1024)
		cap := maxLine * 4
		if cap < 1<<20 {
			cap = 1 << 20
		}
		scanner.Buffer(buf, cap)

		lineNo := 0
		for scanner.Scan() {
			lineNo++
			text := scanner.Text()
			if !re.MatchString(text) {
				continue
			}
			// 截断
			if len(text) > maxLine {
				text = text[:maxLine] + "…"
			}
			res.Matches = append(res.Matches, grepMatch{
				File: path,
				Line: lineNo,
				Text: text,
			})
			if len(res.Matches) >= maxMatches {
				res.Truncated = true
				return filepath.SkipAll
			}
		}
		return nil
	})

	if walkErr != nil && walkErr != filepath.SkipAll {
		if walkErr == context.Canceled || walkErr == context.DeadlineExceeded {
			return "", walkErr
		}
		// 非严重错误吞：返回收集到的结果（保持向前兼容）
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
