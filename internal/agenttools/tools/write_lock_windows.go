package tools

import (
	"context"
	"hash/fnv"
	"path/filepath"
	"strings"
)

var writeFileLocks = func() [64]chan struct{} {
	var locks [64]chan struct{}
	for i := range locks {
		locks[i] = make(chan struct{}, 1)
		locks[i] <- struct{}{}
	}
	return locks
}()

// Windows cannot reliably replace the same file concurrently within a process.
func lockWriteFile(ctx context.Context, path string) (func(), error) {
	canonical, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(filepath.Clean(canonical))))
	lock := writeFileLocks[h.Sum32()%uint32(len(writeFileLocks))]
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-lock:
	}
	return func() { lock <- struct{}{} }, nil
}
