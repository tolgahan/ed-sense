package ds4w

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// fakeNames are names no DS4Windows uses, so a DS4Windows that runs on the
// machine is never opened, signalled or held.
func fakeNames(t *testing.T) ClosedNames {
	id := fmt.Sprintf("EDSenseTest_%d_%s", os.Getpid(), t.Name())
	return ClosedNames{
		Exe:   id + ".exe",
		Event: id + "_event",
		IPC:   Names{ClassBlock: id + "_class", Title: id},
	}
}

// TestClosedWith: each sign of a running DS4Windows counts: its exe, the
// exe named in custom_exe_name.txt, its window, its event.
func TestClosedWith(t *testing.T) {
	n := fakeNames(t)
	if ok, why := closedWith(n, ""); !ok {
		t.Fatalf("nothing runs, yet: %s", why)
	}

	// the event, opened by EDSense for a moment only
	ev, err := windows.CreateEvent(nil, 1, 0, windows.StringToUTF16Ptr(n.Event))
	if err != nil {
		t.Fatal(err)
	}
	if ok, why := closedWith(n, ""); ok || why != "it runs" {
		t.Errorf("event: %v %q", ok, why)
	}
	if st, _ := windows.WaitForSingleObject(ev, 0); st != uint32(windows.WAIT_TIMEOUT) {
		t.Error("the event was set")
	}
	windows.CloseHandle(ev)
	if ok, why := closedWith(n, ""); !ok {
		t.Errorf("event closed: %s", why)
	}

	// the exe, by its own name: this test's
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	own := n
	own.Exe = filepath.Base(self)
	if ok, why := closedWith(own, ""); ok || !strings.HasSuffix(why, " runs") {
		t.Errorf("exe: %v %q", ok, why)
	}

	// a renamed exe, named next to it
	exeDir := t.TempDir()
	write(t, filepath.Join(exeDir, customExeFile), strings.TrimSuffix(filepath.Base(self), ".exe")+"\r\n")
	if ok, why := closedWith(n, exeDir); ok || !strings.Contains(why, filepath.Base(self)) {
		t.Errorf("custom exe: %v %q", ok, why)
	}
	if ok, _ := closedWith(n, t.TempDir()); !ok {
		t.Error("a folder without custom_exe_name.txt")
	}

	// the window
	f := startFake(t, func(string) (string, bool) { return "", true })
	win := n
	win.IPC = f.names
	if ok, why := closedWith(win, ""); ok || why != "its window is open" {
		t.Errorf("window: %v %q", ok, why)
	}
}
