package sandbox

import "testing"

func TestPathMap(t *testing.T) {
	mounts := []Mount{
		{HostPath: "/Users/xiaobaitu/.soloqueue", ContainerPath: "/root/.soloqueue"},
		{HostPath: "/Users/xiaobaitu/github/project-a", ContainerPath: "/Users/xiaobaitu/github/project-a"},
	}
	pm := NewPathMap(mounts)

	tests := []struct {
		name  string
		input string
		toC   string // expected container path
		toH   string // expected host path
	}{
		{
			name:  "main workdir",
			input: "/Users/xiaobaitu/.soloqueue/groups/default.md",
			toC:   "/root/.soloqueue/groups/default.md",
			toH:   "/Users/xiaobaitu/.soloqueue/groups/default.md",
		},
		{
			name:  "project workspace",
			input: "/Users/xiaobaitu/github/project-a/src/main.go",
			toC:   "/Users/xiaobaitu/github/project-a/src/main.go",
			toH:   "/Users/xiaobaitu/github/project-a/src/main.go",
		},
		{
			name:  "exact mount root",
			input: "/Users/xiaobaitu/.soloqueue",
			toC:   "/root/.soloqueue",
			toH:   "/Users/xiaobaitu/.soloqueue",
		},
		{
			name:  "unknown path passthrough",
			input: "/tmp/something",
			toC:   "/tmp/something",
			toH:   "/tmp/something",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pm.ToContainerPath(tt.input); got != tt.toC {
				t.Errorf("ToContainerPath(%q) = %q, want %q", tt.input, got, tt.toC)
			}
			if got := pm.ToHostPath(tt.toC); got != tt.toH {
				t.Errorf("ToHostPath(%q) = %q, want %q", tt.toC, got, tt.toH)
			}
		})
	}
}

func TestPathMapLongestPrefix(t *testing.T) {
	// When multiple prefixes match, longest should win
	mounts := []Mount{
		{HostPath: "/a", ContainerPath: "/a"},
		{HostPath: "/a/b", ContainerPath: "/a/b"},
	}
	pm := NewPathMap(mounts)

	got := pm.ToContainerPath("/a/b/file.txt")
	want := "/a/b/file.txt"
	if got != want {
		t.Errorf("ToContainerPath = %q, want %q", got, want)
	}

	got = pm.ToContainerPath("/a/other.txt")
	want = "/a/other.txt"
	if got != want {
		t.Errorf("ToContainerPath = %q, want %q", got, want)
	}
}
