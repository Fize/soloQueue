package runtime

import (
	"path/filepath"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agenttools/skill"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// buildSkills initializes the registry from skills installed in the SoloQueue
// work directory. ClawHub owns installation and updates; SoloQueue only loads
// the resulting SKILL.md packages and hot-reloads their content.
func (bc *buildContext) buildSkills() {
	skillStart := time.Now()
	skill.SetPackageLogger(bc.log)
	skillReg := skill.NewSkillRegistry()

	// Load user skills from workDir/skills/
	userSkillsDir := filepath.Join(bc.workDir, "skills")
	skillDirs := map[string]string{
		"user": userSkillsDir,
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
