package dualsense

import (
	"bytes"
	"io"
	"log"
	"runtime"
	"testing"
	"time"
)

// next is the report the link queued, or nil.
func next(l *Link) []byte {
	select {
	case r := <-l.reports:
		return r
	default:
		return nil
	}
}

// TestLinkRelease: with StopClears (DS4Windows) the link sends the release
// when it opens the pad and for each stop, and forgets the motor levels on
// open. DSX's link sends nothing on open, and its stop keeps the rumble
// bits.
func TestLinkRelease(t *testing.T) {
	old := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(old)
	pad := HIDDevice{OutLen: 48}

	l := &Link{opts: LinkOptions{StopClears: true}, reports: make(chan []byte, 1), lastLeft: 200, lastRight: 50}
	l.opened(pad, 0, 0)
	if !l.Available() {
		t.Fatal("not open after opening")
	}
	if got := next(l); !bytes.Equal(got, ReleaseReport(48)) {
		t.Errorf("on open: % x, want the release", got)
	}
	if l.lastLeft != 0 || l.lastRight != 0 {
		t.Errorf("levels after the release: %d %d", l.lastLeft, l.lastRight)
	}
	l.SetRumble(200, 0)
	if got := next(l); !bytes.Equal(got, RumbleReport(48, 200, 0)) {
		t.Errorf("rumble: % x", got)
	}
	l.SetRumble(0, 0)
	if got := next(l); !bytes.Equal(got, ReleaseReport(48)) {
		t.Errorf("stop: % x, want the release", got)
	}

	d := &Link{reports: make(chan []byte, 1), lastLeft: 200, lastRight: 50}
	d.opened(pad, 0, 0)
	if got := next(d); got != nil {
		t.Errorf("DSX on open: % x", got)
	}
	if d.lastLeft != 200 || d.lastRight != 50 {
		t.Errorf("DSX's levels changed on open: %d %d", d.lastLeft, d.lastRight)
	}
	d.SetRumble(0, 0)
	if got := next(d); !bytes.Equal(got, RumbleReport(48, 0, 0)) {
		t.Errorf("DSX's stop: % x", got)
	}
}

// TestLinkWriter: writeLoop starts with the first open, so a link that
// never opens a pad runs nothing; Close ends it, and after Close nothing
// opens again.
func TestLinkWriter(t *testing.T) {
	old := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(old)
	base := runtime.NumGoroutine()
	lists := 0
	l := NewLinkFor(LinkOptions{List: func() []HIDDevice {
		lists++
		return nil
	}})
	l.Maintain()
	if lists != 1 || l.Available() {
		t.Fatalf("Maintain listed %d times, open %v", lists, l.Available())
	}
	if l.writing {
		t.Fatal("writeLoop runs before a pad opened")
	}
	l.Close() // never opened: nothing to wait for
	if !l.closed {
		t.Fatal("not closed")
	}

	l = NewLinkFor(LinkOptions{StopClears: true, List: func() []HIDDevice { return nil }})
	if !l.opened(HIDDevice{OutLen: 48}, 0, 0) {
		t.Fatal("refused to open")
	}
	l.startWriting()
	l.startWriting()
	if !l.writing {
		t.Fatal("writeLoop did not start on the first open")
	}
	l.SetRumble(200, 0) // the write fails on the fake handle; it is only logged
	l.Close()
	l.Close()
	waitGoroutines(t, base)
	if l.Available() {
		t.Fatal("open after Close")
	}
	if l.opened(HIDDevice{OutLen: 48}, 0, 0) || l.Available() {
		t.Fatal("opened after Close")
	}
	l.lastScan = time.Time{}
	l.Maintain()
	l.startWriting()
	waitGoroutines(t, base)
}

// waitGoroutines waits up to 2 s for the goroutines to be back to want.
func waitGoroutines(t *testing.T, want int) {
	t.Helper()
	n := 0
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if n = runtime.NumGoroutine(); n <= want {
			return
		}
	}
	t.Fatalf("%d goroutines, want %d", n, want)
}
