// Package sandbox 提供容器化沙盒的抽象与 Docker 实现。
//
// 设计原则（已更新）：
//   - 全局唯一共享沙盒：主程序启动时创建或重用已停止的容器，退出时停止（不删除）。
//   - 容器允许跨多个程序运行周期重用：通过 Stop/Start 模式实现容器生命周期管理。
//   - 不做硬性资源限制，容器默认使用宿主机全部可用算力。
//   - 容器启动时挂载所有 workspace 目录：家目录映射到 /root/.soloqueue，
//     其余 workspace 容器内路径与宿主机路径完全一致（1:1 映射）。
//   - Exec 支持高并发调用（每次 exec 独立，互不阻塞）。
package sandbox

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── 接口定义 ────────────────────────────────────────────────────────────────

// Sandbox 是容器化沙盒的抽象接口。
type Sandbox interface {
	// Start 执行清理残留、镜像 Pull、容器拉起，或重用已停止的容器。
	Start(ctx context.Context) error

	// Exec 在沙盒中执行命令，需支持高并发调用。
	Exec(ctx context.Context, cmd string) (stdout []byte, stderr []byte, err error)

	// Destroy 停止该全局容器。容器保持已停止状态，稍后可通过 Start 重新启动。
	Destroy(ctx context.Context) error
}

// ─── Docker 实现 ─────────────────────────────────────────────────────────────

const (
	containerName = "soloqueue-sandbox"
	imageName     = "debian:bookworm"
)

// DockerSandbox 基于 Docker SDK 的沙盒实现。
type DockerSandbox struct {
	cli         *client.Client
	containerID string
	mu          sync.Mutex // protects containerID + Start/Destroy transitions
	started     bool
	mounts      []Mount        // 需要挂载到容器内的目录列表
	env         []string       // 容器环境变量（KEY=VALUE 格式）
	workDir     string         // 容器内默认工作目录（~/.soloqueue 的容器内路径）
	pathMap     *PathMap       // 宿主机 ↔ 容器路径映射
	log         *logger.Logger // 可选的结构化日志
}

// resolveSandboxEnv 解析 sandbox 环境变量列表。
// "KEY" → 从宿主机 os.Getenv 读取
// "KEY=VALUE" → 原样使用
func resolveSandboxEnv(env []string) []string {
	resolved := make([]string, 0, len(env))
	for _, e := range env {
		if e == "" {
			continue
		}
		if strings.Contains(e, "=") {
			resolved = append(resolved, e)
		} else {
			resolved = append(resolved, e+"="+os.Getenv(e))
		}
	}
	return resolved
}

// SetLogger 设置 logger，nil 表示不记录日志。
func (d *DockerSandbox) SetLogger(l *logger.Logger) {
	d.log = l
}

// Mount 描述一个宿主机到容器的目录挂载。
type Mount struct {
	// HostPath 宿主机绝对路径。
	HostPath string
	// ContainerPath 容器内绝对路径。为空时默认 /root/.soloqueue（第一个 mount）或与 HostPath 一致（其余 mount）。
	ContainerPath string
}

// PathMap 维护宿主机路径与容器路径之间的双向映射。
type PathMap struct {
	hostToContainer map[string]string // 前缀映射：宿主机前缀 → 容器前缀
	containerToHost map[string]string // 前缀映射：容器前缀 → 宿主机前缀
}

// deduplicateByContainerPath removes mounts with duplicate ContainerPath,
// keeping the first occurrence. This prevents Docker "Duplicate mount point"
// errors when different HostPaths map to the same container path.
func deduplicateByContainerPath(mounts []Mount) []Mount {
	seen := make(map[string]bool)
	result := make([]Mount, 0, len(mounts))
	for _, m := range mounts {
		if seen[m.ContainerPath] {
			continue
		}
		seen[m.ContainerPath] = true
		result = append(result, m)
	}
	return result
}

// NewPathMap 从挂载列表构建路径映射。
func NewPathMap(mounts []Mount) *PathMap {
	pm := &PathMap{
		hostToContainer: make(map[string]string),
		containerToHost: make(map[string]string),
	}
	for _, m := range mounts {
		pm.hostToContainer[m.HostPath] = m.ContainerPath
		pm.containerToHost[m.ContainerPath] = m.HostPath
	}
	return pm
}

