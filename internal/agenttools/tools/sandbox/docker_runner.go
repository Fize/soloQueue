package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// DockerRunner manages a long-lived Docker container for executing commands.
type DockerRunner struct {
	mu             sync.Mutex
	cli            *client.Client
	imageName      string
	containerID    string
	started        bool
	mounts         []Mount
	pathMap        *PathMap
	log            *logger.Logger
	name           string
	policyHash     string
	networkEnabled bool
}

// NewDockerRunner initializes a workspace-scoped Docker backend using the
// current directory. New production callers should use NewDockerBackend.
func NewDockerRunner(log *logger.Logger) (*DockerRunner, error) {
	return NewDockerBackend(DockerOptions{}, log)
}

// NewDockerBackend initializes a Docker-backed SandboxBackend.
func NewDockerBackend(opts DockerOptions, log *logger.Logger) (*DockerRunner, error) {
	if err := ensureDockerHost(); err != nil {
		return nil, err
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("sandbox: create docker client: %w", err)
	}

	var mounts []Mount
	if opts.Workspace == "" && opts.PlanDir == "" && opts.CacheDir == "" && opts.ArtifactDir == "" {
		mounts = buildDefaultMounts()
	} else {
		mounts, err = buildMounts(opts)
		if err != nil {
			return nil, err
		}
	}
	pathMap := NewPathMap(mounts)
	policyBytes, _ := json.Marshal(struct {
		Schema         int
		Owner          string
		NetworkEnabled bool
		Mounts         []Mount
	}{Schema: 2, Owner: opts.Owner, NetworkEnabled: opts.NetworkEnabled, Mounts: mounts})
	sum := sha256.Sum256(policyBytes)
	policyHash := hex.EncodeToString(sum[:])
	name := "soloqueue-sbx-" + policyHash[:16]

	return &DockerRunner{
		cli:            cli,
		mounts:         mounts,
		pathMap:        pathMap,
		log:            log,
		name:           name,
		policyHash:     policyHash,
		networkEnabled: opts.NetworkEnabled,
	}, nil
}

// Name identifies the concrete SandboxBackend implementation.
func (d *DockerRunner) Name() string { return "docker" }

func (d *DockerRunner) Prepare(ctx context.Context) error { return d.Start(ctx) }

