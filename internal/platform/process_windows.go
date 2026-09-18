package platform

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// eachProcess calls f with every running process's exe name until f returns
// false.
func eachProcess(f func(exe string) bool) error {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snap)
	pe := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snap, &pe); err != nil {
		return err
	}
	for {
		if !f(windows.UTF16ToString(pe.ExeFile[:])) || windows.Process32Next(snap, &pe) != nil {
			return nil
		}
	}
}

// ProcessRunning reports whether a process with this exe name runs. When the
// process list can't be read it answers true, so nothing is switched off by
// mistake.
func ProcessRunning(exe string) bool {
	found := false
	err := eachProcess(func(name string) bool {
		found = strings.EqualFold(name, exe)
		return !found
	})
	return found || err != nil
}
