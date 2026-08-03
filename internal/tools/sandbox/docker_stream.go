package sandbox

import (
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/pkg/stdcopy"
)

func copyDockerOutput(attach *types.HijackedResponse, stdout, stderr io.Writer) (int64, error) {
	header, err := attach.Reader.Peek(8)
	if err == nil && looksLikeDockerStreamHeader(header) {
		return stdcopy.StdCopy(stdout, stderr, attach.Reader)
	}
	return io.Copy(stdout, attach.Reader)
}

func looksLikeDockerStreamHeader(header []byte) bool {
	return len(header) >= 8 && header[0] <= byte(stdcopy.Systemerr) &&
		header[1] == 0 && header[2] == 0 && header[3] == 0
}

func demuxDockerStream(data []byte) (stdout, stderr []byte) {
	offset := 0
	for offset+8 <= len(data) {
		streamType := data[offset]
		size := uint32(data[offset+4])<<24 | uint32(data[offset+5])<<16 |
			uint32(data[offset+6])<<8 | uint32(data[offset+7])

		offset += 8
		if offset+int(size) > len(data) {
			stdout = append(stdout, data[offset:]...)
			break
		}

		payload := data[offset : offset+int(size)]
		offset += int(size)

		switch streamType {
		case 1:
			stdout = append(stdout, payload...)
		case 2:
			stderr = append(stderr, payload...)
		default:
			stdout = append(stdout, payload...)
		}
	}
	if offset < len(data) {
		stdout = append(stdout, data[offset:]...)
	}
	return
}
