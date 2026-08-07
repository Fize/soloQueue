package tools

import (
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/cron"
)

func TestCronScope_EnabledAndModes(t *testing.T) {
	globalScope := CronAccessScope{Mode: CronAccessGlobal}
	if !globalScope.Enabled() || !globalScope.IsGlobal() || globalScope.IsTeam() {
		t.Errorf("globalScope behavior invalid: %+v", globalScope)
	}

	teamScope := CronAccessScope{Mode: CronAccessTeam, Owner: "engineering"}
	if !teamScope.Enabled() || teamScope.IsGlobal() || !teamScope.IsTeam() {
		t.Errorf("teamScope behavior invalid: %+v", teamScope)
	}

	emptyTeamScope := CronAccessScope{Mode: CronAccessTeam, Owner: ""}
	if emptyTeamScope.Enabled() {
		t.Errorf("empty owner teamScope should not be enabled")
	}
}

func TestCronToolsRequireExplicitScope(t *testing.T) {
	cfg := newCronToolTestConfig(t, CronAccessScope{})
	for _, tool := range Build(cfg) {
		if IsCronTool(tool.Name()) {
			t.Fatalf("disabled scope unexpectedly registered %q", tool.Name())
		}
	}
}

func TestAuthorizeCronTask(t *testing.T) {
	globalScope := CronAccessScope{Mode: CronAccessGlobal}
	teamScope := CronAccessScope{Mode: CronAccessTeam, Owner: "Engineering"}

	taskAny := &cron.Task{TargetAgent: "finance"}
	taskMatch := &cron.Task{TargetAgent: " engineering "}

	if err := authorizeCronTask(globalScope, taskAny); err != nil {
		t.Errorf("global scope should authorize any task, got %v", err)
	}

	if err := authorizeCronTask(teamScope, taskMatch); err != nil {
		t.Errorf("team scope should authorize matching team task (case-insensitive & trimmed), got %v", err)
	}

	if err := authorizeCronTask(teamScope, taskAny); err == nil {
		t.Errorf("team scope should reject task belonging to another team")
	}
}
