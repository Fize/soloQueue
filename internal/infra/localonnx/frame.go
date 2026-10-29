//go:build cgo

package localonnx

import (
	"bufio"
	"errors"
	"io"
)

const maxWorkerFrame = 1 << 20

var errFrameTooLarge = errors.New("worker frame too large")

// readWorkerFrame drains oversized input up to its newline so subsequent requests remain usable.
func readWorkerFrame(r *bufio.Reader) ([]byte, error) {
	var frame []byte
	oversized := false
	for {
		chunk, err := r.ReadSlice('\n')
		if len(frame)+len(chunk) > maxWorkerFrame {
			oversized = true
		} else if !oversized {
			frame = append(frame, chunk...)
		}
		if err == nil {
			if oversized {
				return nil, errFrameTooLarge
			}
			return frame, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) && len(chunk) == 0 && len(frame) == 0 && !oversized {
			return nil, io.EOF
		}
		if errors.Is(err, io.EOF) {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
}
