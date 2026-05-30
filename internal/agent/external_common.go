package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

type externalMessageType string

var testExternalBackend externalBackend

const (
	externalMessageText     externalMessageType = "text"
	externalMessageThinking externalMessageType = "thinking"
	externalMessageStatus   externalMessageType = "status"
	externalMessageError    externalMessageType = "error"
	externalMessageLog      externalMessageType = "log"
)

type externalMessage struct {
	Type      externalMessageType
	Content   string
	SessionID string
	Level     string
}

type externalResult struct {
	Status    string
	Output    string
	Error     string
	SessionID string
}

type externalExecOptions struct {
	Cwd             string
	Model           string
	SystemPrompt    string
	ResumeSessionID string
	CustomArgs      []string
	CustomEnv       map[string]string
	Timeout         time.Duration
}

type externalSession struct {
	Messages <-chan externalMessage
	Result   <-chan externalResult
}

type externalBackend interface {
	Execute(ctx context.Context, prompt string, opts externalExecOptions) (*externalSession, error)
}

func buildEnv(customEnv map[string]string) []string {
	env := os.Environ()
	for k, v := range customEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return env
}

func resolveExecutablePath(provider string) string {
	var path string
	upperProv := strings.ToUpper(provider)

	path = os.Getenv("SOLOQUEUE_" + upperProv + "_PATH")
	if path == "" {
		path = os.Getenv("MULTICA_" + upperProv + "_PATH")
	}
	if path == "" {
		path = provider
	}
	return path
}

// logWriter adapts a *logger.Logger to an io.Writer for capturing stderr.
type logWriter struct {
	logger *logger.Logger
	prefix string
}

func newLogWriter(l *logger.Logger, prefix string) *logWriter {
	return &logWriter{logger: l, prefix: prefix}
}

func (w *logWriter) Write(p []byte) (int, error) {
	text := strings.TrimSpace(string(p))
	if text != "" && w.logger != nil {
		w.logger.Debug(logger.CatActor, text)
	}
	return len(p), nil
}

// runExternalCLI executes an external CLI agent subprocess and translates its messages.
func (a *Agent) runExternalCLI(ctx context.Context, prompt string, out chan<- AgentEvent, cw *ctxwin.ContextWindow) bool {
	a.setWorkStart(prompt)
	defer a.clearWork()

	modelID := a.EffectiveModelID()
	execPath := resolveExecutablePath(a.Def.ExternalType)

	customEnv := make(map[string]string)
	for k, v := range a.Def.CustomEnv {
		customEnv[k] = v
	}

	opts := externalExecOptions{
		Cwd:             a.WorkDir,
		Model:           modelID,
		SystemPrompt:    a.Def.SystemPrompt,
		ResumeSessionID: a.externalSessionID,
		CustomArgs:      a.Def.CustomArgs,
		CustomEnv:       customEnv,
		Timeout:         20 * time.Minute,
	}

	var backend externalBackend
	switch a.Def.ExternalType {
	case "claude":
		backend = &claudeBackend{execPath: execPath, logger: a.Log}
	case "codex":
		backend = &codexBackend{execPath: execPath, logger: a.Log}
	case "opencode":
		backend = &opencodeBackend{execPath: execPath, logger: a.Log}
	case "gemini":
		backend = &geminiBackend{execPath: execPath, logger: a.Log}
	case "mock":
		backend = testExternalBackend
		if backend == nil {
			err := fmt.Errorf("mock external backend not set")
			a.emit(ctx, out, ErrorEvent{Err: err})
			a.RecordError(err)
			return false
		}
	default:
		err := fmt.Errorf("unsupported external type: %s", a.Def.ExternalType)
		a.emit(ctx, out, ErrorEvent{Err: err})
		a.RecordError(err)
		return false
	}

	a.logInfo(ctx, logger.CatActor, "executing external agent CLI", "provider", a.Def.ExternalType, "execPath", execPath)

	session, err := backend.Execute(ctx, prompt, opts)
	if err != nil {
		a.emit(ctx, out, ErrorEvent{Err: err})
		a.RecordError(err)
		return false
	}

	var finalContent strings.Builder
	var finalReasoning strings.Builder
	var finalError string
	var finalStatus string

	for {
		select {
		case msg, ok := <-session.Messages:
			if !ok {
				goto waitResult
			}
			switch msg.Type {
			case externalMessageText:
				finalContent.WriteString(msg.Content)
				a.emit(ctx, out, ContentDeltaEvent{Delta: msg.Content})
			case externalMessageThinking:
				finalReasoning.WriteString(msg.Content)
				a.emit(ctx, out, ReasoningDeltaEvent{Delta: msg.Content})
			case externalMessageError:
				finalError = msg.Content
				a.emit(ctx, out, ErrorEvent{Err: errors.New(msg.Content)})
			}
		case <-ctx.Done():
			a.emit(ctx, out, ErrorEvent{Err: ctx.Err()})
			a.RecordError(ctx.Err())
			return false
		}
	}

waitResult:
	select {
	case res := <-session.Result:
		finalStatus = res.Status
		if res.Error != "" {
			finalError = res.Error
		}
		if res.SessionID != "" {
			a.externalSessionID = res.SessionID
		}
	case <-ctx.Done():
		a.emit(ctx, out, ErrorEvent{Err: ctx.Err()})
		a.RecordError(ctx.Err())
		return false
	}

	if finalError != "" {
		err := fmt.Errorf("external agent failed with status %s: %s", finalStatus, finalError)
		a.emit(ctx, out, ErrorEvent{Err: err})
		a.RecordError(err)
		return false
	}

	// Success! Emit DoneEvent
	a.emit(ctx, out, DoneEvent{
		Content:          finalContent.String(),
		ReasoningContent: finalReasoning.String(),
	})

	return false
}
