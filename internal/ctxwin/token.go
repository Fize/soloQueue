// Package ctxwin 提供纯内存态、基于规则的线性上下文截断器
//
// 包名用 ctxwin（非 context），避免与 Go 标准库 context 包冲突。
// 本项目大量导入 "context" 用于 context.Context，同名会导致必须 alias。
package ctxwin

import (
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

// Tokenizer 封装 tiktoken-go，提供 token 计数能力
//
// 线程安全：内部 enc 是只读的，Count 可以并发调用。
// 使用 cl100k_base 编码，与 DeepSeek 分词近似。
// 仅用于估算新 push 消息的 token 数，大部分计数由 API 精确校准。
type Tokenizer struct {
	enc *tiktoken.Tiktoken
}

var (
	tokenizerOnce sync.Once
	defaultEnc    *tiktoken.Tiktoken
)

// NewTokenizer 创建 Tokenizer（cl100k_base 编码）
//
// 内部使用 sync.Once 确保编码只加载一次（BPE rank 文件 I/O），
// 多次调用返回的 Tokenizer 共享同一个底层编码实例。
func NewTokenizer() *Tokenizer {
	tokenizerOnce.Do(func() {
		defaultEnc, _ = tiktoken.EncodingForModel("gpt-4")
	})
	return &Tokenizer{enc: defaultEnc}
}

// Count 返回 text 的 token 数估算
//
// 对于空字符串返回 0。
// 注意：cl100k_base 对 DeepSeek 是近似值，误差通常在 5-15% 以内。
// 通过 Calibrate 机制，每次 API 调用后用精确值校准，估算误差不累积。
func (t *Tokenizer) Count(text string) int {
	if text == "" || t.enc == nil {
		return 0
	}
	return len(t.enc.Encode(text, nil, nil))
}

// EstimateByLen 基于字符长度快速估算 token 数，不调用 BPE 编码。
//
// 用于截断后重新估算 token 数等对精度要求不高的场景。
// 比率：每 token ≈ 3.3 bytes（cl100k_base 对中英混合的经验值）。
// 误差可接受，因为 Calibrate 会在下次 API 调用时校准。
func (t *Tokenizer) EstimateByLen(text string) int {
	if text == "" {
		return 0
	}
	// 3.3 bytes/token，向上取整避免低估
	return (len(text) + 2) / 3
}
