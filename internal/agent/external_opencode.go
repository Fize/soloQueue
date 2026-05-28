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

var opencodeBlockedArgs = map[string]blockedArgMode{
	"--format": blockedWithValue, // json output format for daemon communication
}

type opencodeBackend struct {
	execPath string
	logger   *logger.Logger
}

func (b *opencodeBackend) Execute(ctx context.Context, prompt string, opts externalExecOptions) (*externalSession, error) {
	execPath := b.execPath
	if execPath == "" {
		execPath = "opencode"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("opencode executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := []string{"run", "--format", "json"}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--prompt", opts.SystemPrompt)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--session", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, opencodeBlockedArgs)...)
	args = append(args, prompt)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	if b.logger != nil {
		b.logger.Info("agent command", "exec", execPath, "args", args)
	}
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}

	env := buildEnv(opts.CustomEnv)
	env = append(env, `OPENCODE_PERMISSION={"*":"allow"}`) // Auto-approve tools
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("opencode stdout pipe: %w", err)
	}

	var stderrLogger *logWriter
	if b.logger != nil {
		stderrLogger = newLogWriter(b.logger, "[opencode:stderr] ")
	}
	cmd.Stderr = stderrLogger

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start opencode: %w", err)
	}

	if b.logger != nil {
		b.logger.Info("opencode started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)
	}

	msgCh := make(chan externalMessage, 256)
	resCh := make(chan externalResult, 1)

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

			var event opencodeEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				continue
			}

			if event.SessionID != "" {
				sessionID = event.SessionID
			}

			switch event.Type {
			case "text":
				text := event.Part.Text
				if text != "" {
					output.WriteString(text)
					trySend(msgCh, externalMessage{Type: externalMessageText, Content: text})
				}
			case "error":
				errMsg := ""
				if event.Error != nil {
					errMsg = event.Error.Message()
				}
				if errMsg == "" {
					errMsg = "unknown opencode error"
				}
				trySend(msgCh, externalMessage{Type: externalMessageError, Content: errMsg})
				finalStatus = "failed"
				finalError = errMsg
			case "step_start":
				trySend(msgCh, externalMessage{Type: externalMessageStatus, Content: "running"})
			}
		}

		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("opencode timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if exitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("opencode exited with error: %v", exitErr)
		}

		if b.logger != nil {
			b.logger.Info("opencode finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())
		}

		resCh <- externalResult{
			Status:    finalStatus,
			Output:    output.String(),
			Error:     finalError,
			SessionID: sessionID,
		}
	}()

	return &externalSession{Messages: msgCh, Result: resCh}, nil
}

// ── OpenCode JSON types ──

type opencodeEvent struct {
	Type      string            `json:"type"`
	SessionID string            `json:"sessionID,omitempty"`
	Part      opencodeEventPart `json:"part"`
	Error     *opencodeError    `json:"error,omitempty"`
}

type opencodeEventPart struct {
	Text string `json:"text,omitempty"`
}

type opencodeError struct {
	Name string           `json:"name,omitempty"`
	Data *opencodeErrData `json:"data,omitempty"`
}

func (e *opencodeError) Message() string {
	if e.Data != nil && e.Data.Message != "" {
		return e.Data.Message
	}
	return e.Name
}

type opencodeErrData struct {
	Message string `json:"message,omitempty"`
}