// Start ensures the Docker container is created and running.
func (d *DockerRunner) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.started {
		return nil
	}

	if err := d.ensureImage(ctx); err != nil {
		return err
	}
	imageInspect, _, err := d.cli.ImageInspectWithRaw(ctx, d.imageName)
	if err != nil {
		return fmt.Errorf("sandbox: inspect image identity: %w", err)
	}
	instanceSum := sha256.Sum256([]byte(d.policyHash + "|" + imageInspect.ID + "|docker-v2"))
	instanceHash := hex.EncodeToString(instanceSum[:])
	d.name = "soloqueue-sbx-" + instanceHash[:16]

	filter := filters.NewArgs()
	filter.Add("name", d.name)
	containers, err := d.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: filter})
	if err == nil {
		for _, c := range containers {
			for _, n := range c.Names {
				if n == "/"+d.name && c.Labels["soloqueue.policy_hash"] == d.policyHash &&
					c.Labels["soloqueue.schema"] == "2" &&
					c.Labels["soloqueue.image_id"] == imageInspect.ID {
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

	var hostBinds []mount.Mount
	for _, m := range d.mounts {
		hostBinds = append(hostBinds, mount.Mount{
			Type:     mount.TypeBind,
			Source:   m.HostPath,
			Target:   m.ContainerPath,
			ReadOnly: m.ReadOnly,
		})
	}

	workDir := containerWorkingDirectory(d.mounts)

	pidsLimit := int64(512)
	networkMode := sandboxNetworkMode(d.networkEnabled)
	hostCfg := &container.HostConfig{
		Mounts:      hostBinds,
		NetworkMode: networkMode,
		CapDrop:     []string{"ALL"},
		SecurityOpt: []string{"no-new-privileges:true"},
		Resources: container.Resources{
			Memory:    2 << 30,
			NanoCPUs:  2_000_000_000,
			PidsLimit: &pidsLimit,
		},
		Tmpfs: map[string]string{
			"/tmp": "rw,nosuid,nodev,size=256m",
		},
	}

	resp, err := d.cli.ContainerCreate(ctx,
		&container.Config{
			Image:      d.imageName,
			Cmd:        []string{"/bin/sh", "-c", "mkdir -p \"$HOME\" && exec tail -f /dev/null"},
			WorkingDir: workDir,
			User:       sandboxContainerUser(),
			Env: []string{
				"HOME=/tmp/soloqueue-home",
				"XDG_CACHE_HOME=/tmp/soloqueue-home/.cache",
			},
			Labels: map[string]string{
				"soloqueue.owner":       "soloqueue",
				"soloqueue.policy_hash": d.policyHash,
				"soloqueue.schema":      "2",
				"soloqueue.backend":     "docker",
				"soloqueue.image_id":    imageInspect.ID,
			},
		},
		hostCfg,
		nil,
		nil,
		d.name,
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

func containerWorkingDirectory(mounts []Mount) string {
	for _, mounted := range mounts {
		if mounted.Purpose == "workspace" {
			return mounted.ContainerPath
		}
	}
	return "/root"
}

func sandboxNetworkMode(enabled bool) container.NetworkMode {
	if enabled {
		return "bridge"
	}
	return "none"
}

// RunCommand executes a command inside the running Docker container.
func (d *DockerRunner) RunCommand(ctx context.Context, cmd string, opts RunCommandOptions) (RunCommandResult, error) {
	return d.runArgv(ctx, []string{"/bin/sh", "-c", cmd}, nil, opts)
}

func (d *DockerRunner) runArgv(ctx context.Context, argv []string, env []string, opts RunCommandOptions) (RunCommandResult, error) {
	if err := d.Start(ctx); err != nil {
		return RunCommandResult{}, fmt.Errorf("sandbox: ensure container started: %w", err)
	}

	d.mu.Lock()
	cid := d.containerID
	d.mu.Unlock()

	containerWorkDir := containerWorkingDirectory(d.mounts)
	if opts.WorkingDirectory != "" {
		containerWorkDir = d.pathMap.ToContainerPath(opts.WorkingDirectory)
	}

	token, err := randomExecToken()
	if err != nil {
		return RunCommandResult{}, err
	}
	env = append(env, "SOLOQUEUE_EXEC_TOKEN="+token)
	execCfg := container.ExecOptions{
		Cmd:          argv,
		Env:          env,
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
		if err := d.waitExecRunning(ctx, execResp.ID); err != nil {
			return RunCommandResult{}, err
		}
		go func() {
			_, _ = attachResp.Conn.Write([]byte(opts.Stdin))
			_ = attachResp.CloseWrite()
		}()
	}

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	readDone := make(chan error, 1)
	go func() {
		_, err := copyDockerOutput(&attachResp, &stdoutBuf, &stderrBuf)
		readDone <- err
	}()

	select {
	case <-readDone:
	case <-ctx.Done():
		killCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = d.killExec(killCtx, execResp.ID, token)
		cancel()
		attachResp.Close()
		return RunCommandResult{}, ctx.Err()
	}

	stdout := stdoutBuf.Bytes()
	stderr := stderrBuf.Bytes()

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

// StartProcess starts a long-lived process without shell interpolation.
func (d *DockerRunner) StartProcess(ctx context.Context, spec ProcessSpec) (Process, error) {
	if err := d.Start(ctx); err != nil {
		return nil, fmt.Errorf("sandbox: ensure container started: %w", err)
	}
	if strings.TrimSpace(spec.Command) == "" {
		return nil, fmt.Errorf("sandbox: empty process command")
	}

	d.mu.Lock()
	cid := d.containerID
	d.mu.Unlock()

	workDir := containerWorkingDirectory(d.mounts)
	if spec.WorkingDirectory != "" {
		workDir = d.pathMap.ToContainerPath(spec.WorkingDirectory)
	}
	env := make([]string, 0, len(spec.Env))
	for k, v := range spec.Env {
		if k == "SOLOQUEUE_EXEC_TOKEN" {
			continue
		}
		env = append(env, k+"="+v)
	}
	token, err := randomExecToken()
	if err != nil {
		return nil, err
	}
	env = append(env, "SOLOQUEUE_EXEC_TOKEN="+token)
	sort.Strings(env)
	command := spec.Command
	if filepath.IsAbs(command) {
		command = d.pathMap.ToContainerPath(command)
	}
	args := make([]string, len(spec.Args))
	for i, arg := range spec.Args {
		args[i] = arg
		if filepath.IsAbs(arg) {
			args[i] = d.pathMap.ToContainerPath(arg)
		}
	}

	execResp, err := d.cli.ContainerExecCreate(ctx, cid, container.ExecOptions{
		Cmd:          append([]string{command}, args...),
		Env:          env,
		WorkingDir:   workDir,
		AttachStdout: true,
		AttachStderr: true,
		AttachStdin:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("sandbox: process create: %w", err)
	}
	attach, err := d.cli.ContainerExecAttach(ctx, execResp.ID, container.ExecStartOptions{})
	if err != nil {
		return nil, fmt.Errorf("sandbox: process attach: %w", err)
	}
	if err := d.waitExecRunning(ctx, execResp.ID); err != nil {
		attach.Close()
		return nil, err
	}

	proc := newDockerProcess(d, execResp.ID, token, attach)
	go func() {
		select {
		case <-ctx.Done():
			_ = proc.Kill()
		case <-proc.done:
		}
	}()
	return proc, nil
}

func randomExecToken() (string, error) {
	name, err := randomTempName()
	if err != nil {
		return "", fmt.Errorf("sandbox: create execution identity: %w", err)
	}
	return strings.TrimPrefix(name, ".soloqueue-tmp-"), nil
}

func (d *DockerRunner) waitExecRunning(ctx context.Context, execID string) error {
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		inspect, err := d.cli.ContainerExecInspect(waitCtx, execID)
		if err != nil {
			return fmt.Errorf("sandbox: inspect process startup: %w", err)
		}
		if inspect.Running {
			return nil
		}
		if inspect.Pid > 0 {
			return fmt.Errorf("sandbox: process exited before its stdio stream became ready")
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("sandbox: wait for process stdio: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}

// Stop stops the container.
func (d *DockerRunner) Stop(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}

	cid := d.containerID
	if cid == "" {
		return nil
	}

	_ = d.cli.ContainerKill(ctx, cid, "SIGKILL")
	if err := d.cli.ContainerRemove(ctx, cid, container.RemoveOptions{Force: true}); err != nil &&
		!strings.Contains(strings.ToLower(err.Error()), "no such container") {
		return err
	}
	d.containerID = ""
	d.started = false
	return nil
}

// Close is an alias used by owners that manage backend lifecycle.
func (d *DockerRunner) Close() error {
	if err := d.Stop(context.Background()); err != nil {
		return err
	}
	return d.cli.Close()
}

var _ Backend = (*DockerRunner)(nil)
