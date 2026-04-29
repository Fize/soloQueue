package ctxwin

import "context"

// Compactor is the global context compression abstraction.
//
// Any LLM backend can implement this interface as a compression engine.
// ContextWindow calls Compact asynchronously when the soft waterline is crossed,
// compressing the conversation history into a single concise summary.
//
// Implementation notes:
//   - ctx is provided by the caller (asyncCompact uses context.Background())
//   - msgs is a snapshot copy; implementation can safely hold it
//   - The returned summary replaces all messages except messages[0] (system prompt)
//   - On error, ContextWindow keeps its current state unchanged
type Compactor interface {
	Compact(ctx context.Context, msgs []Message) (string, error)
}

// WithCompactor sets the context compressor for async compression.
func WithCompactor(c Compactor) Option {
	return func(cw *ContextWindow) { cw.compactor = c }
}
