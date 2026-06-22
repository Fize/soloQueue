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

// DistFS returns the embedded filesystem containing web assets and skills.
func DistFS() fs.FS {
	return distFS()
}
