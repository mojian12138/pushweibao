package assets

import (
	"embed"
	"io/fs"
)

//go:embed static/*
var content embed.FS

func FS() fs.FS {
	root, err := fs.Sub(content, "static")
	if err != nil {
		panic(err)
	}
	return root
}
