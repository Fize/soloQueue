package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ─── Sandbox helper ──────────────────────────────────────────────────────

// resolveSandbox 把 input 路径规范化并校验它落在 AllowedDirs 的某一根之内
//
// 返回值：
//   - abs：清理后的绝对路径（os-native 分隔符）；调用方可直接用于 os.XXX
//   - err：ErrPathOutOfSandbox（含原路径） / os.Stat 错误透传
//
// 策略：
//  1. filepath.Abs 统一到绝对路径（相对路径按 CWD 解析）
//  2. filepath.Clean 去掉 .. / . / 多余分隔符
//  3. 对每个 AllowedDirs 做同样规范化；比较时确保根以 PathSeparator 结尾
//     （避免 /tmp/foo 匹配 /tmp/foobar 的前缀误报）
//
// 注：不做 os.Stat / symlink resolve（会引入读边信道）。符号链接跨沙箱
// 攻击由上层策略管（sandbox 目录不应含外部 symlink；或后续加
// filepath.EvalSymlinks）。
func resolveSandbox(allowed []string, input string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("%w: empty path", ErrInvalidArgs)
	}
	abs, err := filepath.Abs(input)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	abs = filepath.Clean(abs)

	for _, root := range allowed {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rootAbs = filepath.Clean(rootAbs)
		// abs == root OR abs 以 root + PathSeparator 开头
		if abs == rootAbs {
			return abs, nil
		}
		sep := string(os.PathSeparator)
		if strings.HasPrefix(abs, rootAbs+sep) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrPathOutOfSandbox, input)
}
