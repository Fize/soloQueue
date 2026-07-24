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

func TestBuildDefaultMounts_IncludesSSHAndCache(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("User home dir unavailable")
	}

	sshDir := filepath.Join(home, ".ssh")
	_ = os.MkdirAll(sshDir, 0700)

	mounts := buildDefaultMounts()
	foundSSH := false
	foundCache := false

	for _, m := range mounts {
		if m.ContainerPath == "/root/.ssh" {
			foundSSH = true
		}
		if m.ContainerPath == "/root/.cache" {
			foundCache = true
		}
	}

	if !foundSSH {
		t.Errorf("buildDefaultMounts() missing /root/.ssh mount")
	}
	if !foundCache {
		t.Errorf("buildDefaultMounts() missing /root/.cache mount")
	}
}
