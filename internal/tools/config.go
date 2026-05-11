// Package tools 汇集 agent 可用的内置业务工具
//
// 设计原则：
//   - 所有工具都是 "配置驱动的值对象"：main.go 在启动时构造一个 Config
//     并调用 Build(cfg)，返回可直接传给 agent.WithTools 的 []Tool。
//   - 工具扁平布局（每个 .go 文件一个工具 + 一个 *_test.go）；不做子包。
//     当 tool 数量超过 ~30 或按域划分更有意义时，再做 refactor。
//   - 共享的配置 / helper（sandbox 检查、atomic write）分别在
//     sandbox.go / helpers.go 集中管理。
//   - 工具 Execute 总返回 JSON 字符串（便于 LLM 解析）或结构化错误；
//     错误由 agent 层格式化为 "error: ..." 喂回 LLM，不中断循环。
//
// 典型用法：
//
//	cfg := tools.Config{
//	    MaxFileSize:  1 << 20,
//	    MaxWriteSize: 1 << 20,
//	}
//	all := tools.Build(cfg)
//	a := agent.NewAgent(def, llm, log, agent.WithTools(all...))
package tools

import (
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/permanent"
	"github.com/xiaobaitu/soloqueue/internal/sandbox"
	"github.com/xiaobaitu/soloqueue/internal/todo"
)

// ─── Config ──────────────────────────────────────────────────────────────────

// Config 是所有内置工具的共享配置
//
// 零值语义：所有字段留零值时，Build 仍可调用，但与文件系统 / 网络相关的
// 工具会在 Execute 时按"最严格"处理。生产代码应在 main.go 显式填充。
type Config struct {
	// ── 读限制 ────────────────────────────────────────────────────

	// MaxFileSize Read 单文件上限（字节）
	MaxFileSize int64

	// MaxMatches grep 匹配数量上限（超过截断并返回 truncated=true）
	MaxMatches int

	// MaxLineLen grep 单行字符上限（超过截断）
	MaxLineLen int

	// MaxGlobItems glob 匹配文件数上限
	MaxGlobItems int

	// ── 写限制 ────────────────────────────────────────────────────

	// MaxWriteSize Write / Edit / MultiEdit 单次写入上限
	MaxWriteSize int64

	// MaxMultiWriteBytes MultiWrite 所有 Content 之和上限
	MaxMultiWriteBytes int64

	// MaxMultiWriteFiles MultiWrite 单次最多文件数
	MaxMultiWriteFiles int

	// MaxReplaceEdits MultiEdit 单次最多 edit 数
	MaxReplaceEdits int

	// ── WebFetch ────────────────────────────────────────────────

	// HTTPAllowedHosts 若非空，只允许 URL host 命中其中之一
	HTTPAllowedHosts []string

	// HTTPMaxBody 响应体上限（字节）
	HTTPMaxBody int64

	// HTTPTimeout HTTP 请求超时
	HTTPTimeout time.Duration

	// HTTPBlockPrivate 是否拦截私有 / 环回 / 链路本地地址（默认建议 true）
	HTTPBlockPrivate bool

	// ── Bash ──────────────────────────────────────────────────

	// ShellBlockRegexes 命令黑名单正则（命中即拒绝）
	ShellBlockRegexes []string

	// ShellConfirmRegexes 命令确认名单正则（命中后需用户确认）
	ShellConfirmRegexes []string

	// ShellMaxOutput shell 输出最大字节数（stdout/stderr 各自截断）
	ShellMaxOutput int64

	// ── WebSearch ─────────────────────────────────────────────
	// WebSearchTimeout 搜索请求超时
	WebSearchTimeout time.Duration

	// ── 日志 ──────────────────────────────────────────────────
	// Logger 可选日志实例（nil = 静默，不输出日志）
	Logger *logger.Logger

	// ── 沙盒执行器 ──────────────────────────────────────────────
	// Executor 是所有工具的执行底座，所有宿主机交互必须通过它。
	// nil 时 Build 会自动注入 LocalExecutor（保障测试和本地开发场景）。
	Executor sandbox.Executor

	// ── Plan Directory ─────────────────────────────────────────────
	// PlanDir is the absolute path to the plan directory (~/.soloqueue/plan/).
	// When non-empty, writeFileImpl will auto-create intermediate directories
	// under this prefix (instead of returning ErrParentDirMissing).
	// Set by main.go via config.PlanDir().
	PlanDir string

	// ── 长期记忆 ──────────────────────────────────────────────
	// PermanentManager 为长期记忆管理器（nil = 未启用）。
	// Remember / RecallMemory 工具仅在非 nil 时生效。
	PermanentManager *permanent.Manager

	// ── Todo 系统 ─────────────────────────────────────────────
	// TodoStore 为 Todo 持久化存储（nil = 未启用）。
	// CreatePlan, UpdatePlan, DeletePlan, AddTodoItems, DeleteTodoItems,
	// ToggleTodo, SetTodoDependencies, ListPlans, GetPlan 工具仅在非 nil 时生效。
	TodoStore *todo.Store

	// ── Image Generation ─────────────────────────────────────
	// ImageModels 图片生成模型列表。只要有一个 Enabled 的模型就注册 ImageGenerate 工具。
	ImageModels []ImgModelCfg
}

// ImgModelCfg 运行时图片模型配置
type ImgModelCfg struct {
	ID           string
	Name         string
	Provider     string
	SecretIdEnv  string
	SecretKeyEnv string
	APIKeyEnv    string
	APIBaseHost  string
	Region       string
	IsDefault    bool
	Enabled      bool
}

// ─── Build ────────────────────────────────────────────────────────────────

// ensureExecutor 保证 cfg.Executor 不为 nil，否则注入 LocalExecutor。
func ensureExecutor(cfg *Config) {
	if cfg.Executor == nil {
		cfg.Executor = sandbox.NewLocalExecutor()
	}
}

// Build 返回当前 Config 下启用的所有工具
//
// 返回切片顺序保持与声明顺序一致（便于 debug）。
// 如果 cfg.Executor 为 nil，自动注入 LocalExecutor。
func Build(cfg Config) []Tool {
	ensureExecutor(&cfg)
	tools := []Tool{
		newFileReadTool(cfg),
		newGrepTool(cfg),
		newGlobTool(cfg),
		newWriteFileTool(cfg),
		newReplaceTool(cfg),
		newMultiReplaceTool(cfg),
		newMultiWriteTool(cfg),
		newHTTPFetchTool(cfg),
		newShellExecTool(cfg),
		newWebSearchTool(cfg),
		newRememberTool(cfg),
		newRecallMemoryTool(cfg),
	}
	if cfg.TodoStore != nil {
		tools = append(tools,
			newCreatePlanTool(cfg),
			newUpdatePlanTool(cfg),
			newDeletePlanTool(cfg),
			newAddTodoItemsTool(cfg),
			newDeleteTodoItemsTool(cfg),
			newToggleTodoTool(cfg),
			newSetTodoDependenciesTool(cfg),
			newListPlansTool(cfg),
			newGetPlanTool(cfg),
		)
	}
	hasImgModel := false
	for _, m := range cfg.ImageModels {
		if m.Enabled {
			hasImgModel = true
			break
		}
	}
	if hasImgModel {
		tools = append(tools, newImageGenTool(cfg))
	}
	return tools
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
		HTTPTimeout:      10 * time.Minute,
		HTTPBlockPrivate: true,

		ShellMaxOutput: 256 << 10,

		WebSearchTimeout: 10 * time.Minute,
	}
}
