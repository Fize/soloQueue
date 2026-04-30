package skill

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ─── 预处理管道 ─────────────────────────────────────────────────────────────

// PreprocessConfig 预处理管道的配置
type PreprocessConfig struct {
	// ShellTimeout shell 命令执行超时
	ShellTimeout time.Duration
	// WorkingDir shell 命令的工作目录（默认为 skill.Dir）
	WorkingDir string
}

// DefaultPreprocessConfig 默认预处理配置
func DefaultPreprocessConfig() PreprocessConfig {
	return PreprocessConfig{
		ShellTimeout: 30 * time.Second,
	}
}

var (
	// shellExpandRegex 匹配 !`command` 模式
	shellExpandRegex = regexp.MustCompile("!`([^`]+)`")
	// fileRefRegex 匹配 @filepath 模式
	// 不匹配 @@（转义）和代码块中的 @
	fileRefRegex = regexp.MustCompile("(?:^|[^@])@([\\w./\\-]+)")
)

// PreprocessContent 对 skill 内容执行预处理管道
//
// 管道顺序（与 Claude Code 一致）：
//  1. $ARGUMENTS → 替换为用户传入的 args
//  2. !`command` → 执行 shell 命令并替换为 stdout
//  3. @filepath  → 替换为文件内容
//
// baseDir 用于解析 @filepath 的相对路径和 shell 命令的工作目录。
func PreprocessContent(content, args, baseDir string) string {
	// 1. $ARGUMENTS 替换
	content = strings.ReplaceAll(content, "$ARGUMENTS", args)

	// 2. !`command` shell 执行
	cfg := DefaultPreprocessConfig()
	if baseDir != "" {
		cfg.WorkingDir = baseDir
	}
	content = expandShellCommands(content, cfg)

	// 3. @file 引用
	content = expandFileRefs(content, baseDir)

	return content
}

// expandShellCommands 执行 !`command` 模式的 shell 命令
//
// stdout 输出替换命令行。失败时替换为空字符串（与 CC 行为一致）。
func expandShellCommands(content string, cfg PreprocessConfig) string {
	return shellExpandRegex.ReplaceAllStringFunc(content, func(match string) string {
		// 提取命令内容：!`cmd` → cmd
		cmdStr := shellExpandRegex.FindStringSubmatch(match)[1]
		if cmdStr == "" {
			return ""
		}

		ctx, cancel := context.WithTimeout(context.Background(), cfg.ShellTimeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
		if cfg.WorkingDir != "" {
			cmd.Dir = cfg.WorkingDir
		}

		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = nil // 丢弃 stderr

		if err := cmd.Run(); err != nil {
			// 失败变为空字符串（与 CC 行为一致）
			return ""
		}

		return strings.TrimSpace(stdout.String())
	})
}

// expandFileRefs 解析 @filepath 引用
//
// 文件路径可以是绝对路径或相对于 baseDir 的路径。
// 读取失败时替换为错误提示。
func expandFileRefs(content, baseDir string) string {
	return fileRefRegex.ReplaceAllStringFunc(content, func(match string) string {
		// 提取文件路径：匹配可能包含前导非@字符
		submatch := fileRefRegex.FindStringSubmatch(match)
		if len(submatch) < 2 || submatch[1] == "" {
			return match
		}

		relPath := submatch[1]

		// 解析路径
		var absPath string
		if filepath.IsAbs(relPath) {
			absPath = relPath
		} else {
			absPath = filepath.Join(baseDir, relPath)
		}

		data, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Sprintf("<file error: %s>", err)
		}

		// 保留匹配中的前导字符（非@部分）
		prefix := ""
		if len(match) > 0 && match[0] != '@' {
			prefix = string(match[0])
		}
		return prefix + string(data)
	})
}
