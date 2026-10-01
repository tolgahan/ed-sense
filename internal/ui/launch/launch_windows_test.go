package launch

import (
	"fmt"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// TestNextStartEndsLastJob: what a window leaves behind in its job is
// gone once the next window starts, long before the job's timer.
func TestNextStartEndsLastJob(t *testing.T) {
	core := newFakeCore()
	w, l, _ := testWindow(t, "orphan", core)
	w.Open()
	eventually(t, "the window's exit", func() bool { return grandchild(l) > 0 && !w.Running() })
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(grandchild(l)))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	if ev, _ := windows.WaitForSingleObject(h, 0); ev == windows.WAIT_OBJECT_0 {
		t.Fatal("the process left behind ended by itself")
	}

	t.Setenv("EDSENSE_FAKE_WINDOW", "normal")
	w.Open()
	if ev, _ := windows.WaitForSingleObject(h, 200); ev != windows.WAIT_OBJECT_0 {
		t.Errorf("the last run's process still runs after the next start:\n%s", l)
	}
	eventually(t, "the page", func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.cur != nil && w.cur.up.Load()
	})
	start := time.Now()
	w.Close(5 * time.Second)
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("Close took %v", d)
	}
}

// TestListenForOpen: a second start reaches the listener, and after stop
// finds no EDSense to signal. Its own event name keeps a running EDSense
// out of it.
func TestListenForOpen(t *testing.T) {
	old := openEvent
	openEvent = fmt.Sprintf(`Local\EDSense-open-test-%d`, os.Getpid())
	t.Cleanup(func() { openEvent = old })
	if SignalRunning() {
		t.Fatal("signalled with no one listening")
	}
	opened := make(chan struct{}, 4)
	stop, err := ListenForOpen(func() { opened <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	if !SignalRunning() {
		t.Fatal("could not signal the listener")
	}
	select {
	case <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("the signal did not open")
	}
	stop()
	stop() // twice is fine
	if SignalRunning() {
		t.Error("signalled after stop")
	}
	select {
	case <-opened:
		t.Error("opened after stop")
	case <-time.After(100 * time.Millisecond):
	}
}
