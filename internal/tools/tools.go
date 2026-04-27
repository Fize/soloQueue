// Package tools 汇集 agent 可用的内置业务工具
//
// 设计原则：
//
//   - 所有工具都是 "配置驱动的值对象"：main.go 在启动时构造一个 Config
//     并调用 Build(cfg)，返回可直接传给 agent.WithTools 的 []Tool。
//   - 工具扁平布局（每个 .go 文件一个工具 + 一个 *_test.go）；不做子包。
//     当 tool 数量超过 ~30 或按域划分更有意义时，再做 refactor。
//   - 共享的配置 / helper（sandbox 检查、atomic write）集中在本文件。
//   - 工具 Execute 总返回 JSON 字符串（便于 LLM 解析）或结构化错误；
//     错误由 agent 层格式化为 `"error: ..."` 喂回 LLM，不中断循环。
//
// 典型用法：
//
//	cfg := tools.Config{
//	    AllowedDirs:  []string{"/srv/workspace"},
//	    MaxFileSize:  1 << 20,
//	    MaxWriteSize: 1 << 20,
//	    ...
//	}
//	all := tools.Build(cfg)
//	a := agent.NewAgent(def, llm, log, agent.WithTools(all...))
package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ─── Config ──────────────────────────────────────────────────────────────────

// Config 是所有内置工具的共享配置
//
// 零值语义：所有字段留零值时，Build 仍可调用，但与文件系统 / 网络相关的
// 工具会在 Execute 时按"最严格"处理（例如 AllowedDirs 为空 → 任何路径都被
// ErrPathOutOfSandbox 拒绝）。生产代码应在 main.go 显式填充。
type Config struct {
	// ── 文件系统共享 ────────────────────────────────────────────────

	// AllowedDirs 沙箱根目录列表。读 / 写 / grep / glob 的路径都必须落在其一之内。
	// 空列表 = 禁止所有文件操作（安全默认）。
	AllowedDirs []string

	// ── 读限制 ────────────────────────────────────────────────────

	// MaxFileSize file_read 单文件上限（字节）
	MaxFileSize int64

	// MaxMatches grep 匹配数量上限（超过截断并返回 truncated=true）
	MaxMatches int

	// MaxLineLen grep 单行字符上限（超过截断）
	MaxLineLen int

	// MaxGlobItems glob 匹配文件数上限
	MaxGlobItems int

	// ── 写限制 ────────────────────────────────────────────────────

	// MaxWriteSize write_file / replace / multi_replace 单次写入上限
	MaxWriteSize int64

	// MaxMultiWriteBytes multi_write 所有 Content 之和上限
	MaxMultiWriteBytes int64

	// MaxMultiWriteFiles multi_write 单次最多文件数
	MaxMultiWriteFiles int

	// MaxReplaceEdits multi_replace 单次最多 edit 数
	MaxReplaceEdits int

	// ── http_fetch ────────────────────────────────────────────────

	// HTTPAllowedHosts 若非空，只允许 URL host 命中其中之一
	HTTPAllowedHosts []string

	// HTTPMaxBody 响应体上限（字节）
	HTTPMaxBody int64

	// HTTPTimeout HTTP 请求超时
	HTTPTimeout time.Duration

	// HTTPBlockPrivate 是否拦截私有 / 环回 / 链路本地地址（默认建议 true）
	HTTPBlockPrivate bool

	// ── shell_exec ────────────────────────────────────────────────

	// ShellBlockRegexes 命令黑名单正则（命中即拒绝）
	ShellBlockRegexes []string

	// ShellConfirmRegexes 命令确认名单正则（命中后需用户确认）
	ShellConfirmRegexes []string

	// ShellTimeout 子进程超时
	ShellTimeout time.Duration

	// ShellMaxOutput stdout + stderr 各自上限（字节）
	ShellMaxOutput int64

	// ── web_search (Tavily) ──────────────────────────────────────

	// TavilyAPIKey 空 = Build 跳过 web_search 注册
	TavilyAPIKey string

	// TavilyEndpoint 默认 https://api.tavily.com/search
	TavilyEndpoint string

	// TavilyTimeout 搜索请求超时
	TavilyTimeout time.Duration
}

// ─── Build ────────────────────────────────────────────────────────────────

// Build 返回当前 Config 下启用的所有工具
//
// 规则：
//   - web_search 仅当 TavilyAPIKey 非空时被注册（可选外部依赖）
//   - 其他工具总是返回（不可用的配置由各自 Execute 时报错）
//
// 返回切片顺序保持与声明顺序一致（便于 debug）。
func Build(cfg Config) []Tool {
	out := []Tool{
		newFileReadTool(cfg),
		newGrepTool(cfg),
		newGlobTool(cfg),
		newWriteFileTool(cfg),
		newReplaceTool(cfg),
		newMultiReplaceTool(cfg),
		newMultiWriteTool(cfg),
		newHTTPFetchTool(cfg),
		newShellExecTool(cfg),
	}
	if cfg.TavilyAPIKey != "" {
		out = append(out, newWebSearchTool(cfg))
	}
	return out
}

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

