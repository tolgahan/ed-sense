//go:build !windows

package platform

import "log"

func ProcessRunning(exe string) bool         { return true }
func AutostartEnabled(app string) bool       { return false }
func SetAutostart(app string, on bool) error { return nil }
func ShowInfo(title, text string)            { log.Print(text) }
func ShowError(title, text string)           { log.Print(text) }
func AskYesNo(title, text string) bool       { return false }
func OpenInEditor(path string)               {}
func AttachConsole()                         {}
