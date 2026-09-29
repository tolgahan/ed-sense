package platform

import (
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Starts and stops the watch without moving the mouse: Windows accepts the
// window and the registration, and a watch is cleaned up for the next one.
func TestWatchMouse(t *testing.T) {
	for range 2 {
		stop := make(chan struct{})
		if err := WatchMouse(stop, func(MouseMove) {}); err != nil {
			t.Fatal(err)
		}
		if err := WatchMouse(stop, func(MouseMove) {}); err == nil {
			t.Fatal("a second watch at the same time")
		}
		endWatch(t, stop)
	}
}

// A zero move reaches Raw Input without moving the cursor: MoveMouse's INPUT
// is accepted, and the report has no device and carries InputTag.
func TestWatchSeesOurMove(t *testing.T) {
	ours := make(chan struct{}, 1)
	stop := make(chan struct{})
	if err := watchRawMouse(stop, func(ri *rawInput, _ time.Time) {
		if ri.Header.Device == 0 && ri.Mouse.ExtraInfo == InputTag {
			select {
			case ours <- struct{}{}:
			default:
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer endWatch(t, stop)
	if !MoveMouse(0, 0) {
		t.Skip("Windows refused the input (a locked screen?)")
	}
	select {
	case <-ours:
	case <-time.After(2 * time.Second):
		t.Fatal("our move did not reach Raw Input with InputTag")
	}
}

func endWatch(t *testing.T, stop chan struct{}) {
	t.Helper()
	close(stop)
	for deadline := time.Now().Add(2 * time.Second); mouseWatching.Load(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the watch did not stop")
		}
	}
}

// A relative move counts with any other flag set (0x08 is
// MOUSE_MOVE_NOCOALESCE); positions and reports without movement do not.
func TestMouseMove(t *testing.T) {
	at := time.Now()
	const mouse = windows.Handle(0x1234)
	for _, c := range []struct {
		name      string
		device    windows.Handle
		flags     uint16
		x, y      int32
		extra     uint32
		ok        bool
		inj, ours bool
	}{
		{"a mouse", mouse, 0, 3, -2, 0, true, false, false},
		{"not coalesced", mouse, 0x08, 1, 0, 0, true, false, false},
		{"SendInput", 0, 0, -5, 0, 0, true, true, false},
		{"ours", 0, 0, 0, 4, InputTag, true, true, true},
		{"a mouse with our tag", mouse, 0, 1, 1, InputTag, true, false, false},
		{"a position", 0, 0x01, 30000, 20000, 0, false, false, false},
		{"a position on the virtual desktop", 0, 0x03, 30000, 20000, 0, false, false, false},
		{"buttons only", mouse, 0, 0, 0, 0, false, false, false},
	} {
		var ri rawInput
		ri.Header.Device = c.device
		ri.Mouse = rawMouse{Flags: c.flags, LastX: c.x, LastY: c.y, ExtraInfo: c.extra}
		mv, ok := mouseMove(&ri, at)
		if ok != c.ok {
			t.Errorf("%s: ok %v", c.name, ok)
			continue
		}
		if ok && mv != (MouseMove{DX: c.x, DY: c.y, Injected: c.inj, Ours: c.ours, At: at}) {
			t.Errorf("%s: %+v", c.name, mv)
		}
	}
}
