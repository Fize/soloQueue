package sandbox

import (
	"os"
	"path/filepath"
	"strings"
)

// Mount describes a host to container directory mount.
type Mount struct {
	HostPath      string
	ContainerPath string
}

// PathMap handles bidirectional path mapping between host and container.
type PathMap struct {
	hostToContainer map[string]string
	containerToHost map[string]string
}

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

func (pm *PathMap) ToContainerPath(hostPath string) string {
	best := ""
	for hp := range pm.hostToContainer {
		if (hostPath == hp || strings.HasPrefix(hostPath, hp+string(filepath.Separator))) && len(hp) > len(best) {
			best = hp
		}
	}
	if best == "" {
		return hostPath
	}
	rel := hostPath[len(best):]
	return filepath.ToSlash(filepath.Clean(pm.hostToContainer[best] + rel))
}

// buildDefaultMounts constructs Pattern 1 mounts including workspace, user ~/.ssh & persistent cache.
func buildDefaultMounts() []Mount {
	var mounts []Mount
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		sshDir := filepath.Join(home, ".ssh")
		if fi, err := os.Stat(sshDir); err == nil && fi.IsDir() {
			mounts = append(mounts, Mount{
				HostPath:      sshDir,
				ContainerPath: "/root/.ssh",
			})
		}

		// Pattern 1 Persistent Cache mounts
		cacheDir := filepath.Join(home, ".soloqueue", "sandbox", "cache")
		_ = os.MkdirAll(cacheDir, 0755)
		mounts = append(mounts, Mount{
			HostPath:      cacheDir,
			ContainerPath: "/root/.cache",
		})
		mounts = append(mounts, Mount{
			HostPath:      cacheDir,
			ContainerPath: "/root/.local",
		})

		soloDir := filepath.Join(home, ".soloqueue")
		if fi, err := os.Stat(soloDir); err == nil && fi.IsDir() {
			mounts = append(mounts, Mount{
				HostPath:      soloDir,
				ContainerPath: filepath.ToSlash(soloDir),
			})
		}
	}

	cwd, err := os.Getwd()
	if err == nil && cwd != "" {
		mounts = append(mounts, Mount{
			HostPath:      cwd,
			ContainerPath: filepath.ToSlash(cwd),
		})
	}

	return deduplicateMounts(mounts)
}

func deduplicateMounts(mounts []Mount) []Mount {
	seen := make(map[string]bool)
	var res []Mount
	for _, m := range mounts {
		if seen[m.ContainerPath] {
			continue
		}
		seen[m.ContainerPath] = true
		res = append(res, m)
	}
	return res
}
