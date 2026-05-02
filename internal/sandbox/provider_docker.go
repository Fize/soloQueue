// Package sandbox 提供容器化沙盒的抽象与 Docker 实现。
//
// 设计原则：
//   - 全局唯一共享沙盒：主程序启动时创建，退出时销毁。
//   - 不做硬性资源限制，容器默认使用宿主机全部可用算力。
//   - 容器启动时挂载所有 team workspace 目录，保证沙盒内可访问项目文件。
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

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── 接口定义 ────────────────────────────────────────────────────────────────

// Sandbox 是容器化沙盒的抽象接口。
type Sandbox interface {
	// Start 执行清理残留、镜像 Pull、容器拉起。
	Start(ctx context.Context) error

	// Exec 在沙盒中执行命令，需支持高并发调用。
	Exec(ctx context.Context, cmd string) (stdout []byte, stderr []byte, err error)

	// Destroy 强制停止并删除该全局容器。
	Destroy(ctx context.Context) error
}

// ─── Docker 实现 ─────────────────────────────────────────────────────────────

const (
	containerName = "soloqueue-sandbox"
	imageName     = "debian:bookworm-slim"
)

// DockerSandbox 基于 Docker SDK 的沙盒实现。
type DockerSandbox struct {
	cli         *client.Client
	containerID string
	mu          sync.Mutex // protects containerID + Start/Destroy transitions
	started     bool
	mounts      []Mount       // 需要挂载到容器内的目录列表
	workDir     string        // 容器内默认工作目录（~/.soloqueue 的容器内路径）
	pathMap     *PathMap      // 宿主机 ↔ 容器路径映射
	log         *logger.Logger // 可选的结构化日志
}

// SetLogger 设置 logger，nil 表示不记录日志。
func (d *DockerSandbox) SetLogger(l *logger.Logger) {
	d.log = l
}

// Mount 描述一个宿主机到容器的目录挂载。
type Mount struct {
	// HostPath 宿主机绝对路径。
	HostPath string
	// ContainerPath 容器内绝对路径。为空时默认 /root/.soloqueue（第一个 mount）或 /root/projects/<basename>。
	ContainerPath string
}

// PathMap 维护宿主机路径与容器路径之间的双向映射。
type PathMap struct {
	hostToContainer map[string]string // 前缀映射：宿主机前缀 → 容器前缀
	containerToHost map[string]string // 前缀映射：容器前缀 → 宿主机前缀
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
// 第一个 mount 视为主工作目录，映射到 /root/.soloqueue；
// 其余 mount 映射到 /root/projects/<basename>。
// 自动探测 Docker / Rancher Desktop / OrbStack 的 socket 路径。
func NewDockerSandbox(mounts []Mount) (*DockerSandbox, error) {
	if err := ensureDockerHost(); err != nil {
		return nil, err
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("sandbox: create docker client: %w", err)
	}

	// 为没有指定 ContainerPath 的 mount 分配容器内路径
	for i := range mounts {
		if mounts[i].ContainerPath != "" {
			continue
		}
		if i == 0 {
			mounts[i].ContainerPath = "/root/.soloqueue"
		} else {
			mounts[i].ContainerPath = "/root/projects/" + filepath.Base(mounts[i].HostPath)
		}
	}

	workDir := "/root/.soloqueue"
	if len(mounts) > 0 {
		workDir = mounts[0].ContainerPath
	}

	return &DockerSandbox{
		cli:     cli,
		mounts:  mounts,
		workDir: workDir,
		pathMap: NewPathMap(mounts),
	}, nil
}

// Start 清理残留容器、拉取镜像、拉起新容器。
func (d *DockerSandbox) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.started {
		return nil
	}

	// 1. 清理同名残留容器
	if err := d.removeExisting(ctx); err != nil {
		if d.log != nil {
			d.log.LogError(ctx, logger.CatApp, "sandbox: remove existing container failed", err)
		}
		return fmt.Errorf("sandbox: remove existing container: %w", err)
	}

	// 2. 拉取镜像
	reader, err := d.cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		if d.log != nil {
			d.log.LogError(ctx, logger.CatApp, "sandbox: pull image failed", err, "image", imageName)
		}
		return fmt.Errorf("sandbox: pull image %s: %w", imageName, err)
	}
	// 必须读完 pull 响应，否则镜像可能未完整落盘
	_, _ = io.Copy(io.Discard, reader)
	reader.Close()

	// 3. 创建容器
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
			Cmd:        []string{"/bin/sh", "-c", "apt-get update -qq && apt-get install -y --no-install-recommends -qq curl ca-certificates > /dev/null 2>&1; tail -f /dev/null"},
			WorkingDir: d.workDir,
		},
		hostConfig,
		nil,
		nil,
		containerName,
	)
	if err != nil {
		if d.log != nil {
			d.log.LogError(ctx, logger.CatApp, "sandbox: create container failed", err, "image", imageName)
		}
		return fmt.Errorf("sandbox: create container: %w", err)
	}

	// 4. 启动容器
	if err := d.cli.ContainerStart(ctx, createResp.ID, container.StartOptions{}); err != nil {
		// 启动失败时清理已创建的容器
		_ = d.cli.ContainerRemove(ctx, createResp.ID, container.RemoveOptions{Force: true})
		if d.log != nil {
			d.log.LogError(ctx, logger.CatApp, "sandbox: start container failed", err, "container_id", createResp.ID[:12])
		}
		return fmt.Errorf("sandbox: start container: %w", err)
	}

	d.containerID = createResp.ID
	d.started = true
	return nil
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

	// 读取全部输出（stdout + stderr 多路复用）
	all, err := io.ReadAll(attachResp.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox: exec read: %w", err)
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

// Destroy 强制停止并删除该全局容器。
func (d *DockerSandbox) Destroy(ctx context.Context) error {
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
			"/var/run/docker.sock",                   // Docker Desktop 默认（或其它 runtime 的符号链接）
			filepath.Join(home, ".docker/run/docker.sock"), // Docker Desktop 新版
			filepath.Join(home, ".orbstack/run/docker.sock"), // OrbStack
			filepath.Join(home, ".rd/docker.sock"),          // Rancher Desktop
		}
	case "linux":
		candidates = []string{
			"/var/run/docker.sock",                        // 标准路径
			filepath.Join(home, ".docker/desktop/docker.sock"), // Docker Desktop on Linux
			"/run/docker.sock",                            // 部分发行版
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
