package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

)

// multiWriteTool 批量写多个文件（尽力而为，非全原子）
//
// Schema:
//
//	{
//	  "files":[
//	    {"path":"a.go","content":"...","overwrite":true},
//	    {"path":"b.go","content":"...","overwrite":false}
//	  ]
//	}
//
// 语义：
//   - 整体预检：
//       * len(files) == 0       → ErrEmptyInput
//       * len(files) > MaxMultiWriteFiles → ErrTooManyFiles
//       * Σ len(content) > MaxMultiWriteBytes → ErrTotalBytesTooLarge
//       * 任一 path 沙箱外      → 立刻返回 ErrPathOutOfSandbox（整体拒绝）
//   - 预检通过后**串行**逐文件调 writeFileImpl；
//     单文件失败只影响该条目（status=error），其他继续写。
//   - 返回 {files:[{path,status,size,created,err}], summary:{total,ok,error}}
type multiWriteTool struct {
	cfg Config
}

func newMultiWriteTool(cfg Config) *multiWriteTool { ensureExecutor(&cfg); return &multiWriteTool{cfg: cfg} }

func (multiWriteTool) Name() string { return "MultiWrite" }

func (multiWriteTool) Description() string {
	return "Write multiple files best-effort (each file atomically; failures per-file do not abort others). " +
		"Returns {files:[{path,status,size,created,err}],summary:{total,ok,error}}."
}

func (multiWriteTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "files":{
      "type":"array",
      "minItems":1,
      "items":{
        "type":"object",
        "properties":{
          "path":{"type":"string"},
          "content":{"type":"string"},
          "overwrite":{"type":"boolean","default":true}
        },
        "required":["path","content"]
      }
    }
  },
  "required":["files"]
}`)
}

type multiWriteArgs struct {
	Files []writeFileArgs `json:"files"`
}

type multiWriteEntry struct {
	Path    string `json:"path"`
	Status  string `json:"status"` // "ok" | "error"
	Size    int64  `json:"size"`
	Created bool   `json:"created"`
	Err     string `json:"err,omitempty"`
}

type multiWriteSummary struct {
	Total int `json:"total"`
	OK    int `json:"ok"`
	Error int `json:"error"`
}

type multiWriteResult struct {
	Files   []multiWriteEntry `json:"files"`
	Summary multiWriteSummary `json:"summary"`
}

func (t *multiWriteTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	var a multiWriteArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if len(a.Files) == 0 {
		return "", fmt.Errorf("%w: files", ErrEmptyInput)
	}

	maxFiles := t.cfg.MaxMultiWriteFiles
	if maxFiles <= 0 {
		maxFiles = 50
	}
	if len(a.Files) > maxFiles {
		return "", fmt.Errorf("%w: %d > %d", ErrTooManyFiles, len(a.Files), maxFiles)
	}

	maxBytes := t.cfg.MaxMultiWriteBytes
	if maxBytes <= 0 {
		maxBytes = 10 << 20
	}
	var total int64
	for i, f := range a.Files {
		if err := validateNotZeroLen(fmt.Sprintf("files[%d].path", i), f.Path); err != nil {
			return "", err
		}
		total += int64(len(f.Content))
	}
	if total > maxBytes {
		return "", fmt.Errorf("%w: %d > %d", ErrTotalBytesTooLarge, total, maxBytes)
	}

	// 预检：沙箱硬边界（任一路径越界 → 整体拒绝）
	for i, f := range a.Files {
		if _, err := resolveSandbox(t.cfg.AllowedDirs, f.Path); err != nil {
			return "", fmt.Errorf("files[%d]: %w", i, err)
		}
	}

	// 通过预检后，逐文件独立写
	res := multiWriteResult{
		Files: make([]multiWriteEntry, len(a.Files)),
	}
	for i, f := range a.Files {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		overwrite := true
		if f.Overwrite != nil {
			overwrite = *f.Overwrite
		}
		out, err := writeFileImpl(t.cfg, f.Path, f.Content, overwrite)
		if err != nil {
			res.Files[i] = multiWriteEntry{
				Path:   f.Path,
				Status: "error",
				Err:    err.Error(),
			}
			res.Summary.Error++
			continue
		}
		var parsed writeFileResult
		// out 是 JSON string；不敏感于解析失败（fallback 用原始字段）
		if uerr := json.Unmarshal([]byte(out), &parsed); uerr != nil {
			// 理论上 writeFileImpl 返回自己序列化的合法 JSON；防御性处理
			res.Files[i] = multiWriteEntry{
				Path:   f.Path,
				Status: "error",
				Err:    errors.New("write succeeded but result unparseable").Error(),
			}
			res.Summary.Error++
			continue
		}
		res.Files[i] = multiWriteEntry{
			Path:    parsed.Path,
			Status:  "ok",
			Size:    parsed.Size,
			Created: parsed.Created,
		}
		res.Summary.OK++
	}
	res.Summary.Total = len(a.Files)

	b, _ := json.Marshal(res)
	return string(b), nil
}

// CheckConfirmation 实现 Confirmable：批量写文件始终需要确认。
func (t *multiWriteTool) CheckConfirmation(raw string) (bool, string) {
	var a multiWriteArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return true, fmt.Sprintf("Write files (unable to parse args). Allow?")
	}
	n := len(a.Files)
	if n <= 3 {
		paths := make([]string, n)
		for i, f := range a.Files {
			paths[i] = f.Path
		}
		return true, fmt.Sprintf("Write %d file(s): %v. Allow?", n, paths)
	}
	return true, fmt.Sprintf("Write %d file(s). Allow?", n)
}

// ConfirmationOptions 实现 Confirmable：二元确认。
func (t *multiWriteTool) ConfirmationOptions(_ string) []string { return nil }

// ConfirmArgs 实现 Confirmable：无需修改 args。
func (t *multiWriteTool) ConfirmArgs(original string, choice ConfirmChoice) string {
	if choice != ChoiceApprove {
		return original
	}
	return original
}

// SupportsSessionWhitelist 实现 Confirmable：支持 allow-in-session。
func (t *multiWriteTool) SupportsSessionWhitelist() bool { return true }

// Compile-time checks
var _ Tool = (*multiWriteTool)(nil)
var _ Confirmable = (*multiWriteTool)(nil)
