package runtime

import (
	"path/filepath"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/skill"
)

// buildSkills initializes the global skill registry and registers built-in and user-defined skills.
func (bc *buildContext) buildSkills() {
	skillStart := time.Now()
	skill.SetPackageLogger(bc.log)
	skillReg := skill.NewSkillRegistry()

	// 1. Register builtin skills first (lower priority)
	skill.RegisterBuiltinSkills(skillReg)

	// 2. Load user skills from workDir/skills/
	skillDirs := map[string]string{
		"user": filepath.Join(bc.workDir, "skills"),
	}
	if skills, err := skill.LoadSkillsFromDirs(skillDirs); err == nil {
		for _, s := range skills {
			_ = skillReg.Register(s)
		}
	}
	bc.skillReg = skillReg
	bc.skillDirs = skillDirs

	bc.log.Debug(logger.CatApp, "build: skills loaded", "duration", time.Since(skillStart).String())
}
