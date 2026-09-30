package platform

import (
	"fmt"
	"os"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestInstanceRunning: a held mutex counts, and so does one this process
// may not open, as a tray EDSense running as administrator holds it.
func TestInstanceRunning(t *testing.T) {
	name := fmt.Sprintf(`Local\EDSenseTest-%d-%d`, os.Getpid(), time.Now().UnixNano())
	if InstanceRunning(name) {
		t.Fatal("a mutex nobody made counts as running")
	}
	hold := func(name string, sa *windows.SecurityAttributes) {
		p, _ := windows.UTF16PtrFromString(name)
		h, err := windows.CreateMutex(sa, false, p)
		if h == 0 {
			t.Fatal(err)
		}
		t.Cleanup(func() { windows.CloseHandle(h) })
	}
	hold(name, nil)
	if !InstanceRunning(name) {
		t.Error("a held mutex does not count")
	}
	// everyone denied, as this process is for an administrator's mutex
	sd, err := windows.SecurityDescriptorFromString("D:(D;;GA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	denied := name + "-denied"
	hold(denied, &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd})
	if !InstanceRunning(denied) {
		t.Error("a mutex this process may not open does not count")
	}
}

// The answer for the window in front is kept for 2 s; another window in
// front is looked up at once.
func TestForegroundIs(t *testing.T) {
	hwnd := windows.GetForegroundWindow()
	cache := func(h windows.HWND, age time.Duration) {
		foreground.Lock()
		foreground.hwnd, foreground.checked, foreground.exe = h, time.Now().Add(-age), "cached.exe"
		foreground.Unlock()
	}
	cache(hwnd, 0)
	if !ForegroundIs("cached.exe") {
		t.Error("the same window was looked up again within 2 s")
	}
	cache(hwnd+1, 0)
	if ForegroundIs("cached.exe") {
		t.Error("another window in front kept the old answer")
	}
	cache(hwnd, 3*time.Second)
	if ForegroundIs("cached.exe") {
		t.Error("the answer was kept past 2 s")
	}
}
