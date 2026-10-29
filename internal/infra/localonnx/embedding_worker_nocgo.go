//go:build !cgo

package localonnx

import (
	"fmt"
	"os"
)

func InternalEmbeddingMain(_ []string) int {
	fmt.Fprintln(os.Stderr, "internal embedding requires cgo")
	return 1
}
