package runtime

import (
	"path/filepath"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/skill"
)

// buildSkills initializes the global skill registry and registers built-in and user-defined skills.
//
// The production catalog is the embedded skill store (see internal/server/dist/skills,
// built by `make build-web` and embedded via //go:embed). User-installed skills live
// in <workDir>/skills/ and override embedded entries with the same ID.
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