// ToContainerPath 将宿主机绝对路径转换为容器内路径。
// 找到最长匹配的挂载前缀进行替换。
func (pm *PathMap) ToContainerPath(hostPath string) string {
	best := ""
	for hp := range pm.hostToContainer {
		if strings.HasPrefix(hostPath, hp) && len(hp) > len(best) {
			best = hp
		}
	}
	if best == "" {
		return hostPath // 不在挂载范围内，原样返回
	}
	return pm.hostToContainer[best] + hostPath[len(best):]
}

// ToHostPath 将容器内路径转换为宿主机绝对路径。
// 找到最长匹配的挂载前缀进行替换。
func (pm *PathMap) ToHostPath(containerPath string) string {
	best := ""
	for cp := range pm.containerToHost {
		if strings.HasPrefix(containerPath, cp) && len(cp) > len(best) {
			best = cp
		}
	}
	if best == "" {
		return containerPath // 不在挂载范围内，原样返回
	}
	return pm.containerToHost[best] + containerPath[len(best):]
}

// NewDockerSandbox 创建一个新的 Docker 沙盒实例（未启动）。
// mounts 指定需要挂载到容器内的目录列表。
// env 指定注入容器的环境变量（KEY=VALUE 格式）。
// 第一个 mount 视为主工作目录，映射到 /root/.soloqueue；
// 其余 mount 容器内路径与宿主机路径完全一致（1:1 映射）。
// 自动探测 Docker / Rancher Desktop / OrbStack 的 socket 路径。
func NewDockerSandbox(mounts []Mount, env []string) (*DockerSandbox, error) {
	if err := ensureDockerHost(); err != nil {
		return nil, err
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("sandbox: create docker client: %w", err)
	}

	// 为没有指定 ContainerPath 的 mount 分配容器内路径
	// 家目录（第一个 mount）映射到 /root/.soloqueue；
	// 其余 mount 容器内路径与宿主机路径完全一致。
	for i := range mounts {
		if mounts[i].ContainerPath != "" {
			continue
		}
		if i == 0 {
			mounts[i].ContainerPath = "/root/.soloqueue"
		} else {
			mounts[i].ContainerPath = mounts[i].HostPath
		}
	}

	// 按 ContainerPath 去重（多个 HostPath 可能映射到同一容器路径）
	mounts = deduplicateByContainerPath(mounts)

	workDir := "/root/.soloqueue"
	if len(mounts) > 0 {
		workDir = mounts[0].ContainerPath
	}

	// 解析 env 列表：把 bare name 解析为 KEY=VALUE
	resolved := resolveSandboxEnv(env)

	return &DockerSandbox{
		cli:     cli,
		mounts:  mounts,
		env:     resolved,
		workDir: workDir,
		pathMap: NewPathMap(mounts),
	}, nil
}

