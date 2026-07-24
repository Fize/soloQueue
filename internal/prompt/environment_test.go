package prompt

import (
	"strings"
	"testing"
)

// EnvSection no longer leaks workDir / exploreDir paths to the LLM.
// See internal/prompt/environment.go for rationale.

func TestEnvSection_XMLMode_DoesNotContainWorkDir(t *testing.T) {
	result := EnvSection("/home/user/.soloqueue", "/home/user/.soloqueue/explore", true, true)

	if strings.Contains(result, "/home/user/.soloqueue") {
		t.Error("EnvSection should not contain the workDir path")
	}
	if strings.Contains(result, "Working Directory") {
		t.Error("EnvSection should not contain 'Working Directory' line")
	}
	if strings.Contains(result, "Exploration Artifacts") {
		t.Error("EnvSection should not contain 'Exploration Artifacts' line")
	}
}

func TestEnvSection_MarkdownMode_DoesNotContainWorkDir(t *testing.T) {
	result := EnvSection("/home/user/.soloqueue", "/home/user/.soloqueue/explore", false, false)

	if strings.Contains(result, "/home/user/.soloqueue") {
		t.Error("EnvSection (markdown) should not contain the workDir path")
	}
	if strings.Contains(result, "Working Directory") {
		t.Error("EnvSection (markdown) should not contain 'Working Directory' line")
	}
	if strings.Contains(result, "Exploration Artifacts") {
		t.Error("EnvSection (markdown) should not contain 'Exploration Artifacts' line")
	}
}

func TestEnvSection_XMLMode_ContainsOSInfo(t *testing.T) {
	result := EnvSection("/home/user/.soloqueue", "/home/user/.soloqueue/explore", true, true)

	if !strings.Contains(result, "<environment>") {
		t.Error("EnvSection should have xml wrapper")
	}
	if !strings.Contains(result, "</environment>") {
		t.Error("EnvSection should close xml wrapper")
	}
	if !strings.Contains(result, "Operating System") {
		t.Error("EnvSection should contain OS info")
	}
	if !strings.Contains(result, "Architecture") {
		t.Error("EnvSection should contain Architecture info")
	}
	if !strings.Contains(result, "Shell") {
		t.Error("EnvSection should contain Shell info")
	}
	if !strings.Contains(result, "Path Separator") {
		t.Error("EnvSection should contain Path Separator")
	}
}

func TestEnvSection_IncludesTimeInstruction(t *testing.T) {
	withTime := EnvSection("/work", "/work/explore", true, true)
	if !strings.Contains(withTime, "Current Local Time") {
		t.Error("EnvSection should include time instruction when includeTimeInstruction=true")
	}

	withoutTime := EnvSection("/work", "/work/explore", true, false)
	if strings.Contains(withoutTime, "Current Local Time") {
		t.Error("EnvSection should NOT include time instruction when includeTimeInstruction=false")
	}
}

func TestEnvSection_MarkdownModeFormat(t *testing.T) {
	result := EnvSection("/work", "/work/explore", false, false)

	if !strings.HasPrefix(result, "# Environment") {
		t.Error("EnvSection markdown mode should start with '# Environment'")
	}
	if strings.Contains(result, "<environment>") {
		t.Error("EnvSection markdown mode should not contain xml tags")
	}
}
