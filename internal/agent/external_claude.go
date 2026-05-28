package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

var claudeBlockedArgs = map[string]blockedArgMode{
	"--output-format": blockedWithValue, // stream-json output format for daemon communication
	"-o":              blockedWithValue,
	"--resume":        blockedWithValue, // resume is managed by the daemon session resolver
}

type blockedArgMode int

const (
	blockedWithValue blockedArgMode = iota
	blockedFlagOnly
)

type claudeBackend struct {
	execPath string
	logger   *logger.Logger
}

func (b *claudeBackend) Execute(ctx context.Context, prompt string, opts externalExecOptions) (*externalSession, error) {
	execPath := b.execPath
	if execPath == "" {
		execPath = "claude"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("claude executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := b.buildClaudeArgs(opts)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	if b.logger != nil {
		b.logger.Info("agent command", "exec", execPath, "args", args)
	}
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(opts.CustomEnv)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("claude stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("claude stdin pipe: %w", err)
	}
	closeStdin := func() {
		if stdin != nil {
			_ = stdin.Close()
			stdin = nil
		}
	}

	var stderrLogger *logWriter
	if b.logger != nil {
		stderrLogger = newLogWriter(b.logger, "[claude:stderr] ")
	}
	stderrBuf := newStderrTail(stderrLogger, agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		closeStdin()
		cancel()
		return nil, fmt.Errorf("start claude: %w", err)
	}
	if err := writeClaudeInput(stdin, prompt); err != nil {
		closeStdin()
		cancel()
		_ = cmd.Wait()
		return nil, errors.New(withAgentStderr(fmt.Sprintf("write claude input: %v", err), "claude", stderrBuf.Tail()))
	}
	closeStdin()

	if b.logger != nil {
		b.logger.Info("claude started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)
	}

	msgCh := make(chan externalMessage, 256)
	resCh := make(chan externalResult, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		var output strings.Builder
		var sessionID string
		finalStatus := "completed"
		var finalError string

		// Close stdout when context is cancelled to unblock scanner.Scan()
		go func() {
			<-runCtx.Done()
			_ = stdout.Close()
		}()

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var msg claudeSDKMessage
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				continue
			}

			switch msg.Type {
			case "assistant":
				b.handleAssistant(msg, msgCh, &output)
			case "user":
				b.handleUser(msg, msgCh)
			case "system":
				if msg.SessionID != "" {
					sessionID = msg.SessionID
				}
				trySend(msgCh, externalMessage{Type: externalMessageStatus, Content: "running", SessionID: sessionID})
			case "result":
				closeStdin()
				sessionID = msg.SessionID
				if msg.ResultText != "" {
					output.Reset()
					output.WriteString(msg.ResultText)
				}
				if msg.IsError {
					finalStatus = "failed"
					finalError = msg.ResultText
				}
			case "log":
				if msg.Log != nil {
					trySend(msgCh, externalMessage{
						Type:    externalMessageLog,
						Level:   msg.Log.Level,
						Content: msg.Log.Message,
					})
				}
			case "control_request":
				b.handleControlRequest(msg, stdin)
			}
		}

		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("claude timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if exitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("claude exited with error: %v", exitErr)
		}

		if finalError != "" {
			finalError = withAgentStderr(finalError, "claude", stderrBuf.Tail())
		}

		if b.logger != nil {
			b.logger.Info("claude finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())
		}

		reportedSessionID := resolveSessionID(opts.ResumeSessionID, sessionID, finalStatus == "failed")

		resCh <- externalResult{
			Status:    finalStatus,
			Output:    output.String(),
			Error:     finalError,
			SessionID: reportedSessionID,
		}
	}()

	return &externalSession{Messages: msgCh, Result: resCh}, nil
}

func (b *claudeBackend) buildClaudeArgs(opts externalExecOptions) []string {
	args := []string{"--output-format", "stream-json"}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", opts.SystemPrompt)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, claudeBlockedArgs)...)
	return args
}

func (b *claudeBackend) handleAssistant(msg claudeSDKMessage, ch chan<- externalMessage, output *strings.Builder) {
	var content claudeMessageContent
	if err := json.Unmarshal(msg.Message, &content); err != nil {
		return
	}

	for _, block := range content.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				output.WriteString(block.Text)
				trySend(ch, externalMessage{Type: externalMessageText, Content: block.Text})
			}
		case "thinking":
			if block.Text != "" {
				trySend(ch, externalMessage{Type: externalMessageThinking, Content: block.Text})
			}
		}
	}
}

func (b *claudeBackend) handleUser(msg claudeSDKMessage, ch chan<- externalMessage) {
	// User/Tool response mapping is not strictly required to be forwarded back to L1/L2
}

func (b *claudeBackend) handleControlRequest(msg claudeSDKMessage, stdin io.Writer) {
	if stdin == nil {
		return
	}
	var req claudeControlRequestPayload
	if err := json.Unmarshal(msg.Request, &req); err != nil {
		return
	}

	var inputMap map[string]any
	if req.Input != nil {
		_ = json.Unmarshal(req.Input, &inputMap)
	}
	if inputMap == nil {
		inputMap = map[string]any{}
	}

	response := map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": msg.RequestID,
			"response": map[string]any{
				"behavior":     "allow",
				"updatedInput": inputMap,
			},
		},
	}

	data, err := json.Marshal(response)
	if err != nil {
		return
	}
	data = append(data, '\n')
	_, _ = stdin.Write(data)
}

func writeClaudeInput(w io.Writer, prompt string) error {
	payload := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]any{
				{
					"type": "text",
					"text": prompt,
				},
			},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

func resolveSessionID(requestedResume, emitted string, failed bool) string {
	if failed && requestedResume != "" && emitted != requestedResume {
		return ""
	}
	if emitted != "" {
		return emitted
	}
	return requestedResume
}

func filterCustomArgs(args []string, blocked map[string]blockedArgMode) []string {
	var filtered []string
	skipNext := false
	for i, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if mode, isBlocked := blocked[arg]; isBlocked {
			if mode == blockedWithValue && i+1 < len(args) {
				skipNext = true
			}
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered
}

func trySend(ch chan<- externalMessage, msg externalMessage) {
	select {
	case ch <- msg:
	default:
	}
}

// ── Claude SDK JSON structures ──

type claudeSDKMessage struct {
	Type       string          `json:"type"`
	Message    json.RawMessage `json:"message,omitempty"`
	SessionID  string          `json:"session_id,omitempty"`
	ResultText string          `json:"result,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
	Log        *claudeLogEntry `json:"log,omitempty"`
	RequestID  string          `json:"request_id,omitempty"`
	Request    json.RawMessage `json:"request,omitempty"`
}

type claudeLogEntry struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

type claudeMessageContent struct {
	Content []claudeContentBlock `json:"content"`
}

type claudeContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type claudeControlRequestPayload struct {
	Input json.RawMessage `json:"input,omitempty"`
}
