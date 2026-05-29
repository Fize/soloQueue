package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

var geminiBlockedArgs = map[string]blockedArgMode{
	"-p":     blockedWithValue,  // non-interactive prompt
	"--yolo": blockedFlagOnly,   // auto-approve tool use
	"-o":     blockedWithValue,  // stream-json output format
	"-r":     blockedWithValue,  // resume session ID
}

type geminiBackend struct {
	execPath string
	logger   *logger.Logger
}

func (b *geminiBackend) Execute(ctx context.Context, prompt string, opts externalExecOptions) (*externalSession, error) {
	execPath := b.execPath
	if execPath == "" {
		execPath = "gemini"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("gemini executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := b.buildGeminiArgs(prompt, opts)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	if b.logger != nil {
		b.logger.Info("agent command", "exec", execPath, "args", args)
	}
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildGeminiEnv(opts.CustomEnv)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("gemini stdout pipe: %w", err)
	}

	var stderrLogger *logWriter
	if b.logger != nil {
		stderrLogger = newLogWriter(b.logger, "[gemini:stderr] ")
	}
	stderrBuf := newStderrTail(stderrLogger, agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start gemini: %w", err)
	}

	if b.logger != nil {
		b.logger.Info("gemini started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)
	}

	msgCh := make(chan externalMessage, 256)
	resCh := make(chan externalResult, 1)

	// Close stdout when the context is cancelled so scanner.Scan() unblocks.
	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		var output strings.Builder
		var sessionID string
		finalStatus := "completed"
		var finalError string

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var evt geminiStreamEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				continue
			}

			switch evt.Type {
			case "init":
				sessionID = evt.SessionID
				trySend(msgCh, externalMessage{Type: externalMessageStatus, Content: "running", SessionID: sessionID})

			case "message":
				if evt.Role == "assistant" && evt.Content != "" {
					output.WriteString(evt.Content)
					trySend(msgCh, externalMessage{Type: externalMessageText, Content: evt.Content})
				}

			case "error":
				trySend(msgCh, externalMessage{Type: externalMessageError, Content: evt.Message})

			case "result":
				if evt.Status == "error" && evt.Error != nil {
					finalStatus = "failed"
					finalError = evt.Error.Message
				}
			}
		}

		waitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("gemini timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if waitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("gemini exited with error: %v", waitErr)
		}

		if finalError != "" {
			finalError = withAgentStderr(finalError, "gemini", stderrBuf.Tail())
		}

		if b.logger != nil {
			b.logger.Info("gemini finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())
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

func (b *geminiBackend) buildGeminiArgs(prompt string, opts externalExecOptions) []string {
	args := []string{
		"-p", prompt,
		"--yolo",
		"-o", "stream-json",
	}
	if opts.Model != "" {
		args = append(args, "-m", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "-r", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, geminiBlockedArgs)...)
	return args
}

func buildGeminiEnv(customEnv map[string]string) []string {
	const trustKey = "GEMINI_CLI_TRUST_WORKSPACE"
	if _, ok := customEnv[trustKey]; ok {
		return buildEnv(customEnv)
	}
	merged := make(map[string]string, len(customEnv)+1)
	for k, v := range customEnv {
		merged[k] = v
	}
	merged[trustKey] = "true"
	return buildEnv(merged)
}

// ── Gemini stream-json event types ──

type geminiStreamEvent struct {
	Type      string             `json:"type"`
	SessionID string             `json:"session_id,omitempty"`
	Role      string             `json:"role,omitempty"`
	Content   string             `json:"content,omitempty"`
	Message   string             `json:"message,omitempty"`
	Status    string             `json:"status,omitempty"`
	Error     *geminiStreamError `json:"error,omitempty"`
}

type geminiStreamError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