// Start 启动沙盒容器。
// 启动优先级：
//   1. 如果已启动，直接返回
//   2. 检查是否存在已停止的容器，存在则直接启动
//   3. 否则创建新容器后启动
//
// Start 启动优先级（快速路径优先）：
//   1. 已启动 → 直接返回
//   2. 容器已存在且正在运行 → 直接复用
//   3. 容器已存在但已停止 → 直接启动
//   4. 容器不存在 → 确保镜像存在，然后创建并启动
func (d *DockerSandbox) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.started {
		return nil
	}

	// 尝试查找现有容器（无论 running 还是 stopped）
	findStart := time.Now()
	existingID, state, err := d.findContainer(ctx, containerName)
	if d.log != nil {
		d.log.Info(logger.CatApp, "sandbox: findContainer took",
			"duration", time.Since(findStart).String(),
			"found", existingID != "",
			"state", state,
			"err", fmt.Sprintf("%v", err))
	}

	if err == nil && existingID != "" {
		switch state {
		case "running":
			// 情况 2：容器正在运行。
			// 可能上次退出时 Stop() 尚未完成，不能贸然复用——
			// 旧进程的 Stop() 稍后可能把容器停掉。
			// 正确做法：先停掉（应处于停止状态），再启动，确保状态干净。
			if d.log != nil {
				d.log.Info(logger.CatApp, "sandbox: container still running, stopping then restarting",
					"container_id", existingID[:12])
			}
				_ = d.cli.ContainerKill(ctx, existingID, "SIGKILL")
			state = "exited" // fallthrough to start path below
			fallthrough
		case "exited", "stopped":
			// 情况 3：容器已停止，直接启动
			startStart := time.Now()
			if err := d.cli.ContainerStart(ctx, existingID, container.StartOptions{}); err != nil {
				if d.log != nil {
					d.log.LogError(ctx, logger.CatApp, "sandbox: start existing container failed",
						err, "container_id", existingID[:12], "duration", time.Since(startStart).String())
				}
				return fmt.Errorf("sandbox: start existing container: %w", err)
			}
			if d.log != nil {
				d.log.Info(logger.CatApp, "sandbox: ContainerStart took",
					"duration", time.Since(startStart).String(), "container_id", existingID[:12])
			}
			d.containerID = existingID
			d.started = true
			if d.log != nil {
				d.log.Info(logger.CatApp, "sandbox: started existing stopped container",
					"container_id", existingID[:12],
					"total_duration", time.Since(findStart).String())
			}
			return nil
		default:
			// 其他状态（如 created, paused 等），先移除再重建
			if d.log != nil {
				d.log.Info(logger.CatApp, "sandbox: container in unexpected state, removing",
					"container_id", existingID[:12], "state", state)
			}
			_ = d.cli.ContainerRemove(ctx, existingID, container.RemoveOptions{Force: true})
		}
	}

	// 情况 4：未找到容器或容器状态异常，创建新容器
	// 4a. 确保镜像存在（本地没有时才 pull）
	if err := d.ensureImage(ctx); err != nil {
		return err
	}

	// 4b. 清理同名残留容器（如果有）
	if err := d.removeExisting(ctx); err != nil {
		if d.log != nil {
			d.log.LogError(ctx, logger.CatApp, "sandbox: remove existing container failed", err)
		}
		return fmt.Errorf("sandbox: remove existing container: %w", err)
	}

	// 4c. 创建容器
	createStart := time.Now()
	hostConfig := &container.HostConfig{}
	switch runtime.GOOS {
	case "linux":
		hostConfig.NetworkMode = "host"
	default:
		// macOS / Windows: Docker Desktop 无法使用 host 网络模式，
		// 通过 ExtraHosts 让沙盒内 localhost 指向宿主机。
		hostConfig.ExtraHosts = []string{"localhost:host-gateway"}
	}

	// 挂载目录（hostPath:containerPath）
	for _, m := range d.mounts {
		hostConfig.Binds = append(hostConfig.Binds, m.HostPath+":"+m.ContainerPath)
	}

	createResp, err := d.cli.ContainerCreate(ctx,
		&container.Config{
			Image:      imageName,
			Cmd:        []string{"/bin/sh", "-c", "tail -f /dev/null"},
			WorkingDir: d.workDir,
			Env:        d.env,
		},
		hostConfig,
		nil,
		nil,
		containerName,
	)
	if err != nil {
		if d.log != nil {
			mountsLog := make([]string, len(d.mounts))
			for i, m := range d.mounts {
				mountsLog[i] = m.HostPath + ":" + m.ContainerPath
			}
			d.log.LogError(ctx, logger.CatApp, "sandbox: create container failed",
				err, "image", imageName, "mounts", mountsLog)
		}
		return fmt.Errorf("sandbox: create container: %w", err)
	}
	if d.log != nil {
		d.log.Info(logger.CatApp, "sandbox: ContainerCreate took",
			"duration", time.Since(createStart).String())
	}

	// 4d. 启动容器
	startNewStart := time.Now()
	if err := d.cli.ContainerStart(ctx, createResp.ID, container.StartOptions{}); err != nil {
		_ = d.cli.ContainerRemove(ctx, createResp.ID, container.RemoveOptions{Force: true})
		if d.log != nil {
			d.log.LogError(ctx, logger.CatApp, "sandbox: start container failed",
				err, "container_id", createResp.ID[:12])
		}
		return fmt.Errorf("sandbox: start container: %w", err)
	}
	if d.log != nil {
		d.log.Info(logger.CatApp, "sandbox: ContainerStart (new) took",
			"duration", time.Since(startNewStart).String(), "container_id", createResp.ID[:12])
	}

	d.containerID = createResp.ID
	d.started = true
	return nil
}

