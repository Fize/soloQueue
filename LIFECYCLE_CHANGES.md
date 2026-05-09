# Docker Sandbox Lifecycle Management - Architecture Change

## Summary

Changed the Docker sandbox container lifecycle from **destroy-on-exit/create-on-startup** to **stop-on-exit/start-on-startup**. This allows container reuse across multiple program execution cycles, improving startup performance and resource efficiency.

**Date:** May 2026  
**Branch:** main  
**Status:** Implemented and compiled successfully

---

## Key Changes

### 1. New Container Reuse Logic (Start Method)

**File:** `internal/sandbox/provider_docker.go`

The `Start()` method now implements a three-tier startup strategy:

```go
// Start 启动沙盒容器。
// 启动优先级：
//   1. 如果已启动，直接返回
//   2. 检查是否存在已停止的容器，存在则直接启动
//   3. 否则创建新容器后启动
func (d *DockerSandbox) Start(ctx context.Context) error {
    // First check: already running?
    if d.started {
        return nil
    }
    
    // Second check: reuse stopped container?
    existingID, err := d.findStoppedContainer(ctx, containerName)
    if err == nil && existingID != "" {
        // Found stopped container - just start it
        if err := d.cli.ContainerStart(ctx, existingID, container.StartOptions{}); err != nil {
            // ... error handling
        }
        d.containerID = existingID
        d.started = true
        return nil
    }
    
    // Third: create new container
    // ... original creation logic
}
```

**Benefits:**
- First subsequent run reuses the stopped container from the previous session
- Faster startup: no need to pull image or create container
- Container data persists between sessions
- If stopped container is missing/corrupted, gracefully falls back to creation

### 2. New Stop Method (Non-destructive)

**File:** `internal/sandbox/provider_docker.go`

Added a new `Stop()` method that gracefully stops the container without deleting it:

```go
// Stop 停止容器但保留其存在状态，允许稍后通过 Start 重新启动。
// 这是一个幂等操作：如果容器已停止或不存在，不返回错误。
func (d *DockerSandbox) Stop(ctx context.Context) error {
    d.mu.Lock()
    defer d.mu.Unlock()
    
    cid := d.containerID
    if cid == "" {
        return nil
    }
    
    timeoutSecs := 10
    if err := d.cli.ContainerStop(ctx, cid, container.StopOptions{Timeout: &timeoutSecs}); err != nil {
        // Idempotent: if already stopped or missing, not an error
        if d.log != nil {
            d.log.WarnContext(ctx, logger.CatApp, "sandbox: stop container warning", err, ...)
        }
    }
    d.started = false
    // NOTE: do NOT clear d.containerID - allows Start() to restart same container
    return nil
}
```

**Key Features:**
- Idempotent: safe to call multiple times
- Graceful: 10-second timeout for container to stop cleanly
- Non-destructive: keeps container on disk for reuse
- Note: `d.containerID` is NOT cleared, preserving reference for restart

### 3. Updated Destroy Method

**File:** `internal/sandbox/provider_docker.go`

`Destroy()` now delegates to `Stop()` (non-destructive):

```go
// Destroy 停止该全局容器。容器保持已停止状态，
// 稍后可通过 Start() 重新启动。
func (d *DockerSandbox) Destroy(ctx context.Context) error {
    return d.Stop(ctx)
}
```

### 4. New DestroyPermanently Method

**File:** `internal/sandbox/provider_docker.go`

Added a separate method for permanent deletion (used by cleanup command):

```go
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
        // ... error handling
    }
    return nil
}
```

### 5. New Helper Method: findStoppedContainer

**File:** `internal/sandbox/provider_docker.go`

Added helper method to find and identify stopped containers:

```go
// findStoppedContainer 查找名为 name 的已停止容器的 ID。
// 如果找到多个，返回第一个；如果不存在或全部运行中，返回空字符串和 nil。
func (d *DockerSandbox) findStoppedContainer(ctx context.Context, name string) (string, error) {
    filter := filters.NewArgs()
    filter.Add("name", name)
    containers, err := d.cli.ContainerList(ctx, container.ListOptions{
        All:     true,
        Filters: filter,
    })
    if err != nil {
        return "", fmt.Errorf("sandbox: list containers: %w", err)
    }
    for _, c := range containers {
        for _, n := range c.Names {
            if n == "/"+name {
                // Check if container is stopped
                if c.State == "exited" || c.State == "stopped" {
                    return c.ID, nil
                }
                break
            }
        }
    }
    return "", nil
}
```

### 6. Updated Interface Documentation

**File:** `internal/sandbox/provider_docker.go`

Updated the `Sandbox` interface comments to reflect new behavior:

```go
// Sandbox 是容器化沙盒的抽象接口。
type Sandbox interface {
    // Start 执行清理残留、镜像 Pull、容器拉起，或重用已停止的容器。
    Start(ctx context.Context) error
    
    // Exec 在沙盒中执行命令，需支持高并发调用。
    Exec(ctx context.Context, cmd string) (stdout []byte, stderr []byte, err error)
    
    // Destroy 停止该全局容器。容器保持已停止状态，稍后可通过 Start 重新启动。
    Destroy(ctx context.Context) error
}
```

### 7. Updated Package Documentation

**File:** `internal/sandbox/provider_docker.go`

Updated package-level docstring:

```go
// 设计原则（已更新）：
//   - 全局唯一共享沙盒：主程序启动时创建或重用已停止的容器，退出时停止（不删除）。
//   - 容器允许跨多个程序运行周期重用：通过 Stop/Start 模式实现容器生命周期管理。
//   - 不做硬性资源限制，容器默认使用宿主机全部可用算力。
//   - 容器启动时挂载所有 workspace 目录：家目录映射到 /root/.soloqueue，
//     其余 workspace 容器内路径与宿主机路径完全一致（1:1 映射）。
//   - Exec 支持高并发调用（每次 exec 独立，互不阻塞）。
```

