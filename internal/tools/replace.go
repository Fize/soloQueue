package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// replaceTool 对单个文件做严格字符串替换
//
// Schema:
//
//	{
//	  "path":"...",
//	  "old_string":"...",
//	  "new_string":"...",
//	  "replace_all":false   // 默认 false；false 时要求 old_string 唯一出现
//	}
//
// 匹配规则（严格字符串，不支持正则）：
//   - strings.Count(content, old_string) == 0 → ErrOldStringNotFound
//   - Count > 1 && !ReplaceAll              → ErrOldStringAmbiguous
//   - ReplaceAll=true 时允许任意多次（0 次仍报错）
//   - old_string == ""                       → ErrInvalidArgs
//   - old_string == new_string               → ErrNoopReplace
//
// 原子性：读文件 → strings.Replace 内存中完成 → atomicWrite 一次性写回
type replaceTool struct {
	cfg    Config
	logger *logger.Logger
}

func newReplaceTool(cfg Config) *replaceTool { return &replaceTool{cfg: cfg, logger: cfg.Logger} }

func (replaceTool) Name() string { return "Edit" }

func (replaceTool) Description() string {
	return "Replace exact substrings in a file (no regex). " +
		"With replace_all=false, old_string must match exactly once. " +
		"Returns {path,replacements,size_before,size_after}."
}

func (replaceTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "path":{"type":"string"},
    "old_string":{"type":"string","description":"Exact substring to match; must be non-empty"},
    "new_string":{"type":"string"},
    "replace_all":{"type":"boolean","default":false}
  },
  "required":["path","old_string","new_string"]
}`)
}

type replaceArgs struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type replaceResult struct {
	Path         string `json:"path"`
	Replacements int    `json:"replacements"`
	SizeBefore   int    `json:"size_before"`
	SizeAfter    int    `json:"size_after"`
}

func (t *replaceTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	var a replaceArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("path", a.Path); err != nil {
		return "", err
	}
	if err := validateNotZeroLen("old_string", a.OldString); err != nil {
		return "", err
	}
	if a.OldString == a.NewString {
		return "", ErrNoopReplace
	}

	abs, err := resolveSandbox(t.cfg.AllowedDirs, a.Path)
	if err != nil {
		return "", err
	}
	data, err := readFileCapped(abs, t.cfg.MaxFileSize)
	if err != nil {
		return "", err
	}
	before := string(data)
	count := strings.Count(before, a.OldString)
	if count == 0 {
		return "", fmt.Errorf("%w: in %s", ErrOldStringNotFound, abs)
	}
	if count > 1 && !a.ReplaceAll {
		return "", fmt.Errorf("%w: %d occurrences in %s", ErrOldStringAmbiguous, count, abs)
	}

	n := 1
	if a.ReplaceAll {
		n = -1
	}
	after := strings.Replace(before, a.OldString, a.NewString, n)
	if t.cfg.MaxWriteSize > 0 && int64(len(after)) > t.cfg.MaxWriteSize {
		return "", fmt.Errorf("%w: result %d bytes > %d", ErrContentTooLarge, len(after), t.cfg.MaxWriteSize)
	}

	if _, werr := atomicWrite(abs, []byte(after), true); werr != nil {
		return "", werr
	}

	replacements := count
	if !a.ReplaceAll {
		replacements = 1
	}
	out := replaceResult{
		Path:         abs,
		Replacements: replacements,
		SizeBefore:   len(before),
		SizeAfter:    len(after),
	}
	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "replace: completed",
			"path", abs, "replacements", replacements)
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// CheckConfirmation 实现 Confirmable：替换操作始终需要确认。
func (t *replaceTool) CheckConfirmation(raw string) (bool, string) {
	var a replaceArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return true, fmt.Sprintf("Replace in file (unable to parse args). Allow?")
	}
	oldPreview := truncateString(a.OldString, 40)
	newPreview := truncateString(a.NewString, 40)
	return true, fmt.Sprintf("Replace in %q: %q → %q. Allow?", a.Path, oldPreview, newPreview)
}

// ConfirmationOptions 实现 Confirmable：二元确认。
func (t *replaceTool) ConfirmationOptions(_ string) []string { return nil }

// ConfirmArgs 实现 Confirmable：无需修改 args。
func (t *replaceTool) ConfirmArgs(original string, choice ConfirmChoice) string {
	if choice != ChoiceApprove {
		return original
	}
	return original
}

// SupportsSessionWhitelist 实现 Confirmable：支持 allow-in-session。
func (t *replaceTool) SupportsSessionWhitelist() bool { return true }

// Compile-time checks
var _ Tool = (*replaceTool)(nil)
var _ Confirmable = (*replaceTool)(nil)
