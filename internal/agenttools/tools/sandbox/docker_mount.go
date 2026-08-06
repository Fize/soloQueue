package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Mount describes a host to container directory mount.
type Mount struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
	Purpose       string
}

// DockerOptions selects the workspace-scoped resources exposed by the
// DockerBackend. Empty paths are not mounted.
type DockerOptions struct {
	Workspace      string
	CacheDir       string
	Owner          string
	NetworkEnabled bool
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
	originalPath := filepath.Clean(hostPath)
	hostPath = resolvePathForMapping(hostPath)
	best := ""
	for hp := range pm.hostToContainer {
		if (hostPath == hp || strings.HasPrefix(hostPath, hp+string(filepath.Separator))) && len(hp) > len(best) {
			best = hp
		}
	}
	if best == "" {
		return filepath.ToSlash(originalPath)
	}
	rel := hostPath[len(best):]
	return filepath.ToSlash(filepath.Clean(pm.hostToContainer[best] + rel))
}

// resolvePathForMapping canonicalizes existing symlink prefixes while still
// supporting a not-yet-created final path. This matters on macOS where /var is
// a symlink to /private/var.
func resolvePathForMapping(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	current := abs
	var suffix []string
	for {
		if resolved, resolveErr := filepath.EvalSymlinks(current); resolveErr == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(abs)
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func (pm *PathMap) ToHostPath(containerPath string) string {
	best := ""
	for cp := range pm.containerToHost {
		if (containerPath == cp || strings.HasPrefix(containerPath, cp+"/")) && len(cp) > len(best) {
			best = cp
		}
	}
	if best == "" {
		return filepath.Clean(containerPath)
	}
	rel := strings.TrimPrefix(containerPath, best)
	rel = strings.TrimPrefix(rel, "/")
	return filepath.Join(pm.containerToHost[best], filepath.FromSlash(rel))
}

// buildDefaultMounts is retained for compatibility with direct constructor
// callers. It now exposes only the current workspace; it never mounts ~/.ssh
// or the SoloQueue work directory implicitly.
func buildDefaultMounts() []Mount {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		return nil
	}
	mounts, _ := buildMounts(DockerOptions{Workspace: cwd})
	return mounts
}

func buildMounts(opts DockerOptions) ([]Mount, error) {
	var mounts []Mount
	add := func(path, target string, readOnly bool, purpose string) error {
		if path == "" {
			return nil
		}
		canonical, err := canonicalMountPath(path)
		if err != nil {
			return fmt.Errorf("%s mount: %w", purpose, err)
		}
		mounts = append(mounts, Mount{
			HostPath:      canonical,
			ContainerPath: filepath.ToSlash(target),
			ReadOnly:      readOnly,
			Purpose:       purpose,
		})
		return nil
	}

	if opts.Workspace != "" {
		canonical, err := canonicalMountPath(opts.Workspace)
		if err != nil {
			return nil, fmt.Errorf("workspace mount: %w", err)
		}
		if err := validateWorkspaceMount(canonical); err != nil {
			return nil, err
		}
		mounts = append(mounts, Mount{
			HostPath:      canonical,
			ContainerPath: filepath.ToSlash(canonical),
			Purpose:       "workspace",
		})
	}
	if err := add(opts.CacheDir, "/root/.cache/soloqueue", false, "cache"); err != nil {
		return nil, err
	}

	if home, err := os.UserHomeDir(); err == nil {
		sshPath := filepath.Join(home, ".ssh")
		if _, err := os.Stat(sshPath); err == nil {
			if err := add(sshPath, "/root/.ssh", true, "ssh"); err != nil {
				return nil, err
			}
		}

		soloqueuePath := filepath.Join(home, ".soloqueue")
		if _, err := os.Stat(soloqueuePath); err == nil {
			if err := add(soloqueuePath, "/root/.soloqueue", false, "soloqueue"); err != nil {
				return nil, err
			}
		}
	}

	return deduplicateMounts(mounts), nil
}

func canonicalMountPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", resolved)
	}
	return filepath.Clean(resolved), nil
}

func validateWorkspaceMount(path string) error {
	volume := filepath.VolumeName(path)
	root := string(filepath.Separator)
	if volume != "" {
		root = volume + string(filepath.Separator)
	}
	if filepath.Clean(path) == filepath.Clean(root) {
		return fmt.Errorf("sandbox: refusing filesystem root as workspace mount")
	}
	if home, err := os.UserHomeDir(); err == nil {
		homeResolved, resolveErr := filepath.EvalSymlinks(home)
		if resolveErr == nil {
			if filepath.Clean(path) == filepath.Clean(homeResolved) {
				return fmt.Errorf("sandbox: refusing user home as workspace mount")
			}
			controlDir := filepath.Join(homeResolved, ".soloqueue")
			if filepath.Clean(path) == filepath.Clean(controlDir) {
				return fmt.Errorf("sandbox: refusing SoloQueue control directory as workspace mount")
			}
		}
	}
	return nil
}

func deduplicateMounts(mounts []Mount) []Mount {
	seen := make(map[string]bool)
	var res []Mount
	for _, m := range mounts {
		key := filepath.Clean(m.HostPath) + "\x00" + filepath.Clean(m.ContainerPath)
		if seen[key] {
			continue
		}
		seen[key] = true
		res = append(res, m)
	}
	return res
}
