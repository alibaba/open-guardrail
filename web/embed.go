// Package web embeds the management shell; end users do not need Node.js.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static/*
var files embed.FS

func Files() fs.FS {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	} // Build-time invariant.
	return sub
}
