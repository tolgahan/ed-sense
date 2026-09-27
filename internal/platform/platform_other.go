//go:build !windows

package platform

import "log"

func ProcessRunning(exe string) bool         { return true }
func ProcessPath(exe string) string          { return "" }
func ForegroundIs(exe string) bool           { return false }
func KeyDown(vk int) bool                    { return false }
func AutostartEnabled(app string) bool       { return false }
func SetAutostart(app string, on bool) error { return nil }
func ShowInfo(title, text string)            { log.Print(text) }
func ShowError(title, text string)           { log.Print(text) }
func AskYesNo(title, text string) bool       { return false }
func OpenInEditor(path string)               {}
func AttachConsole()                         {}
func MakeDPIAware()                          {}
func SteamLibraries() []string               { return nil }