// ensureImage 确保镜像在本地存在。如果本地已存在则跳过拉取。
func (d *DockerSandbox) ensureImage(ctx context.Context) error {
	// 检查本地是否已有该镜像
	_, _, err := d.cli.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		// 本地已存在，跳过 pull
		if d.log != nil {
			d.log.Info(logger.CatApp, "sandbox: image already exists locally, skipping pull", "image", imageName)
		}
		return nil
	}

	// 本地不存在，拉取镜像
	pullStart := time.Now()
	if d.log != nil {
		d.log.Info(logger.CatApp, "sandbox: pulling image (not found locally)", "image", imageName)
	}
	reader, err := d.cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		if d.log != nil {
			d.log.LogError(ctx, logger.CatApp, "sandbox: pull image failed", err, "image", imageName)
		}
		return fmt.Errorf("sandbox: pull image %s: %w", imageName, err)
	}
	_, _ = io.Copy(io.Discard, reader)
	reader.Close()
	if d.log != nil {
		d.log.Info(logger.CatApp, "sandbox: ImagePull took",
			"duration", time.Since(pullStart).String(), "image", imageName)
	}
	return nil
}

// findContainer 查找名为 name 的任意状态容器的 ID 和状态。
// 返回第一个匹配的容器 ID 和其状态。
// 如果未找到，返回空字符串和空状态。
func (d *DockerSandbox) findContainer(ctx context.Context, name string) (id string, state string, err error) {
	filter := filters.NewArgs()
	filter.Add("name", name)
	listStart := time.Now()
	containers, err := d.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filter,
	})
	if d.log != nil {
		d.log.Info(logger.CatApp, "sandbox: ContainerList took",
			"duration", time.Since(listStart).String(), "err", fmt.Sprintf("%v", err))
	}
	if err != nil {
		return "", "", fmt.Errorf("sandbox: list containers: %w", err)
	}
	for _, c := range containers {
		for _, n := range c.Names {
			if n == "/"+name {
				if d.log != nil {
					d.log.Info(logger.CatApp, "sandbox: found container",
						"container_id", c.ID[:12], "state", c.State)
				}
				return c.ID, c.State, nil
			}
		}
	}
	if d.log != nil {
		d.log.Info(logger.CatApp, "sandbox: no container found", "name", name)
	}
	return "", "", nil
}

// Exec 在沙盒中利用 ContainerExecCreate 执行命令。
// 每次调用创建独立的 exec 实例，天然支持高并发。
func (d *DockerSandbox) Exec(ctx context.Context, cmd string) (stdout []byte, stderr []byte, err error) {
	d.mu.Lock()
	cid := d.containerID
	d.mu.Unlock()

	if cid == "" {
		return nil, nil, fmt.Errorf("sandbox: container not started")
	}

	// 创建 exec 实例
	execConfig := container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          []string{"/bin/sh", "-c", cmd},
	}
	execResp, err := d.cli.ContainerExecCreate(ctx, cid, execConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox: exec create: %w", err)
	}

	// 附加到 exec 进程获取输出流
	attachResp, err := d.cli.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox: exec attach: %w", err)
	}
	defer attachResp.Close()

	// 在 goroutine 中读取输出，使 context 取消能中断阻塞的 I/O。
	// Docker hijacked 连接不响应 Go context；关闭连接会触发 daemon
	// 清理 exec 进程并使 ReadAll 返回错误。
	type execOut struct {
		all []byte
		err error
	}
	outCh := make(chan execOut, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				outCh <- execOut{nil, fmt.Errorf("exec I/O goroutine panic: %v", r)}
			}
		}()
		all, err := io.ReadAll(attachResp.Reader)
		outCh <- execOut{all, err}
	}()

	var all []byte
	select {
	case out := <-outCh:
		if out.err != nil {
			return nil, nil, fmt.Errorf("sandbox: exec read: %w", out.err)
		}
		all = out.all
	case <-ctx.Done():
		attachResp.Close()
		if d.log != nil {
			d.log.WarnContext(ctx, logger.CatTool, "sandbox: exec cancelled, closing hijacked connection",
				"command", cmd, "reason", ctx.Err().Error())
		}
		return nil, nil, fmt.Errorf("sandbox: exec cancelled: %w", ctx.Err())
	}

	// Docker 使用 MultiplexedStream：每帧 8 字节头（1 字节 stream type + 3 字节 padding + 4 字节 size）
	// stream type: 1=stdout, 2=stderr
	stdout, stderr = demuxDockerStream(all)

	// 检查退出码
	inspectResp, err := d.cli.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		if d.log != nil {
			d.log.LogError(ctx, logger.CatTool, "sandbox: exec inspect failed", err, "command", cmd)
		}
		return stdout, stderr, fmt.Errorf("sandbox: exec inspect: %w", err)
	}
	if inspectResp.ExitCode != 0 {
		if d.log != nil {
			d.log.WarnContext(ctx, logger.CatTool, "sandbox: exec non-zero exit",
				"command", cmd, "exit_code", inspectResp.ExitCode)
		}
		return stdout, stderr, &ExecError{
			ExitCode: inspectResp.ExitCode,
			Stdout:   stdout,
			Stderr:   stderr,
		}
	}

	return stdout, stderr, nil
}

