package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func dockerSocketCandidates() []string {
	home, _ := os.UserHomeDir()
	var candidates []string

	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/var/run/docker.sock",
			filepath.Join(home, ".docker/run/docker.sock"),
			filepath.Join(home, ".orbstack/run/docker.sock"),
			filepath.Join(home, ".rd/docker.sock"),
			filepath.Join(home, ".colima/default/docker.sock"),
			filepath.Join(home, ".local/share/containers/podman/machine/qemu/podman.sock"),
		}
	case "linux":
		candidates = []string{
			"/var/run/docker.sock",
			filepath.Join(home, ".docker/desktop/docker.sock"),
			"/run/docker.sock",
		}
	default:
		candidates = []string{"/var/run/docker.sock"}
	}
	return candidates
}

func ensureDockerHost() error {
	if v := os.Getenv("DOCKER_HOST"); v != "" {
		return nil
	}
	for _, path := range dockerSocketCandidates() {
		if fi, err := os.Stat(path); err == nil && fi.Mode().Type() == os.ModeSocket {
			os.Setenv("DOCKER_HOST", "unix://"+path)
			return nil
		}
	}
	return fmt.Errorf("sandbox: no Docker socket found; is Docker running?")
}