---

## Impact on Shutdown Flow

### TUI Mode Shutdown (main.go)

No code changes needed. The existing deferred shutdown handler:

```go
defer func() {
    done := make(chan struct{})
    go func() {
        defer close(done)
        mgr.Shutdown(3 * time.Second)
        rt.Shutdown()  // calls Stack.Shutdown() -> rt.DockerSandbox.Destroy() -> Stop()
    }()
    select {
    case <-done:
    case <-time.After(4 * time.Second):
        log.Warn(logger.CatApp, "shutdown timed out, exiting")
    }
}()
```

Now calls `Stop()` instead of `Destroy()` (which previously removed the container).

### Serve Mode Shutdown (cli/commands.go)

No code changes needed. Signal handler:

```go
go func() {
    <-rootCtx.Done()
    log.Info(logger.CatApp, "shutdown signal received")
    if qqGateway != nil {
        qqGateway.Close()
    }
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    _ = srv.Shutdown(shutdownCtx)
    mgr.Shutdown(5 * time.Second)
}()
```

Then `Stack.Shutdown()` is called, which now calls `Stop()` instead of `Destroy()`.

### Cleanup Command (cli/commands.go)

**Status:** Works as intended (no changes needed)

The cleanup command calls `sb.Cleanup(ctx)`, which:
1. Calls `removeByName()` to permanently delete containers
2. Displays "removed N sandbox container(s)" or "no soloqueue sandbox containers found"

This provides users with a way to explicitly purge old containers if needed.

---

## State Transitions

### Before (Old Behavior)

```
Program Start
    ↓
    Create Container
    ↓
    Start Container
    ↓
Program Running
    ↓
Program Exit
    ↓
    Destroy Container (DELETE)
    ↓
Program State: No container exists
```

### After (New Behavior)

```
Session 1:
    Program Start
        ↓
        Create Container
        ↓
        Start Container
        ↓
    Program Running
        ↓
    Program Exit
        ↓
        Stop Container (KEEP ON DISK)
        ↓
    Program State: Container exists but is stopped

Session 2 (on restart):
    Program Start
        ↓
        Find Stopped Container
        ↓
        Start Container (REUSE)
        ↓
    Program Running
        ↓
    Program Exit
        ↓
        Stop Container (KEEP ON DISK)
        ↓
    Program State: Container exists but is stopped

[Optional] User runs cleanup command:
    soloqueue cleanup
        ↓
        Destroy Container Permanently (DELETE)
        ↓
    Program State: No container exists
```

---

## Benefits

1. **Faster Restarts:** Second and subsequent program runs reuse the container without recreating it
2. **Resource Efficiency:** No repeated image pulls or container creation overhead
3. **Container State Persistence:** Any files or data in the container survive between program cycles
4. **Idempotent Operations:** Stop/Start are safe to call multiple times
5. **Graceful Shutdown:** 10-second timeout allows processes to shut down cleanly
6. **Backward Compatible:** `cleanup` command still available for explicit purging
7. **Better Error Handling:** Gracefully falls back to creation if stopped container is corrupted

---

## Rollback Path

To revert to the previous destroy-on-exit behavior:

1. Change `Destroy()` to call `DestroyPermanently()` instead of `Stop()`
2. Remove the container reuse logic from `Start()` method
3. Delete the `Stop()` and `findStoppedContainer()` methods
4. Revert package documentation

---

## Testing Recommendations

1. **Test Container Reuse:**
   - Start program → Stop program → Start program again
   - Verify same container is reused (check container ID)
   - Verify startup is faster

2. **Test Container Creation Fallback:**
   - Manually delete the stopped container via Docker
   - Restart program
   - Verify new container is created

3. **Test Cleanup Command:**
   - Run `soloqueue cleanup`
   - Verify output shows removed containers
   - Verify containers are permanently deleted

4. **Test Graceful Shutdown:**
   - Start program with long-running process in container
   - Send SIGTERM to program
   - Verify process has ~10 seconds to shut down cleanly

5. **Test Multiple Parallel Restarts:**
   - Stress test rapid stop/start cycles
   - Verify no race conditions or state corruption

---

## Files Modified

- `internal/sandbox/provider_docker.go` (major changes)
  - Added: `Stop()` method
  - Added: `findStoppedContainer()` helper
  - Added: `DestroyPermanently()` method
  - Updated: `Start()` with reuse logic
  - Updated: `Destroy()` to delegate to Stop
  - Updated: Interface and package documentation
  - Removed: `time` import (unused after change)

**Files Not Modified:**
- `cmd/soloqueue/main.go` (no changes needed)
- `cmd/soloqueue/cli/commands.go` (no changes needed)
- `internal/runtime/build.go` (no changes needed)
- `internal/runtime/stack.go` (no changes needed)

---

## Build Status

✅ **Build Successful**
- All compilation errors resolved
- Binary created successfully: `/tmp/soloqueue_test` (36MB, arm64)
- No import errors
- No type errors

---

## Summary of Code Changes

| Method | Old Behavior | New Behavior |
|--------|---|---|
| `Start()` | Create and start container every time | Check for stopped container, reuse if found, else create |
| `Stop()` | N/A (didn't exist) | Gracefully stop container, keep on disk |
| `Destroy()` | Remove container permanently | Alias for `Stop()` (non-destructive) |
| `DestroyPermanently()` | N/A (didn't exist) | Permanently delete container |
| `Cleanup()` | Remove all containers | Remove all containers (unchanged) |

