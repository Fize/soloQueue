package prompt

import (
	"path/filepath"
	"runtime"
	"strings"
)

func ExploreDir(workDir string) string {
	return filepath.Join(workDir, "explore")
}

func ShellDesc() string {
	if runtime.GOOS == "windows" {
		return "powershell.exe -Command (falls back to cmd.exe /c)"
	}
	return "/bin/sh -c"
}

func EnvSection(workDir, exploreDir string, xml bool, includeTimeInstruction bool) string {
	sep := string(filepath.Separator)

	var b strings.Builder
	if xml {
		b.WriteString("<environment>\n")
	} else {
		b.WriteString("# Environment\n\n")
	}

	b.WriteString("- Operating System: ")
	b.WriteString(runtime.GOOS)
	b.WriteString("\n- Architecture: ")
	b.WriteString(runtime.GOARCH)
	b.WriteString("\n- Shell: ")
	b.WriteString(ShellDesc())
	b.WriteString("\n- Working Directory: ")
	b.WriteString(workDir)
	b.WriteString("\n- Exploration Artifacts: ")
	b.WriteString(exploreDir)
	if includeTimeInstruction {
		b.WriteString("\n- Current Local Time: To obtain the current local time/date, run a shell command (e.g., `date` on Unix/macOS or `Get-Date` on Windows) using the execution tools, or check the timestamp in the latest user message.")
	}
	b.WriteString("\n- Path Separator: \"")
	b.WriteString(sep)
	b.WriteString("\"\n")

	if xml {
		b.WriteString("</environment>")
	}

	return b.String()
}
