package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"runtime"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// DockerRunner manages a long-lived Docker container for executing commands.
type DockerRunner struct {
	mu          sync.Mutex
	cli         *client.Client
	imageName   string
	containerID string
	started     bool
	mounts      []Mount
	pathMap     *PathMap
	log         *logger.Logger
}

// NewDockerRunner initializes a DockerRunner instance with dynamic image/Dockerfile resolution.
func NewDockerRunner(log *logger.Logger) (*DockerRunner, error) {
	if err := ensureDockerHost(); err != nil {
		return nil, err
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("sandbox: create docker client: %w", err)
	}

	mounts := buildDefaultMounts()
	pathMap := NewPathMap(mounts)

	return &DockerRunner{
		cli:     cli,
		mounts:  mounts,
		pathMap: pathMap,
		log:     log,
	}, nil
}

// Start ensures the Docker container is created and running.
func (d *DockerRunner) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.started {
		return nil
	}

	filter := filters.NewArgs()
	filter.Add("name", containerName)
	containers, err := d.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: filter})
	if err == nil {
		for _, c := range containers {
			for _, n := range c.Names {
				if n == "/"+containerName {
					d.containerID = c.ID
					if c.State == "running" {
						d.started = true
						return nil
					}
					if err := d.cli.ContainerStart(ctx, c.ID, container.StartOptions{}); err == nil {
						d.started = true
						return nil
					}
					_ = d.cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true})
					d.containerID = ""
					break
				}
			}
		}
	}

	if err := d.ensureImage(ctx); err != nil {
		return err
	}

	var hostBinds []mount.Mount
	for _, m := range d.mounts {
		hostBinds = append(hostBinds, mount.Mount{
			Type:   mount.TypeBind,
			Source: m.HostPath,
			Target: m.ContainerPath,
		})
	}

	workDir := "/root"
	if len(d.mounts) > 0 {
		workDir = d.mounts[len(d.mounts)-1].ContainerPath
	}

	hostCfg := &container.HostConfig{
		Mounts: hostBinds,
	}

	if runtime.GOOS == "linux" {
		hostCfg.NetworkMode = "host"
	} else {
		hostCfg.ExtraHosts = []string{"host.docker.internal:host-gateway"}
	}

	resp, err := d.cli.ContainerCreate(ctx,
		&container.Config{
			Image:      d.imageName,
			Cmd:        []string{"/bin/sh", "-c", "tail -f /dev/null"},
			WorkingDir: workDir,
		},
		hostCfg,
		nil,
		nil,
		containerName,
	)
	if err != nil {
		return fmt.Errorf("sandbox: create container: %w", err)
	}

	if err := d.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = d.cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return fmt.Errorf("sandbox: start container: %w", err)
	}

	d.containerID = resp.ID
	d.started = true
	return nil
}

// RunCommand executes a command inside the running Docker container.
func (d *DockerRunner) RunCommand(ctx context.Context, cmd string, opts RunCommandOptions) (RunCommandResult, error) {
	if err := d.Start(ctx); err != nil {
		return RunCommandResult{}, fmt.Errorf("sandbox: ensure container started: %w", err)
	}

	d.mu.Lock()
	cid := d.containerID
	d.mu.Unlock()

	containerWorkDir := "/root"
	if opts.WorkingDirectory != "" {
		containerWorkDir = d.pathMap.ToContainerPath(opts.WorkingDirectory)
	}

	execCfg := container.ExecOptions{
		Cmd:          []string{"/bin/sh", "-c", cmd},
		WorkingDir:   containerWorkDir,
		AttachStdout: true,
		AttachStderr: true,
		AttachStdin:  opts.Stdin != "",
	}

	execResp, err := d.cli.ContainerExecCreate(ctx, cid, execCfg)
	if err != nil {
		return RunCommandResult{}, fmt.Errorf("sandbox: exec create: %w", err)
	}

	attachResp, err := d.cli.ContainerExecAttach(ctx, execResp.ID, container.ExecStartOptions{})
	if err != nil {
		return RunCommandResult{}, fmt.Errorf("sandbox: exec attach: %w", err)
	}
	defer attachResp.Close()

	if opts.Stdin != "" {
		go func() {
			_, _ = attachResp.Conn.Write([]byte(opts.Stdin))
			_ = attachResp.CloseWrite()
		}()
	}

	var rawBuf bytes.Buffer
	readDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(&rawBuf, attachResp.Reader)
		readDone <- err
	}()

	select {
	case <-readDone:
	case <-ctx.Done():
		return RunCommandResult{}, ctx.Err()
	}

	stdout, stderr := demuxDockerStream(rawBuf.Bytes())

	inspectResp, err := d.cli.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return RunCommandResult{}, fmt.Errorf("sandbox: exec inspect: %w", err)
	}

	maxOut := opts.MaxOutput
	if maxOut <= 0 {
		maxOut = 256 << 10
	}

	var truncated bool
	if int64(len(stdout)) > maxOut {
		stdout = stdout[:maxOut]
		truncated = true
	}
	if int64(len(stderr)) > maxOut {
		stderr = stderr[:maxOut]
		truncated = true
	}

	return RunCommandResult{
		ExitCode:  inspectResp.ExitCode,
		Stdout:    stdout,
		Stderr:    stderr,
		Truncated: truncated,
	}, nil
}

// Stop stops the container.
func (d *DockerRunner) Stop(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	cid := d.containerID
	if cid == "" {
		return nil
	}

	_ = d.cli.ContainerKill(ctx, cid, "SIGKILL")
	d.started = false
	return nil
}
