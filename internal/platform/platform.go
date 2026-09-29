// Package platform wraps the few Windows facilities the app needs:
// processes, the keyboard and mouse, dialogs, the console and Steam's
// library folders. Other systems get harmless stand-ins, so the rest of
// the code builds and tests anywhere.
package platform

import (
	"os"
	"path/filepath"
	"time"
)

// DataDir is where settings and logs live: the exe's folder when it is
// writable (a portable install), otherwise %APPDATA%\<app>.
func DataDir(app string) string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	dir := filepath.Dir(exe)
	if writable(dir) {
		return dir
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return dir
	}
	dir = filepath.Join(base, app)
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// InstanceMutex is held by the tray app while it runs.
const InstanceMutex = `Local\EDSense-single-instance`

// InputTag marks EDSense's own mouse moves (SendInput's dwExtraInfo), so Raw
// Input can tell them apart from DSX's.
const InputTag = 0x45445347 // "EDSG"

// MouseMove is one relative mouse movement seen through Raw Input.
type MouseMove struct {
	DX, DY   int32
	Injected bool // no device: SendInput (DSX's or ours), or a precision touchpad
	Ours     bool // Injected with InputTag
	At       time.Time
}
