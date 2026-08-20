package server

import (
	"io/fs"

	"github.com/xiaobaitu/soloqueue/internal/assets"
)

// DistFS is retained as a source-compatible alias for integrations that used
// the old combined bundle. New code must choose WebFS, StatusFS, or SkillsFS.
func DistFS() fs.FS { return assets.SkillsFS() }
