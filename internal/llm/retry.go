package llm

import (
	"context"
	"time"
)

// RetryPolicy 指数退避配置
//
// 行为：
//   - 第一次失败后等 InitialDelay
//   - 每次翻倍（乘 Multiplier），上限 MaxDelay
//   - 最多尝试 MaxRetries 次额外重试（即总尝试次数 = MaxRetries + 1）
//   - MaxRetries = 0 → 不重试
type RetryPolicy struct {
	MaxRetries   int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
}

// Valid 返回 policy 是否可用（主要防止 zero value 除零等）
func (p RetryPolicy) normalize() RetryPolicy {
	if p.Multiplier <= 1.0 {
		p.Multiplier = 2.0
	}
	if p.InitialDelay <= 0 {
		p.InitialDelay = 500 * time.Millisecond
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = 30 * time.Second
	}
	if p.MaxRetries < 0 {
		p.MaxRetries = 0
	}
	return p
}

// RunWithRetry 执行 fn，按 policy 重试
//
// 参数：
//
//	ctx         caller 的 context；cancel 会立即中止 retry（不等 backoff）
//	policy      退避策略
//	shouldRetry 决定某次失败是否 retry；nil = 全部 retry
//	fn          实际要执行的工作
//
// 返回：最后一次 fn 的 error（成功返回 nil）；ctx 取消返回 ctx.Err()
//
// 幂等性：假设 fn 幂等 —— client 必须只在 retry-safe 的阶段（HTTP 响应前）
// 调用这个 helper；body 开始读之后不该再 retry。
func RunWithRetry(
	ctx context.Context,
	policy RetryPolicy,
	shouldRetry func(error) bool,
	fn func(ctx context.Context) error,
) error {
	return RunWithRetryHooks(ctx, policy, shouldRetry, nil, fn)
}

// RunWithRetryHooks 与 RunWithRetry 相同，但允许注入 onRetry 回调
//
// onRetry(attempt, delay, err)：在决定 retry 且 backoff 开始前调用
//   - attempt：刚失败的那次是第几次尝试（从 1 计）
//   - delay：下一次尝试前的 backoff 时长
//   - err：该次失败的 error
//
// 回调只在"确定 retry"的路径触发；若 shouldRetry=false 或 attempt==MaxRetries
// 不再重试，不调用 onRetry。
func RunWithRetryHooks(
	ctx context.Context,
	policy RetryPolicy,
	shouldRetry func(error) bool,
	onRetry func(attempt int, delay time.Duration, err error),
	fn func(ctx context.Context) error,
) error {
	p := policy.normalize()
	delay := p.InitialDelay

	var lastErr error
	for attempt := 0; attempt <= p.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return lastErr
			}
			return err
		}

		err := fn(ctx)
		if err == nil {
			return nil
		}
		lastErr = err

		// 最后一次尝试：不再 retry
		if attempt == p.MaxRetries {
			break
		}
		// 不可重试
		if shouldRetry != nil && !shouldRetry(err) {
			break
		}

		// 回调：告知 caller 本次将 retry，供记录日志 / 指标
		if onRetry != nil {
			onRetry(attempt+1, delay, err)
		}

		// 等待 delay 或 ctx cancel
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return lastErr
		}

		// 下一轮的 delay（指数递增，cap 在 MaxDelay）
		next := time.Duration(float64(delay) * p.Multiplier)
		if next > p.MaxDelay {
			next = p.MaxDelay
		}
		delay = next
	}
	return lastErr
}
