// Package web embeds the frontend (a dependency-free single page app written
// with native ES modules) into the gateway binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:static
var files embed.FS

// Static returns the files served at the site root.
func Static() fs.FS {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
