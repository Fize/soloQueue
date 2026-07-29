package sandbox

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
)

type dockerProcess struct {
	backend *DockerRunner
	execID  string
	token   string
	attach  types.HijackedResponse

	stdinW  *dockerStdin
	stdoutR *io.PipeReader
	stderrR *io.PipeReader
	done    chan struct{}

	mu       sync.Mutex
	waitErr  error
	once     sync.Once
	doneOnce sync.Once
}

type dockerStdin struct {
	mu     sync.Mutex
	attach *types.HijackedResponse
	closed bool
}

func (w *dockerStdin) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, io.ErrClosedPipe
	}
	written := 0
	for written < len(p) {
		n, err := w.attach.Conn.Write(p[written:])
		written += n
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrNoProgress
		}
	}
	return written, nil
}

func (w *dockerStdin) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	return w.attach.CloseWrite()
}

func newDockerProcess(backend *DockerRunner, execID, token string, attach types.HijackedResponse) *dockerProcess {
	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()
	p := &dockerProcess{
		backend: backend,
		execID:  execID,
		token:   token,
		attach:  attach,
		stdinW:  &dockerStdin{attach: &attach},
		stdoutR: stdoutR,
		stderrR: stderrR,
		done:    make(chan struct{}),
	}
	go func() {
		_, err := copyDockerOutput(&attach, stdoutW, stderrW)
		_ = stdoutW.CloseWithError(err)
		_ = stderrW.CloseWithError(err)
		attach.Close()

		inspect, inspectErr := backend.cli.ContainerExecInspect(context.Background(), execID)
		if err == nil && inspectErr != nil {
			err = inspectErr
		}
		if err == nil && inspect.ExitCode != 0 {
			err = &ProcessExitError{Code: inspect.ExitCode}
		}
		p.complete(err)
	}()
	return p
}

func (p *dockerProcess) complete(err error) {
	p.doneOnce.Do(func() {
		p.mu.Lock()
		p.waitErr = err
		p.mu.Unlock()
		close(p.done)
	})
}

func (p *dockerProcess) Stdin() io.WriteCloser { return p.stdinW }
func (p *dockerProcess) Stdout() io.Reader     { return p.stdoutR }
func (p *dockerProcess) Stderr() io.Reader     { return p.stderrR }

func (p *dockerProcess) Wait() error {
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waitErr
}

func (p *dockerProcess) Kill() error {
	var killErr error
	p.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		killErr = p.backend.killExec(ctx, p.execID, p.token)
		_ = p.stdinW.Close()
		p.attach.Close()
		_ = p.stdoutR.Close()
		_ = p.stderrR.Close()
		p.complete(killErr)
	})
	return killErr
}

func (d *DockerRunner) killExec(ctx context.Context, execID, token string) error {
	inspect, err := d.cli.ContainerExecInspect(ctx, execID)
	if err != nil {
		return err
	}
	if !inspect.Running {
		return nil
	}
	d.mu.Lock()
	cid := d.containerID
	d.mu.Unlock()
	resp, err := d.cli.ContainerExecCreate(ctx, cid, container.ExecOptions{
		Cmd: []string{
			"/bin/sh", "-c", `
token=$1
for process in /proc/[0-9]*; do
	[ -r "$process/environ" ] || continue
	if /usr/bin/tr '\000' '\n' < "$process/environ" 2>/dev/null |
		/bin/grep -Fxq "SOLOQUEUE_EXEC_TOKEN=$token"; then
		/bin/kill -KILL "${process##*/}" 2>/dev/null || true
	fi
done
`, "soloqueue-kill", token,
		},
	})
	if err != nil {
		return err
	}
	if err := d.cli.ContainerExecStart(ctx, resp.ID, container.ExecStartOptions{Detach: true}); err != nil {
		return err
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		target, targetErr := d.cli.ContainerExecInspect(ctx, execID)
		if targetErr != nil {
			return targetErr
		}
		if !target.Running {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

var _ Process = (*dockerProcess)(nil)
