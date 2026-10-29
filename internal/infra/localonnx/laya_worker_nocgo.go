//go:build !cgo

package localonnx

import (
	"fmt"
	"os"
)

func InternalLayaMain(_ []string) int {
	fmt.Fprintln(os.Stderr, "internal laya requires cgo")
	return 1
}
