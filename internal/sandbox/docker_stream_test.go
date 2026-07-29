package sandbox

import (
	"bytes"
	"net"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/pkg/stdcopy"
)

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

func TestCopyDockerOutputDetectsRawAndMultiplexedStreams(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		write     func(net.Conn)
		wantOut   string
		wantError string
	}{
		{
			name: "raw",
			write: func(conn net.Conn) {
				_, _ = conn.Write([]byte(`{"jsonrpc":"2.0"}` + "\n"))
			},
			wantOut: `{"jsonrpc":"2.0"}` + "\n",
		},
		{
			name: "multiplexed",
			write: func(conn net.Conn) {
				_, _ = stdcopy.NewStdWriter(conn, stdcopy.Stdout).Write([]byte("out\n"))
				_, _ = stdcopy.NewStdWriter(conn, stdcopy.Stderr).Write([]byte("err\n"))
			},
			wantOut:   "out\n",
			wantError: "err\n",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, server := net.Pipe()
			attach := types.NewHijackedResponse(client, types.MediaTypeRawStream)
			go func() {
				test.write(server)
				_ = server.Close()
			}()
			var stdout, stderr bytes.Buffer
			if _, err := copyDockerOutput(&attach, &stdout, &stderr); err != nil {
				t.Fatal(err)
			}
			if stdout.String() != test.wantOut || stderr.String() != test.wantError {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}
