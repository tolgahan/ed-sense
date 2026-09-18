// Package assets holds the files built into the executable.
package assets

import _ "embed"

// Tray icons.
var (
	//go:embed icons/active.ico
	IconActive []byte
	//go:embed icons/idle.ico
	IconIdle []byte
	//go:embed icons/error.ico
	IconError []byte
)
