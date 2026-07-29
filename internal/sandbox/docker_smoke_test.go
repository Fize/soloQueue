package sandbox

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDockerBackendIsolationSmoke(t *testing.T) {
	if os.Getenv("SOLOQUEUE_DOCKER_SMOKE") != "1" {
		t.Skip("set SOLOQUEUE_DOCKER_SMOKE=1 to run the real backend test")
	}
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	workspaceFile := filepath.Join(workspace, "inside.txt")
	canary := filepath.Join(base, "outside-canary.txt")
	if err := os.WriteFile(workspaceFile, []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canary, []byte("must-not-be-visible"), 0o644); err != nil {
		t.Fatal(err)
	}

	backend, err := NewDockerBackend(DockerOptions{
		Workspace: workspace,
		Owner:     "smoke",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := backend.Start(ctx); err != nil {
		t.Fatal(err)
	}

	backend.mu.Lock()
	containerID := backend.containerID
	backend.mu.Unlock()
	inspect, err := backend.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		t.Fatal(err)
	}
	if inspect.HostConfig.NetworkMode != "none" {
		t.Fatalf("default network mode = %q", inspect.HostConfig.NetworkMode)
	}

	data, err := backend.ReadFile(ctx, workspaceFile, ReadFileOptions{MaxSize: 1024})
	if err != nil || string(data) != "inside" {
		t.Fatalf("workspace read data=%q err=%v", data, err)
	}
	if _, err := backend.ReadFile(ctx, canary, ReadFileOptions{MaxSize: 1024}); err == nil {
		t.Fatal("unmounted host canary was visible inside sandbox")
	}

	written := filepath.Join(workspace, "written.txt")
	if _, err := backend.WriteFile(ctx, written, []byte("sandbox-write"), WriteFileOptions{
		Overwrite: true,
		MaxSize:   1024,
	}); err != nil {
		t.Fatal(err)
	}
	hostData, err := os.ReadFile(written)
	if err != nil || string(hostData) != "sandbox-write" {
		t.Fatalf("workspace write data=%q err=%v", hostData, err)
	}

	result, err := backend.RunCommand(ctx, "pwd", RunCommandOptions{MaxOutput: 4096})
	if err != nil {
		t.Fatal(err)
	}
	canonicalWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(result.Stdout)) != canonicalWorkspace {
		t.Fatalf("default working directory = %q", result.Stdout)
	}

	result, err = backend.RunCommand(ctx, `
		test "$(id -u)" != 0 &&
		for tool in rtk gopls bash-language-server pyright-langserver typescript-language-server vue-language-server yaml-language-server clangd; do
			command -v "$tool" >/dev/null || exit 19
		done
	`, RunCommandOptions{WorkingDirectory: workspace, MaxOutput: 4096})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("sandbox runtime contract: exit=%d stderr=%s err=%v", result.ExitCode, result.Stderr, err)
	}

	process, err := backend.StartProcess(ctx, ProcessSpec{Command: "/bin/cat", WorkingDirectory: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := process.Stdin().Write([]byte("ping\n")); err != nil {
		t.Fatal(err)
	}
	if err := process.Stdin().Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(process.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Wait(); err != nil {
		t.Fatal(err)
	}
	if string(output) != "ping\n" {
		t.Fatalf("long-lived process output = %q", output)
	}

	streamCtx, streamCancel := context.WithTimeout(ctx, 5*time.Second)
	defer streamCancel()
	stream, err := backend.StartProcess(streamCtx, ProcessSpec{
		Command:          "/bin/sh",
		Args:             []string{"-c", `IFS= read -r line; printf 'reply:%s\n' "$line"; sleep 30`},
		WorkingDirectory: workspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Stdin().Write([]byte("live\n")); err != nil {
		t.Fatal(err)
	}
	liveResult := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(stream.Stdout()).ReadString('\n')
		liveResult <- line
	}()
	select {
	case line := <-liveResult:
		if line != "reply:live\n" {
			t.Fatalf("streaming process output = %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("streaming process response was buffered until process exit")
	}
	if err := stream.Kill(); err != nil {
		t.Fatal(err)
	}
}
