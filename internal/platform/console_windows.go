package platform

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// AttachConsole connects this GUI-subsystem exe to the console it was
// started from, so command-line modes can print.
func AttachConsole() {
	const parentProcess = ^uint32(0)
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")
	if r, _, _ := proc.Call(uintptr(parentProcess)); r == 0 {
		return
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = f, f
		fmt.Println()
	}
}
