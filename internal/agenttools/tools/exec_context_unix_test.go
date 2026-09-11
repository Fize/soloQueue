//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package tools

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

const fifoOperationTimeout = 250 * time.Millisecond

func makeFIFO(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/pipe"
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func unblockFIFO(t *testing.T, path string) {
	t.Helper()
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Errorf("unblock FIFO: %v", err)
		return
	}
	if err := syscall.Close(fd); err != nil {
		t.Errorf("close FIFO: %v", err)
	}
}

func TestReadFileRejectsFIFOWithoutBlocking(t *testing.T) {
	path := makeFIFO(t)
	ctx, cancel := context.WithTimeout(context.Background(), fifoOperationTimeout)
	defer cancel()
	type outcome struct {
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		_, err := NewExecutor().ReadFile(ctx, path, ReadFileOptions{})
		done <- outcome{err: err}
	}()

	timer := time.NewTimer(fifoOperationTimeout)
	defer timer.Stop()
	var got outcome
	blocked := false
	select {
	case got = <-done:
	case <-timer.C:
		blocked = true
		unblockFIFO(t, path)
		got = <-done
	}
	if blocked {
		t.Fatal("ReadFile blocked opening a FIFO")
	}
	if got.err == nil || got.err.Error() != "not a regular file: "+path {
		t.Fatalf("error = %v, want non-regular file rejection", got.err)
	}
}

func TestGlobLiteralFIFOUsesStatWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/pipe"
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), fifoOperationTimeout)
	defer cancel()
	type outcome struct {
		matches []string
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		matches, err := NewExecutor().Glob(ctx, dir, "pipe", GlobOptions{})
		done <- outcome{matches: matches, err: err}
	}()

	timer := time.NewTimer(fifoOperationTimeout)
	defer timer.Stop()
	var got outcome
	blocked := false
	select {
	case got = <-done:
	case <-timer.C:
		blocked = true
		unblockFIFO(t, path)
		got = <-done
	}
	if blocked {
		t.Fatal("Glob blocked opening a literal FIFO")
	}
	if got.err != nil {
		t.Fatal(got.err)
	}
	if len(got.matches) != 1 || got.matches[0] != path {
		t.Fatalf("matches = %v, want [%s]", got.matches, path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("FIFO was modified: %v", err)
	}
}