// ─── Atomic write helper ─────────────────────────────────────────────────

// atomicWrite 把 data 原子写入 path
//
// 步骤：
//  1. os.CreateTemp(filepath.Dir(path), ".soloqueue-tmp-*") 在同一目录内起 tmp
//     （rename 必须同盘）
//  2. tmp.Write(data) → tmp.Sync() → tmp.Close()
//  3. 若 overwrite=false 且 path 已存在 → 删除 tmp 并返回 ErrFileExists
//     （竞态窗口：先 Stat 再 Rename；若两者之间别的进程创建了文件，Rename
//     仍会覆盖 —— 本实现用 Stat 做"常见情况"兜底，OS 级强制 O_EXCL 在不同平台
//     对 rename 语义不一致，此处保持简单）
//  4. os.Rename(tmp, path) 原子替换
//  5. 任一环节失败：删 tmp（best-effort）
//
// 返回的 created 表示"目标路径之前不存在"（用于 Tool 返回 payload）。
func atomicWrite(path string, data []byte, overwrite bool) (created bool, err error) {
	dir := filepath.Dir(path)
	// 父目录必须存在（不自动 MkdirAll）
	if fi, statErr := os.Stat(dir); statErr != nil || !fi.IsDir() {
		return false, fmt.Errorf("%w: %s", ErrParentDirMissing, dir)
	}

	// 检查目标是否已存在
	_, statErr := os.Stat(path)
	existed := statErr == nil
	created = !existed

	if existed && !overwrite {
		return false, fmt.Errorf("%w: %s", ErrFileExists, path)
	}

	// 用匿名函数 + defer 保证失败清理
	tmp, err := os.CreateTemp(dir, ".soloqueue-tmp-*")
	if err != nil {
		return false, fmt.Errorf("create tmp: %w", err)
	}
	tmpName := tmp.Name()
	// 失败时清理 tmp；成功 rename 后 tmpName 已不存在，Remove 变成 no-op
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return false, fmt.Errorf("write tmp: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return false, fmt.Errorf("sync tmp: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return false, fmt.Errorf("close tmp: %w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return false, fmt.Errorf("rename tmp → target: %w", err)
	}
	return created, nil
}

// ─── Binary detection ───────────────────────────────────────────────────

// looksBinary 检查前 N 字节内是否含 NUL；返回 true 说明很可能是二进制
//
// 简单启发式（同 git / grep 的近似逻辑）：UTF-8 文本不含 U+0000，
// 因此首 512 字节里的 NUL 是强信号。
func looksBinary(data []byte) bool {
	n := len(data)
	if n > 512 {
		n = 512
	}
	for i := 0; i < n; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

// ─── Read file with size cap ────────────────────────────────────────────

// readFileCapped 读文件，若 > limit 返回 ErrFileTooLarge
//
// 先 Stat 拿大小，避免 OOM；大小 OK 后一次 ReadAll。limit<=0 表示不限。
func readFileCapped(path string, limit int64) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if limit > 0 && fi.Size() > limit {
		return nil, fmt.Errorf("%w: %s (%d bytes > %d)", ErrFileTooLarge, path, fi.Size(), limit)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// ─── Context helper ──────────────────────────────────────────────────────

// ctxErrOrNil 便利函数：ctx 已取消时返回 ctx.Err()，否则 nil
//
// 用在工具循环里（grep walk、glob 迭代）每 N 项做一次检查。
func ctxErrOrNil(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

// validateNotZeroLen 验证 s 至少非空（统一报 ErrInvalidArgs）
func validateNotZeroLen(field, s string) error {
	if s == "" {
		return fmt.Errorf("%w: %s is empty", ErrInvalidArgs, field)
	}
	return nil
}

// ─── Default Config ─────────────────────────────────────────────────────

// DefaultConfig 返回一组推荐的默认值（main.go 可在其基础上覆盖）
//
// 数值来自 plan §5.3；对大部分 local-dev 场景安全。
func DefaultConfig() Config {
	return Config{
		MaxFileSize:  1 << 20,
		MaxMatches:   100,
		MaxLineLen:   500,
		MaxGlobItems: 1000,

		MaxWriteSize:       1 << 20,
		MaxMultiWriteBytes: 10 << 20,
		MaxMultiWriteFiles: 50,
		MaxReplaceEdits:    50,

		HTTPMaxBody:      5 << 20,
		HTTPTimeout:      10 * time.Second,
		HTTPBlockPrivate: true,

		ShellTimeout:   30 * time.Second,
		ShellMaxOutput: 256 << 10,

		TavilyEndpoint: "https://api.tavily.com/search",
		TavilyTimeout:  15 * time.Second,
	}
}
