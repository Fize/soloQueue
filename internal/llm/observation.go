package llm

import (
	"context"
	"sync"
)

// CallObserver reports provider transport attempts without coupling transports to storage.
// Unknown transports leave AttemptsReported false rather than inventing request counts.
type CallObserver struct {
	mu          sync.Mutex
	attempts    int
	reported    bool
	instruction func(string)
}
type observerContextKey struct{}

func WithCallObserver(ctx context.Context, o *CallObserver) context.Context {
	return context.WithValue(ctx, observerContextKey{}, o)
}
func NewCallObserver(instruction func(string)) *CallObserver {
	return &CallObserver{instruction: instruction}
}
func ReportAttempt(ctx context.Context) {
	if o, _ := ctx.Value(observerContextKey{}).(*CallObserver); o != nil {
		o.mu.Lock()
		o.attempts++
		o.reported = true
		o.mu.Unlock()
	}
}
func ReportInstructions(ctx context.Context, text string) {
	if o, _ := ctx.Value(observerContextKey{}).(*CallObserver); o != nil && o.instruction != nil {
		o.instruction(text)
	}
}
func (o *CallObserver) Attempts() (int, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.attempts, o.reported
}
