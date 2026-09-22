package platform

import (
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// eachProcess calls f with every running process's exe name and ID until f
// returns false.
func eachProcess(f func(exe string, pid uint32) bool) error {
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
		if !f(windows.UTF16ToString(pe.ExeFile[:]), pe.ProcessID) || windows.Process32Next(snap, &pe) != nil {
			return nil
		}
	}
}

// ProcessRunning reports whether a process with this exe name runs. When the
// process list can't be read it answers true, so nothing is switched off by
// mistake.
func ProcessRunning(exe string) bool {
	found := false
	err := eachProcess(func(name string, _ uint32) bool {
		found = strings.EqualFold(name, exe)
		return !found
	})
	return found || err != nil
}

// ProcessPath is the full path of a running process's exe, or "".
func ProcessPath(exe string) string {
	var path string
	_ = eachProcess(func(name string, pid uint32) bool {
		if strings.EqualFold(name, exe) {
			path = processImage(pid)
		}
		return path == ""
	})
	return path
}

func processImage(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procGetAsyncKeyState = user32.NewProc("GetAsyncKeyState")
)

var foreground struct {
	sync.Mutex
	checked time.Time
	exe     string
}

// ForegroundIs reports whether the window in front belongs to this exe
// (cached for half a second).
func ForegroundIs(exe string) bool {
	foreground.Lock()
	defer foreground.Unlock()
	if time.Since(foreground.checked) > 500*time.Millisecond {
		foreground.checked = time.Now()
		foreground.exe = foregroundExe()
	}
	return strings.EqualFold(foreground.exe, exe)
}

func foregroundExe() string {
	hwnd := windows.GetForegroundWindow()
	if hwnd == 0 {
		return ""
	}
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(hwnd, &pid); err != nil || pid == 0 {
		return ""
	}
	return filepath.Base(processImage(pid))
}

// KeyDown reports whether a keyboard key (virtual-key code) is held.
func KeyDown(vk int) bool {
	r, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return r&0x8000 != 0
}
