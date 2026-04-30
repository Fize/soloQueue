package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreprocessContent_ArgumentsSubstitution(t *testing.T) {
	content := "Hello $ARGUMENTS, how are you?"
	result := PreprocessContent(content, "world", "/tmp")
	if result != "Hello world, how are you?" {
		t.Errorf("got %q, want %q", result, "Hello world, how are you?")
	}
}

func TestPreprocessContent_ArgumentsEmpty(t *testing.T) {
	content := "Hello $ARGUMENTS!"
	result := PreprocessContent(content, "", "/tmp")
	if result != "Hello !" {
		t.Errorf("got %q, want %q", result, "Hello !")
	}
}

func TestPreprocessContent_MultipleArguments(t *testing.T) {
	content := "From: $ARGUMENTS\nTo: $ARGUMENTS"
	result := PreprocessContent(content, "alice", "/tmp")
	if result != "From: alice\nTo: alice" {
		t.Errorf("got %q, want %q", result, "From: alice\nTo: alice")
	}
}

func TestPreprocessContent_ShellExecution(t *testing.T) {
	content := "Current dir: !`pwd`"
	result := PreprocessContent(content, "", "/tmp")
	// 应该替换为实际输出，不是原始 !`pwd`
	if result == content {
		t.Errorf("shell command was not executed, got %q", result)
	}
	if result == "Current dir: !`pwd`" {
		t.Error("shell command should have been replaced with output")
	}
}

func TestPreprocessContent_ShellExecutionFailed(t *testing.T) {
	content := "Result: !`nonexistent_command_xyz_123`"
	result := PreprocessContent(content, "", "/tmp")
	// 失败时应替换为空字符串
	if result != "Result: " {
		t.Errorf("failed shell should produce empty, got %q", result)
	}
}

func TestPreprocessContent_FileRef(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(testFile, []byte("hello from file"), 0o644); err != nil {
		t.Fatal(err)
	}

	content := "Data: @data.txt"
	result := PreprocessContent(content, "", dir)
	if result != "Data: hello from file" {
		t.Errorf("got %q, want %q", result, "Data: hello from file")
	}
}

func TestPreprocessContent_FileRefNotFound(t *testing.T) {
	content := "Data: @nonexistent.txt"
	result := PreprocessContent(content, "", "/tmp")
	// 文件不存在时应包含错误提示
	if result == "Data: @nonexistent.txt" {
		t.Error("file ref should have been expanded or replaced with error")
	}
	if result == "Data: " {
		t.Error("file not found should show error, not empty")
	}
}

func TestPreprocessContent_PipelineOrder(t *testing.T) {
	dir := t.TempDir()
	// $ARGUMENTS 替换在 shell 执行之前
	// 所以 !`echo $ARGUMENTS` 中的 $ARGUMENTS 应该已经被替换
	content := "!`echo hello`"
	result := PreprocessContent(content, "", dir)
	if result != "hello" {
		t.Errorf("got %q, want %q", result, "hello")
	}
}

func TestPreprocessContent_ArgumentsInShell(t *testing.T) {
	dir := t.TempDir()
	// $ARGUMENTS 先被替换，然后 shell 执行
	content := "!`echo $ARGUMENTS`"
	result := PreprocessContent(content, "test-value", dir)
	if result != "test-value" {
		t.Errorf("got %q, want %q", result, "test-value")
	}
}

func TestExpandShellCommands_Timeout(t *testing.T) {
	// 验证 shell 执行不会无限阻塞
	content := "!`sleep 0.1 && echo done`"
	result := PreprocessContent(content, "", "/tmp")
	if result != "done" {
		t.Errorf("got %q, want %q", result, "done")
	}
}

func TestParseAllowedTools_Comprehensive(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"single", "Read", []string{"Read"}},
		{"multiple", "Read,Write,Bash(git:*)", []string{"Read", "Write", "Bash(git:*)"}},
		{"with spaces", " Read , Write ", []string{"Read", "Write"}},
		{"mcp pattern", "mcp__server__tool", []string{"mcp__server__tool"}},
		{"edit pattern", "Edit(src/**/*.ts)", []string{"Edit(src/**/*.ts)"}},
		{"only commas", ",,", nil},
		{"mixed empty", "Read,,Write,", []string{"Read", "Write"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseAllowedTools(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("ParseAllowedTools(%q) = %v, want %v", tt.input, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ParseAllowedTools(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}
