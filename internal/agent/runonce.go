package agent

import (
	"context"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── RunOnce ────────────────────────────────────────────────────────────────

// RunOnce 是包级一次性调用：不启动 goroutine、不经过 mailbox
//
// 适合脚本 / CLI / 单元测试等只需调一次 LLM 的场景。
// 内部消费 runOnceStream 的事件流累积成 content，保持旧 API 签名不变。
//
// def 仅用于 LLMRequest 构造（ModelID / SystemPrompt / 等）。log 可以 nil。
func RunOnce(ctx context.Context, def Definition, client LLMClient, log *logger.Logger, prompt string) (string, error) {
	a := &Agent{Def: def, LLM: client, Log: log}

	out := make(chan AgentEvent, 64)
	go a.runOnceStream(ctx, prompt, out)

	var (
		b            strings.Builder
		finalContent string
		finalErr     error
	)
	for ev := range out {
		switch e := ev.(type) {
		case ContentDeltaEvent:
			b.WriteString(e.Delta)
		case DoneEvent:
			finalContent = e.Content
		case ErrorEvent:
			finalErr = e.Err
		}
	}
	if finalErr != nil {
		return "", finalErr
	}
	if finalContent != "" {
		return finalContent, nil
	}
	return b.String(), nil
}
