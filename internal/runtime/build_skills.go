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

	// Load user skills from workDir/skills/
	userSkillsDir := filepath.Join(bc.workDir, "skills")
	skillDirs := map[string]string{
		"user": userSkillsDir,
	}

	// Auto-update local self-created skills at startup
	var localCatalogs []string
	if absUser, err := filepath.Abs(userSkillsDir); err == nil {
		for _, p := range []string{"skills", "../skills", filepath.Join(bc.workDir, "store", "skills")} {
			if absP, errAbs := filepath.Abs(p); errAbs == nil && absP != absUser {
				localCatalogs = append(localCatalogs, p)
			}
		}
	} else {
		localCatalogs = []string{"skills", "../skills", filepath.Join(bc.workDir, "store", "skills")}
	}
	skill.AutoUpdateLocalSkills(bc.workDir, skillDirs["user"], localCatalogs)

	if skills, err := skill.LoadSkillsFromDirs(skillDirs); err == nil {
		for _, s := range skills {
			_ = skillReg.Register(s)
		}
	}
	bc.skillReg = skillReg
	bc.skillDirs = skillDirs

	bc.log.Debug(logger.CatApp, "build: skills loaded", "duration", time.Since(skillStart).String())
}
