package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

const (
	// ReadToolMaxBytes is the maximum file size Read will accept (100 MB).
	ReadToolMaxBytes = 100 << 20
	// ReadDefaultMaxTokens is the default token limit per Read call.
	ReadDefaultMaxTokens = 25000
)

// fileReadTool reads a single file and returns a JSON payload.
//
// Schema:
//
//	{
//	  "path": "...",
//	  "offset": 0,  // optional byte offset
//	  "limit": 25000  // optional max tokens (default 25000)
//	}
//
// Constraints: MaxFileSize is enforced and returns ErrFileTooLarge when exceeded; NUL bytes return ErrBinaryContent.
// A single read returns at most ReadDefaultMaxTokens tokens; if exceeded, the content is truncated and a paging hint is returned.
type fileReadTool struct {
	cfg    Config
	logger *logger.Logger
}

func newFileReadTool(cfg Config) *fileReadTool {
	ensureSandbox(&cfg)
	return &fileReadTool{cfg: cfg, logger: cfg.Logger}
}

func (fileReadTool) Name() string { return "Read" }

func (fileReadTool) Description() string {
	return "Read a UTF-8 text file and return {path,size,content,truncated?,error?}. " +
		"Use offset (byte) and limit (tokens) to paginate. " +
		"Default limit is 25000 tokens. Binary files are rejected. " +
		"Prefer Grep/Glob first to locate content, then Read with offset/limit."
}

func (fileReadTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "path":{"type":"string","description":"Absolute path, or relative to the process CWD"},
    "offset":{"type":"integer","description":"Byte offset to start reading (0-based). Use totalsize from a prior Read to paginate."},
    "limit":{"type":"integer","description":"Max tokens to return (default 25000, capped at 25000). Use smaller values for large files."}
  },
  "required":["path"]
}`)
}

type fileReadArgs struct {
	Path   string `json:"path"`
	Offset int64  `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type fileReadResult struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (t *fileReadTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	var a fileReadArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("path", a.Path); err != nil {
		return "", err
	}

	maxTokens := a.Limit
	if maxTokens <= 0 {
		maxTokens = ReadDefaultMaxTokens
	}
	if maxTokens > ReadDefaultMaxTokens {
		maxTokens = ReadDefaultMaxTokens
	}

	abs, err := absPath(a.Path)
	if err != nil {
		return "", err
	}

	res, err := t.cfg.Sandbox.ReadFile(ctx, abs, ReadFileOptions{
		MaxSize: ReadToolMaxBytes,
	})
	if err != nil {
		return "", err
	}
	data := res.Data
	if a.Offset > 0 {
		if a.Offset >= int64(len(data)) {
			data = nil
		} else {
			data = data[a.Offset:]
			// Skip to next valid UTF-8 rune start
			for len(data) > 0 && !utf8.RuneStart(data[0]) {
				data = data[1:]
			}
		}
	}

	if looksBinary(data) {
		return "", fmt.Errorf("%w: %s", ErrBinaryContent, abs)
	}

	content := string(data)

	// Enforce token limit
	tok := ctxwin.NewTokenizer()
	if tok.Count(content) > maxTokens {
		content = truncateTokens(content, maxTokens, tok)
		if t.logger != nil {
			t.logger.InfoContext(ctx, logger.CatTool, "file_read: truncated",
				"path", abs,
				"max_tokens", maxTokens,
				"original_bytes", len(data),
			)
		}
		out := fileReadResult{
			Path:      abs,
			Size:      int64(len(res.Data)),
			Content:   content,
			Truncated: true,
			Error: fmt.Sprintf(
				"Content exceeds %d-token single-read limit. Use offset/limit to read in chunks "+
					"(e.g. offset=%d, limit=%d for the next page).",
				maxTokens, a.Offset+int64(len(content)), maxTokens,
			),
		}
		b, _ := json.Marshal(out)
		return string(b), nil
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "file_read: completed",
			"path", abs, "size", len(data))
	}

	out := fileReadResult{
		Path:    abs,
		Size:    int64(len(res.Data)),
		Content: content,
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// truncateTokens returns the longest prefix of s that fits within maxTokens.
func truncateTokens(s string, maxTokens int, tok *ctxwin.Tokenizer) string {
	if tok.Count(s) <= maxTokens {
		return s
	}
	// Binary search on rune length
	runes := []rune(s)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if tok.Count(string(runes[:mid])) <= maxTokens {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return string(runes[:lo])
}

// Compile-time check
var _ Tool = (*fileReadTool)(nil)
