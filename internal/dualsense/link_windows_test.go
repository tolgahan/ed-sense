package dualsense

import (
	"bytes"
	"io"
	"log"
	"testing"
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
