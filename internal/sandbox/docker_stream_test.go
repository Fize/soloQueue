package sandbox

import "testing"

func TestDemuxDockerStream(t *testing.T) {
	data := []byte{
		1, 0, 0, 0, 0, 0, 0, 6, 'h', 'e', 'l', 'l', 'o', '\n',
		2, 0, 0, 0, 0, 0, 0, 6, 'e', 'r', 'r', 'o', 'r', '\n',
	}

	stdout, stderr := demuxDockerStream(data)

	if string(stdout) != "hello\n" {
		t.Errorf("stdout = %q, want %q", string(stdout), "hello\n")
	}
	if string(stderr) != "error\n" {
		t.Errorf("stderr = %q, want %q", string(stderr), "error\n")
	}
}
