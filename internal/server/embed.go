package server

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var webDist embed.FS

func distFS() fs.FS {
	f, err := fs.Sub(webDist, "dist")
	if err != nil {
		panic(err)
	}
	return f
}
