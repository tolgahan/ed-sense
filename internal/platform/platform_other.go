//go:build !windows

package platform

import (
	"errors"
	"log"
)

func ProcessRunning(exe string) bool    { return true }
func InstanceRunning(mutex string) bool { return false }
func ProcessPath(exe string) string     { return "" }
func ForegroundIs(exe string) bool      { return false }
func KeyDown(vk int) bool               { return false }
func ShowInfo(title, text string)       { log.Print(text) }
func ShowError(title, text string)      { log.Print(text) }
func AskYesNo(title, text string) bool  { return false }
func OpenInEditor(path string)          {}
func OpenURL(address string)            {}
func OpenFolder(dir string)             {}
func AttachConsole()                    {}
func MakeDPIAware()                     {}
func SteamLibraries() []string          { return nil }

// The mouse is only moved and watched on Windows.

func MoveMouse(dx, dy int32) bool  { return false }
func InputBlocked(exe string) bool { return false }

func WatchMouse(stop <-chan struct{}, f func(MouseMove)) error {
	return errors.New("watching the mouse needs Windows")
}
