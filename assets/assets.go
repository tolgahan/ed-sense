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

// DSXProfile is the DSX controller profile for Elite Dangerous (gyro aim,
// touchpad and trigger setup).
//
//go:embed "dsx/Elite Dangerous.dsx"
var DSXProfile []byte
