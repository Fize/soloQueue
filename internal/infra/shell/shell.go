// Package shell defines the command interpreter used by agent tools.
package shell

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"os/exec"
	"runtime"
	"unicode/utf16"
)

func commandForOS(goos, command string) (string, []string) {
	if goos != "windows" {
		return "/bin/sh", []string{"-c", command}
	}
	script := "[Console]::InputEncoding = [System.Text.UTF8Encoding]::new(); [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new(); $OutputEncoding = [Console]::OutputEncoding; " + command + "\n$commandSucceeded = $?; if (-not $commandSucceeded) { if ($null -ne $LASTEXITCODE -and $LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; exit 1 }"
	units := utf16.Encode([]rune(script))
	raw := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(raw[i*2:], u)
	}
	return "powershell.exe", []string{"-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(raw)}
}

func CommandContext(ctx context.Context, command string) *exec.Cmd {
	name, args := commandForOS(runtime.GOOS, command)
	return exec.CommandContext(ctx, name, args...)
}

func Description() string {
	if runtime.GOOS == "windows" {
		return "Windows PowerShell"
	}
	return "/bin/sh -c"
}
