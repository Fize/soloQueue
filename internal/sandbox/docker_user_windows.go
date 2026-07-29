//go:build windows

package sandbox

func sandboxContainerUser() string {
	return "65532:65532"
}

func sandboxContainerIDs() (int, int) {
	return 65532, 65532
}