// Stop 强制立即杀死容器（ContainerKill），但保留容器存在状态，
// 允许稍后通过 Start 重新启动。使用 Kill 而非 Stop 以避免等待优雅退出超时。
// 这是一个幂等操作：如果容器已停止或不存在，不返回错误。
func (d *DockerSandbox) Stop(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	cid := d.containerID
	if cid == "" {
		return nil
	}

	if err := d.cli.ContainerKill(ctx, cid, "SIGKILL"); err != nil {
		// 如果容器已停止或不存在，这不是错误
		if d.log != nil {
			d.log.WarnContext(ctx, logger.CatApp, "sandbox: kill container warning",
				"err", err.Error(), "container_id", cid[:12])
		}
	}
	d.started = false
	// 注意：不清空 d.containerID，这样之后 Start() 仍可重新启动同一容器
	if d.log != nil {
		d.log.Info(logger.CatApp, "sandbox: container killed (SIGKILL)",
			"container_id", cid[:12])
	}
	return nil
}

// Destroy 停止该全局容器。容器保持已停止状态，
// 稍后可通过 Start() 重新启动。
func (d *DockerSandbox) Destroy(ctx context.Context) error {
	return d.Stop(ctx)
}

// DestroyPermanently 强制停止并永久删除该全局容器。
// 与 Destroy 不同，这个操作不可逆。
// 供 cleanup 子命令等需要彻底清理的场景使用。
func (d *DockerSandbox) DestroyPermanently(ctx context.Context) error {
	d.mu.Lock()
	cid := d.containerID
	d.started = false
	d.containerID = ""
	d.mu.Unlock()

	if cid == "" {
		return nil
	}

	err := d.cli.ContainerRemove(ctx, cid, container.RemoveOptions{Force: true})
	if err != nil {
		if d.log != nil {
			d.log.LogError(ctx, logger.CatApp, "sandbox: destroy container failed", err, "container_id", cid[:12])
		}
		return fmt.Errorf("sandbox: destroy container: %w", err)
	}
	return nil
}

// removeExisting 删除同名残留容器（若存在），静默执行不输出提示。
func (d *DockerSandbox) removeExisting(ctx context.Context) error {
	return d.removeByName(ctx, containerName, false)
}

// Cleanup 删除所有 soloqueue 创建的沙盒容器（供 cleanup 子命令使用），输出结果提示。
// 这会永久删除容器，而不是仅停止。
// 对于平时程序退出时的清理，使用 Destroy()（停止）即可。
// 此方法供 cleanup 子命令使用，目的是彻底清理所有孤立容器。
func (d *DockerSandbox) Cleanup(ctx context.Context) error {
	return d.removeByName(ctx, containerName, true)
}

