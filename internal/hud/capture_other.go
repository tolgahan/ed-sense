//go:build !windows

package hud

// NewScreenGrabber returns nil: screen capture is only implemented on Windows.
func NewScreenGrabber() Grabber { return nil }
