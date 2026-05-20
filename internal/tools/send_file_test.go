package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkSendFileTool(t *testing.T, maxSize int64) (*sendFileTool, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		MaxFileSize: maxSize,
		WorkDir:     dir,
	}
	return newSendFileTool(cfg), dir
}

func TestSendFile_HappyLocalPath(t *testing.T) {
	tool, dir := mkSendFileTool(t, 1024)
	path := filepath.Join(dir, "test_image.png")
	content := []byte("fake image data")
	_ = os.WriteFile(path, content, 0o644)

	// Execute with only path
	args, _ := json.Marshal(sendFileArgs{Path: path})
	out, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var res sendFileResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if res.Status != "success" {
		t.Errorf("status = %q, want 'success'", res.Status)
	}
	if res.FileName != "test_image.png" {
		t.Errorf("file_name = %q, want 'test_image.png'", res.FileName)
	}
	if res.FileType != "image" {
		t.Errorf("file_type = %q, want 'image'", res.FileType)
	}
	expectedBase64 := base64.StdEncoding.EncodeToString(content)
	if res.Base64Data != expectedBase64 {
		t.Errorf("base64_data = %q, want %q", res.Base64Data, expectedBase64)
	}
	if res.URL != "" {
		t.Errorf("url should be empty, got %q", res.URL)
	}
}

func TestSendFile_HappyURL(t *testing.T) {
	tool, _ := mkSendFileTool(t, 1024)
	testURL := "https://example.com/assets/report.pdf?token=123"

	// Execute with only url
	args, _ := json.Marshal(sendFileArgs{URL: testURL})
	out, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var res sendFileResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if res.Status != "success" {
		t.Errorf("status = %q, want 'success'", res.Status)
	}
	if res.FileName != "report.pdf" {
		t.Errorf("file_name = %q, want 'report.pdf' (should strip query string)", res.FileName)
	}
	if res.FileType != "file" {
		t.Errorf("file_type = %q, want 'file'", res.FileType)
	}
	if res.URL != testURL {
		t.Errorf("url = %q, want %q", res.URL, testURL)
	}
	if res.Base64Data != "" {
		t.Errorf("base64_data should be empty, got %q", res.Base64Data)
	}
}

func TestSendFile_HappyManualType(t *testing.T) {
	tool, dir := mkSendFileTool(t, 1024)
	path := filepath.Join(dir, "data.txt")
	_ = os.WriteFile(path, []byte("some text"), 0o644)

	// Force file_type to "voice" even if it is a .txt
	args, _ := json.Marshal(sendFileArgs{
		Path:     path,
		FileType: "voice",
	})
	out, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var res sendFileResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if res.FileType != "voice" {
		t.Errorf("file_type = %q, want 'voice'", res.FileType)
	}
}

func TestSendFile_ValidationErrors(t *testing.T) {
	tool, _ := mkSendFileTool(t, 1024)

	// Neither path nor URL
	_, err := tool.Execute(context.Background(), `{"file_type":"image"}`)
	if err == nil || !strings.Contains(err.Error(), "must provide either 'path' or 'url'") {
		t.Errorf("expected error for missing path/url, got %v", err)
	}

	// Both path and URL
	_, err = tool.Execute(context.Background(), `{"path":"a.png", "url":"http://a.png"}`)
	if err == nil || !strings.Contains(err.Error(), "cannot provide both 'path' and 'url'") {
		t.Errorf("expected error for both path/url, got %v", err)
	}
}

func TestSendFile_Detection(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"a.png", "image"},
		{"a.jpg", "image"},
		{"a.jpeg", "image"},
		{"a.gif", "image"},
		{"a.mp4", "video"},
		{"a.mp3", "voice"},
		{"a.silk", "voice"},
		{"a.txt", "file"},
		{"a.zip", "file"},
		{"a", "file"},
	}

	for _, tc := range tests {
		actual := detectFileType(tc.name)
		if actual != tc.expected {
			t.Errorf("detectFileType(%q) = %q, want %q", tc.name, actual, tc.expected)
		}
	}
}

func TestSendFile_Metadata(t *testing.T) {
	tool, _ := mkSendFileTool(t, 1024)
	if tool.Name() != "SendFile" {
		t.Errorf("Name = %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}
	if len(tool.Parameters()) == 0 {
		t.Error("Parameters should not be empty")
	}
}
