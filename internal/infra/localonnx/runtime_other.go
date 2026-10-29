//go:build !windows && cgo

package localonnx

func configureRuntimeDirectory(_ string) error { return nil }
