package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// multiWriteTool writes multiple files in a best-effort, non-atomic manner.
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
// Semantics:
//   - Overall preflight checks:
//   - len(files) == 0 → ErrEmptyInput
//   - len(files) > MaxMultiWriteFiles → ErrTooManyFiles
//   - Σ len(content) > MaxMultiWriteBytes → ErrTotalBytesTooLarge
//   - After preflight passes, each file is written serially via writeFileImpl;
//     a single-file failure only affects that entry (status=error), and the others continue.
//   - Returns {files:[{path,status,size,created,err}], summary:{total,ok,error}}
type multiWriteTool struct {
	cfg Config
}

func newMultiWriteTool(cfg Config) *multiWriteTool {
	ensureSandbox(&cfg)
	return &multiWriteTool{cfg: cfg}
}

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

	// After preflight checks, each file is written independently
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
		out, err := writeFileImpl(ctx, t.cfg, f.Path, f.Content, overwrite)
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
		// out is a JSON string; parsing failure is tolerated (the original fields are used as a fallback)
		if uerr := json.Unmarshal([]byte(out), &parsed); uerr != nil {
			// In theory writeFileImpl returns its own serialized valid JSON; this is a defensive fallback
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

// CheckConfirmation implements Confirmable: multi-file writes always require confirmation.
func (t *multiWriteTool) CheckConfirmation(raw string) (bool, string) {
	var a multiWriteArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return true, "Write files (unable to parse args). Allow?"
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

// ConfirmationOptions implements Confirmable: binary confirmation.
func (t *multiWriteTool) ConfirmationOptions(_ string) []string { return nil }

// ConfirmArgs implements Confirmable: no args modification needed.
func (t *multiWriteTool) ConfirmArgs(original string, choice ConfirmChoice) string {
	if choice != ChoiceApprove {
		return original
	}
	return original
}

// SupportsSessionWhitelist implements Confirmable: supports allow-in-session.
func (t *multiWriteTool) SupportsSessionWhitelist() bool { return true }

// Compile-time checks
var _ Tool = (*multiWriteTool)(nil)
var _ Confirmable = (*multiWriteTool)(nil)
