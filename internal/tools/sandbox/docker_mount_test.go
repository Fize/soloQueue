package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathMap_ToContainerPath(t *testing.T) {
	mounts := []Mount{
		{HostPath: "/Users/user/.ssh", ContainerPath: "/root/.ssh"},
		{HostPath: "/Users/user/project", ContainerPath: "/Users/user/project"},
	}
	pm := NewPathMap(mounts)

	tests := []struct {
		host string
		want string
	}{
		{"/Users/user/.ssh/id_rsa", "/root/.ssh/id_rsa"},
		{"/Users/user/project/main.go", "/Users/user/project/main.go"},
		{"/var/log/syslog", "/var/log/syslog"},
	}

	for _, tt := range tests {
		got := pm.ToContainerPath(tt.host)
		if got != tt.want {
			t.Errorf("ToContainerPath(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}

func TestPathMapCanonicalizesSymlinkedExistingPrefix(t *testing.T) {
	workspace := t.TempDir()
	canonical, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	pm := NewPathMap([]Mount{{HostPath: canonical, ContainerPath: canonical}})
	input := filepath.Join(workspace, "new", "file.txt")
	expected := filepath.ToSlash(filepath.Join(canonical, "new", "file.txt"))
	if got := pm.ToContainerPath(input); got != expected {
		t.Fatalf("mapped path = %q, want %q", got, expected)
	}
}

func TestBuildDefaultMounts_ExcludesSensitiveHomePaths(t *testing.T) {
	mounts := buildDefaultMounts()

	for _, m := range mounts {
		if m.ContainerPath == "/root/.ssh" {
			t.Errorf("buildDefaultMounts() must not expose /root/.ssh")
		}
		if m.ContainerPath == "/root/.cache" || m.ContainerPath == "/root/.local" {
			t.Errorf("buildDefaultMounts() must not expose shared home cache: %s", m.ContainerPath)
		}
	}
}

func TestBuildMounts_RejectsHomeAsWorkspace(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("User home dir unavailable")
	}
	if _, err := buildMounts(DockerOptions{Workspace: filepath.Clean(home)}); err == nil {
		t.Fatal("expected user home workspace mount to be rejected")
	}
}

func TestBuildMounts_RejectsSoloQueueControlDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	controlDir := filepath.Join(home, ".soloqueue")
	if err := os.MkdirAll(controlDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := buildMounts(DockerOptions{Workspace: controlDir}); err == nil {
		t.Fatal("expected SoloQueue control directory mount to be rejected")
	}
}

func TestContainerWorkingDirectoryPrefersWorkspaceOverArtifactMount(t *testing.T) {
	mounts := []Mount{
		{ContainerPath: "/project", Purpose: "workspace"},
		{ContainerPath: "/soloqueue/artifacts", Purpose: "artifact"},
	}
	if got := containerWorkingDirectory(mounts); got != "/project" {
		t.Fatalf("working directory = %q, want workspace", got)
	}
}

func TestSandboxNetworkIsDeniedByDefault(t *testing.T) {
	if got := sandboxNetworkMode(false); got != "none" {
		t.Fatalf("default network mode = %q", got)
	}
	if got := sandboxNetworkMode(true); got != "bridge" {
		t.Fatalf("enabled network mode = %q", got)
	}
}
