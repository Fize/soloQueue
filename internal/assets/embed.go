// Package assets owns the three independent asset bundles embedded in the
// server binary. Keeping the Web Console, Status UI, and built-in Skill Store
// separate prevents a missing frontend bundle from changing Skill behavior.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

func subdir(name string) fs.FS {
	f, err := fs.Sub(embedded, "dist/"+name)
	if err != nil {
		panic(err)
	}
	return f
}

// WebFS returns the complete browser Web Console bundle.
func WebFS() fs.FS { return subdir("web") }

// StatusFS returns the read-only Status UI bundle.
func StatusFS() fs.FS { return subdir("status") }

// SkillsFS returns built-in Skill Store files.
func SkillsFS() fs.FS { return subdir("skills") }
