//go:build !windows

package tools

import "context"

func lockWriteFile(context.Context, string) (func(), error) { return func() {}, nil }
