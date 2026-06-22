package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// multiReplaceTool applies multiple sequential replacements to a single file and writes atomically.
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
// Semantics:
//   - All edits are applied in order to an in-memory string (the Nth edit runs on the result of the previous one).
//   - Any failed edit (0 matches / ambiguous match / old==new / empty old string) aborts the whole operation and leaves the file unchanged.
//   - After all edits succeed, the result is written once via atomicWrite.
//   - edits count > MaxReplaceEdits → ErrTooManyEdits
//   - edits empty → ErrEmptyInput
type multiReplaceTool struct {
	cfg    Config
	logger *logger.Logger
}

func newMultiReplaceTool(cfg Config) *multiReplaceTool {
	ensureSandbox(&cfg)
	return &multiReplaceTool{cfg: cfg, logger: cfg.Logger}
}

func (multiReplaceTool) Name() string { return "MultiEdit" }

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

	abs, err := absPath(a.Path)
	if err != nil {
		return "", err
	}
	readRes, err := t.cfg.Sandbox.ReadFile(ctx, abs, ReadFileOptions{
		MaxSize: t.cfg.MaxFileSize,
	})
	if err != nil {
		return "", err
	}
	data := readRes.Data
	before := string(data)
	current := before

	for i, e := range a.Edits {
		if err := validateOldString(e.OldString, e.NewString, i); err != nil {
			return "", err
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

	if _, werr := t.cfg.Sandbox.WriteFile(ctx, abs, []byte(current), WriteFileOptions{
		Overwrite: true,
		MaxSize:   t.cfg.MaxWriteSize,
	}); werr != nil {
		return "", werr
	}

	out := multiReplaceResult{
		Path:       abs,
		Applied:    len(a.Edits),
		SizeBefore: len(before),
		SizeAfter:  len(current),
	}
	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "multi_replace: completed",
			"path", abs, "edits_applied", len(a.Edits))
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// CheckConfirmation implements Confirmable: multi-edit replacements always require confirmation.
func (t *multiReplaceTool) CheckConfirmation(raw string) (bool, string) {
	var a multiReplaceArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return true, "Apply edits to file (unable to parse args). Allow?"
	}
	return true, fmt.Sprintf("Apply %d edit(s) to %q. Allow?", len(a.Edits), a.Path)
}

// ConfirmationOptions implements Confirmable: binary confirmation.
func (t *multiReplaceTool) ConfirmationOptions(_ string) []string { return nil }

// ConfirmArgs implements Confirmable: no args modification needed.
func (t *multiReplaceTool) ConfirmArgs(original string, choice ConfirmChoice) string {
	if choice != ChoiceApprove {
		return original
	}
	return original
}

// SupportsSessionWhitelist implements Confirmable: supports allow-in-session.
func (t *multiReplaceTool) SupportsSessionWhitelist() bool { return true }

// Compile-time checks
var _ Tool = (*multiReplaceTool)(nil)
var _ Confirmable = (*multiReplaceTool)(nil)
