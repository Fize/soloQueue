// Package assets owns the two independent browser bundles embedded in the
// server binary: the Web Console and the read-only Status UI.
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
