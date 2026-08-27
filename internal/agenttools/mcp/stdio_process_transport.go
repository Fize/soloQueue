package mcp

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
)

// stdioProcessTransport keeps the MCP wire protocol unchanged while launching the
// stdio process through the shared host Executor.
type stdioProcessTransport struct {
	*transport.Stdio

	executor *tools.Executor
	spec     tools.ProcessSpec

	mu      sync.Mutex
	process tools.Process
}

func newStdioProcessTransport(executor *tools.Executor, spec tools.ProcessSpec) *stdioProcessTransport {
	return &stdioProcessTransport{executor: executor, spec: spec}
}

func (t *stdioProcessTransport) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.process != nil {
		return nil
	}

	process, err := t.executor.StartProcess(ctx, t.spec)
	if err != nil {
		return err
	}
	t.process = process
	go func() {
		_ = process.Wait()
	}()
	go func() {
		_, _ = io.Copy(io.Discard, process.Stderr())
	}()

	t.Stdio = transport.NewIO(
		process.Stdout(),
		process.Stdin(),
		io.NopCloser(&emptyReader{}),
	)
	if err := t.Stdio.Start(ctx); err != nil {
		_ = process.Kill()
		return err
	}
	return nil
}

func (t *stdioProcessTransport) Close() error {
	t.mu.Lock()
	process := t.process
	stdio := t.Stdio
	t.process = nil
	t.mu.Unlock()

	var errs []error
	if stdio != nil {
		if err := stdio.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if process == nil {
		return errors.Join(errs...)
	}

	if err := process.Kill(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

type emptyReader struct{}

func (*emptyReader) Read([]byte) (int, error) { return 0, io.EOF }
