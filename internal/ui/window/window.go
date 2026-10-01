// Package window is EDSense's window process, `EDSense.exe --window`: one
// WebView2 page served from embedded files through Wails. It is started
// by the core (internal/ui/launch) and talks to it over its stdin and
// stdout. Only window_windows.go imports Wails, so the rest builds and
// tests anywhere.
package window

// Options is what main hands to the window.
type Options struct {
	Version string
}

// Default and smallest size, in device independent pixels.
const (
	DefaultWidth  = 1040
	DefaultHeight = 720
	MinWidth      = 480
	MinHeight     = 400
)

// Exit codes.
const (
	exitOK      = 0
	exitFailed  = 1
	exitNoCore  = 3 // not started by the core, or it did not answer
	exitDevOnly = 4 // built without -tags production
)
