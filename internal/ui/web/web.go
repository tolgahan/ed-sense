// Package web holds the window's page: plain HTML, CSS and ES modules
// with no build step, served from the exe.
package web

import (
	"embed"
	"io/fs"
)

//go:embed index.html app.css css js
var files embed.FS

// FS is the page's files at their paths on the page's origin.
func FS() fs.FS { return files }
