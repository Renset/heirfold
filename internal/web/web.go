// Package web embeds the built frontend into the binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var assets embed.FS

// FS returns the files of the built frontend.
func FS() fs.FS {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		// The embed directive guarantees that dist exists.
		panic(err)
	}
	return sub
}
