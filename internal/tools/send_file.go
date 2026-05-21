package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// sendFileTool shares a local file (via Base64) or a public URL with the user.
type sendFileTool struct {
	cfg    Config
	logger *logger.Logger
}

func newSendFileTool(cfg Config) *sendFileTool {
	ensureSandbox(&cfg)
	return &sendFileTool{cfg: cfg, logger: cfg.Logger}
}

func (sendFileTool) Name() string { return "SendFile" }

func (sendFileTool) Description() string {
	return "Send a local file or a public URL to the user via the chat channel (e.g., images, CSV files, text logs)."
}

func (sendFileTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "path":{"type":"string","description":"Local path to the file in the workspace (e.g. plot.png, logs/build.txt)."},
    "url":{"type":"string","description":"Public URL of the file to send (alternative to path)."},
    "file_type":{"type":"string","enum":["image","video","voice","file"],"description":"The type of file. If omitted, it will be automatically detected from the file extension."}
  }
}`)
}

type sendFileArgs struct {
	Path     string `json:"path,omitempty"`
	URL      string `json:"url,omitempty"`
	FileType string `json:"file_type,omitempty"`
}

type sendFileResult struct {
	Status     string `json:"status"`
	FileName   string `json:"file_name"`
	FileType   string `json:"file_type"`
	Base64Data string `json:"base64_data,omitempty"`
	Path       string `json:"path,omitempty"`
	URL        string `json:"url,omitempty"`
}

func (t *sendFileTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	var a sendFileArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}

	if a.Path == "" && a.URL == "" {
		return "", fmt.Errorf("%w: must provide either 'path' or 'url'", ErrInvalidArgs)
	}
	if a.Path != "" && a.URL != "" {
		return "", fmt.Errorf("%w: cannot provide both 'path' and 'url'", ErrInvalidArgs)
	}

	var fileType string
	var fileName string
	var path string
	var url string

	if a.Path != "" {
		abs, err := absPath(a.Path)
		if err != nil {
			return "", err
		}
		fileName = filepath.Base(abs)
		path = abs

		if a.FileType != "" {
			fileType = a.FileType
		} else {
			fileType = detectFileType(fileName)
		}

		data, err := os.ReadFile(abs)
		if err != nil {
			return "", fmt.Errorf("failed to read file: %w", err)
		}

		if fileType == "image" || fileType == "video" || fileType == "voice" {
			enc := base64.StdEncoding.EncodeToString(data)
			out := sendFileResult{
				Status:     "success",
				FileName:   fileName,
				FileType:   fileType,
				Base64Data: enc,
				Path:       path,
			}
			if t.logger != nil {
				t.logger.InfoContext(ctx, logger.CatTool, "send_file: completed",
					"file_name", fileName, "file_type", fileType)
			}
			b, _ := json.Marshal(out)
			return string(b), nil
		}

		out := sendFileResult{
			Status:   "success",
			FileName: fileName,
			FileType: fileType,
			Path:     path,
		}
		if t.logger != nil {
			t.logger.InfoContext(ctx, logger.CatTool, "send_file: completed",
				"file_name", fileName, "file_type", fileType)
		}
		b, _ := json.Marshal(out)
		return string(b), nil
	} else {
		url = a.URL
		fileName = filepath.Base(url)
		// Clean query parameters from filename if any
		if idx := strings.Index(fileName, "?"); idx != -1 {
			fileName = fileName[:idx]
		}

		if a.FileType != "" {
			fileType = a.FileType
		} else {
			fileType = detectFileType(fileName)
		}
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "send_file: completed",
			"file_name", fileName, "file_type", fileType)
	}

	out := sendFileResult{
		Status:   "success",
		FileName: fileName,
		FileType: fileType,
		Path:     path,
		URL:      url,
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func detectFileType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return "image"
	case ".mp4", ".avi", ".mov", ".mkv", ".webm":
		return "video"
	case ".silk", ".wav", ".mp3", ".flac", ".amr":
		return "voice"
	default:
		return "file"
	}
}

// Compile-time check
var _ Tool = (*sendFileTool)(nil)