// removeByName 按容器名查找并强制删除容器。
// verbose 控制是否输出 "no containers found" / "removed N" 提示。
func (d *DockerSandbox) removeByName(ctx context.Context, name string, verbose bool) error {
	filter := filters.NewArgs()
	filter.Add("name", name)
	containers, err := d.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filter,
	})
	if err != nil {
		return fmt.Errorf("sandbox: list containers: %w", err)
	}
	removed := 0
	for _, c := range containers {
		for _, n := range c.Names {
			if n == "/"+name {
				if err := d.cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
					if verbose {
						fmt.Fprintf(os.Stderr, "warning: failed to remove container %s: %v\n", c.ID[:12], err)
					}
				} else {
					removed++
				}
				break
			}
		}
	}
	if verbose {
		if removed == 0 {
			fmt.Println("no soloqueue sandbox containers found")
		} else {
			fmt.Printf("removed %d sandbox container(s)\n", removed)
		}
	}
	return nil
}

// ─── ExecError ──────────────────────────────────────────────────────────────

// ExecError 表示沙盒内命令执行失败（非零退出码）。
type ExecError struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

func (e *ExecError) Error() string {
	return fmt.Sprintf("sandbox: exec exit code %d", e.ExitCode)
}

// ─── Docker Socket 自动探测 ─────────────────────────────────────────────────

// dockerSocketCandidates 返回当前平台上候选 Docker socket 路径（按优先级排序）。
// 调用方应依次检查哪个文件存在，找到第一个即使用。
func dockerSocketCandidates() []string {
	home, _ := os.UserHomeDir()

	candidates := []string{}

	switch runtime.GOOS {
	case "darwin":
		// macOS: Docker Desktop / Rancher Desktop / OrbStack
		candidates = []string{
			"/var/run/docker.sock",                           // Docker Desktop 默认（或其它 runtime 的符号链接）
			filepath.Join(home, ".docker/run/docker.sock"),   // Docker Desktop 新版
			filepath.Join(home, ".orbstack/run/docker.sock"), // OrbStack
			filepath.Join(home, ".rd/docker.sock"),           // Rancher Desktop
		}
	case "linux":
		candidates = []string{
			"/var/run/docker.sock",                             // 标准路径
			filepath.Join(home, ".docker/desktop/docker.sock"), // Docker Desktop on Linux
			"/run/docker.sock",                                 // 部分发行版
		}
	default:
		// Windows / 其它：仅尝试默认
		candidates = []string{"/var/run/docker.sock"}
	}

	return candidates
}

// ensureDockerHost 检查 DOCKER_HOST 环境变量是否已设置且有效；
// 若未设置则自动探测本地 Docker socket 路径并设置环境变量。
// 这样后续 client.FromEnv 就能正确连接。
func ensureDockerHost() error {
	// 如果用户已显式设置 DOCKER_HOST，优先尊重用户配置
	if v := os.Getenv("DOCKER_HOST"); v != "" {
		return nil
	}

	for _, path := range dockerSocketCandidates() {
		if fi, err := os.Stat(path); err == nil && fi.Mode().Type() == os.ModeSocket {
			os.Setenv("DOCKER_HOST", "unix://"+path)
			return nil
		}
	}

	return fmt.Errorf("sandbox: no Docker socket found; is Docker / Rancher Desktop / OrbStack running?")
}

// ─── Docker 多路复用流解析 ───────────────────────────────────────────────────

// demuxDockerStream 将 Docker 多路复用流拆分为 stdout 和 stderr。
// Docker 使用 8 字节帧头：[streamType(1)] [padding(3)] [size(4)] + payload
// streamType: 1=stdout, 2=stderr
func demuxDockerStream(data []byte) (stdout, stderr []byte) {
	offset := 0
	for offset+8 <= len(data) {
		streamType := data[offset]
		// padding: data[offset+1 : offset+4]
		size := uint32(data[offset+4])<<24 | uint32(data[offset+5])<<16 |
			uint32(data[offset+6])<<8 | uint32(data[offset+7])

		offset += 8
		if offset+int(size) > len(data) {
			// 数据不完整，把剩余全部追加到 stdout
			stdout = append(stdout, data[offset:]...)
			break
		}

		payload := data[offset : offset+int(size)]
		offset += int(size)

		switch streamType {
		case 1:
			stdout = append(stdout, payload...)
		case 2:
			stderr = append(stderr, payload...)
		default:
			// 未知 stream type，追加到 stdout
			stdout = append(stdout, payload...)
		}
	}
	// 如果还有剩余数据（非帧格式），追加到 stdout
	if offset < len(data) {
		stdout = append(stdout, data[offset:]...)
	}
	return
}
