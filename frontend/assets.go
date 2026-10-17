package frontend

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var files embed.FS

func Assets() fs.FS {
	f, err := fs.Sub(files, "dist")
	if err != nil {
		panic("找不到內建前端資產")
	}
	return f
}
