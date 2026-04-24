package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

// multiReplaceTool 对同一文件按顺序应用多段替换，原子落盘
//
// Schema:
//
//	{
//	  "path":"...",
//	  "edits":[
//	    {"old_string":"a","new_string":"b","replace_all":false},
//	    {"old_string":"c","new_string":"d","replace_all":true}
//	  ]
//	}
//
// 语义：
//   - 所有 edit 在内存 string 上按顺序生效（第 N 个 edit 在第 N-1 的结果上执行）
//   - 任一 edit 失败（0 匹配 / 歧义匹配 / old==new / old 空）→ 整体失败、文件不变
//   - 通过所有 edit 后，一次性 atomicWrite 落盘
//   - edits 数 > MaxReplaceEdits → ErrTooManyEdits
//   - edits 为空 → ErrEmptyInput
type multiReplaceTool struct {
	cfg Config
}

func newMultiReplaceTool(cfg Config) *multiReplaceTool { return &multiReplaceTool{cfg: cfg} }

func (multiReplaceTool) Name() string { return "multi_replace" }

func (multiReplaceTool) Description() string {
	return "Apply multiple exact-string edits to a single file sequentially and atomically. " +
		"Each edit operates on the result of previous edits. " +
		"Returns {path,applied,size_before,size_after}."
}

func (multiReplaceTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "path":{"type":"string"},
    "edits":{
      "type":"array",
      "minItems":1,
      "items":{
        "type":"object",
        "properties":{
          "old_string":{"type":"string"},
          "new_string":{"type":"string"},
          "replace_all":{"type":"boolean","default":false}
        },
        "required":["old_string","new_string"]
      }
    }
  },
  "required":["path","edits"]
}`)
}

type replaceEdit struct {
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type multiReplaceArgs struct {
	Path  string        `json:"path"`
	Edits []replaceEdit `json:"edits"`
}

type multiReplaceResult struct {
	Path       string `json:"path"`
	Applied    int    `json:"applied"`
	SizeBefore int    `json:"size_before"`
	SizeAfter  int    `json:"size_after"`
}

func (t *multiReplaceTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	var a multiReplaceArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("path", a.Path); err != nil {
		return "", err
	}
	if len(a.Edits) == 0 {
		return "", fmt.Errorf("%w: edits", ErrEmptyInput)
	}
	maxEdits := t.cfg.MaxReplaceEdits
	if maxEdits <= 0 {
		maxEdits = 50
	}
	if len(a.Edits) > maxEdits {
		return "", fmt.Errorf("%w: %d > %d", ErrTooManyEdits, len(a.Edits), maxEdits)
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
	current := before

	for i, e := range a.Edits {
		if e.OldString == "" {
			return "", fmt.Errorf("%w: edits[%d].old_string is empty", ErrInvalidArgs, i)
		}
		if e.OldString == e.NewString {
			return "", fmt.Errorf("%w: edits[%d]", ErrNoopReplace, i)
		}
		count := strings.Count(current, e.OldString)
		if count == 0 {
			return "", fmt.Errorf("%w: edits[%d] in %s", ErrOldStringNotFound, i, abs)
		}
		if count > 1 && !e.ReplaceAll {
			return "", fmt.Errorf("%w: edits[%d] (%d occurrences)", ErrOldStringAmbiguous, i, count)
		}
		n := 1
		if e.ReplaceAll {
			n = -1
		}
		current = strings.Replace(current, e.OldString, e.NewString, n)
	}

	if t.cfg.MaxWriteSize > 0 && int64(len(current)) > t.cfg.MaxWriteSize {
		return "", fmt.Errorf("%w: result %d bytes > %d", ErrContentTooLarge, len(current), t.cfg.MaxWriteSize)
	}

	if _, werr := atomicWrite(abs, []byte(current), true); werr != nil {
		return "", werr
	}

	out := multiReplaceResult{
		Path:       abs,
		Applied:    len(a.Edits),
		SizeBefore: len(before),
		SizeAfter:  len(current),
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// CheckConfirmation 实现 agent.Confirmable：多段替换始终需要确认。
func (t *multiReplaceTool) CheckConfirmation(raw string) (bool, string) {
	var a multiReplaceArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return true, fmt.Sprintf("Apply edits to file (unable to parse args). Allow?")
	}
	return true, fmt.Sprintf("Apply %d edit(s) to %q. Allow?", len(a.Edits), a.Path)
}

// ConfirmationOptions 实现 agent.Confirmable：二元确认。
func (t *multiReplaceTool) ConfirmationOptions(_ string) []string { return nil }

// ConfirmArgs 实现 agent.Confirmable：无需修改 args。
func (t *multiReplaceTool) ConfirmArgs(original string, choice agent.ConfirmChoice) string {
	if choice != agent.ChoiceApprove {
		return original
	}
	return original
}

// SupportsSessionWhitelist 实现 agent.Confirmable：支持 allow-in-session。
func (t *multiReplaceTool) SupportsSessionWhitelist() bool { return true }

// Compile-time checks
var _ agent.Tool = (*multiReplaceTool)(nil)
var _ agent.Confirmable = (*multiReplaceTool)(nil)
