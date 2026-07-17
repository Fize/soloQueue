package tools

import (
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/cron"
)

// CronAccessMode controls which scheduled tasks an agent may manage.
type CronAccessMode string

const (
	CronAccessGlobal CronAccessMode = "global"
	CronAccessTeam   CronAccessMode = "team"
)

// CronAccessScope is trusted runtime configuration. It is never supplied by
// tool arguments, so an agent cannot widen its own scheduling permissions.
type CronAccessScope struct {
	Mode  CronAccessMode
	Owner string
}

func (s CronAccessScope) Enabled() bool {
	return s.Mode == CronAccessGlobal || (s.Mode == CronAccessTeam && strings.TrimSpace(s.Owner) != "")
}

func (s CronAccessScope) IsGlobal() bool { return s.Mode == CronAccessGlobal }
func (s CronAccessScope) IsTeam() bool   { return s.Mode == CronAccessTeam }

func authorizeCronTask(scope CronAccessScope, task *cron.Task) error {
	if scope.IsGlobal() {
		return nil
	}
	if scope.IsTeam() && strings.EqualFold(strings.TrimSpace(task.TargetAgent), strings.TrimSpace(scope.Owner)) {
		return nil
	}
	return fmt.Errorf("cron job not found in this agent's scope")
}
