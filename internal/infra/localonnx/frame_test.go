//go:build cgo

package localonnx

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestWorkerFramesRecoverAfterOversize(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader(strings.Repeat("x", maxWorkerFrame) + "\n{}\n"))
	if _, err := readWorkerFrame(reader); !errors.Is(err, errFrameTooLarge) {
		t.Fatalf("oversized error: %v", err)
	}
	next, err := readWorkerFrame(reader)
	if err != nil || string(next) != "{}\n" {
		t.Fatalf("next frame %q, %v", next, err)
	}
	if _, err = readWorkerFrame(reader); !errors.Is(err, io.EOF) {
		t.Fatalf("EOF: %v", err)
	}
}
func TestWorkerFrameRejectsPartialInput(t *testing.T) {
	if _, err := readWorkerFrame(bufio.NewReader(strings.NewReader("{}"))); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("partial frame: %v", err)
	}
}
