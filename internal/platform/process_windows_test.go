package platform

import (
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

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
