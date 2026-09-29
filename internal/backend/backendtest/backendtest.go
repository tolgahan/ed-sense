// Package backendtest records what EDSense asks of its backend, for the
// golden tests: the packets the real DSX client sends, and the calls made to
// stand-in pad, audio and setup parts.
package backendtest

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/dualsense"
)

// Recorder collects the transcript. It is also the log's writer: a verbose
// DSX client logs each packet just before sending it, so packets and log
// lines land in the order they happened.
type Recorder struct {
	mu      sync.Mutex
	lines   []string
	partial []byte
	hide    []string // old, new pairs
}

// Hide writes as in place of s in the log lines, for paths that change
// from run to run.
func (r *Recorder) Hide(s, as string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hide = append(r.hide, s, as)
}

func (r *Recorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.partial = append(r.partial, p...)
	for {
		i := bytes.IndexByte(r.partial, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := strings.NewReplacer(r.hide...).Replace(string(r.partial[:i]))
		r.partial = r.partial[i+1:]
		if packet, ok := strings.CutPrefix(line, "-> "); ok {
			r.lines = append(r.lines, "udp "+packet)
		} else {
			r.lines = append(r.lines, "log "+line)
		}
	}
}

// Add records one line.
func (r *Recorder) Add(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, fmt.Sprintf(format, args...))
}

// Take returns the lines recorded since the last call.
func (r *Recorder) Take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.lines
	r.lines = nil
	return out
}

// DSX is the real client, verbose, sending to a local port that swallows
// everything. Online and Controllers come from the script, since the real
// ones follow DSX's answers on the wall clock.
type DSX struct {
	*dsx.Client
	Answering bool
	Slots     []int
}

func NewDSX(t testing.TB) *DSX {
	sink, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 65536)
		for {
			if _, err := sink.Read(buf); err != nil {
				return
			}
		}
	}()
	c, err := dsx.NewClient(sink.LocalAddr().(*net.UDPAddr).Port, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c.Close()
		sink.Close()
	})
	return &DSX{Client: c, Slots: []int{0}}
}

func (d *DSX) Online() bool       { return d.Answering }
func (d *DSX) Controllers() []int { return slices.Clone(d.Slots) }

// Pad stands in for the virtual DualSense.
type Pad struct {
	Rec     *Recorder
	Open    bool
	OnRead  func() // after each read, for scripts that react to them
	input   dualsense.State
	pressed dualsense.Button
}

// Hold sets what the controller reports; buttons that go down count as
// pressed, as the real link counts them.
func (p *Pad) Hold(st dualsense.State) {
	p.pressed |= st.Buttons &^ p.input.Buttons
	p.input = st
}

// Tap presses and releases b between two reads.
func (p *Pad) Tap(b dualsense.Button) { p.pressed |= b }

func (p *Pad) Maintain() { p.Rec.Add("pad maintain") }

func (p *Pad) Available() bool {
	p.Rec.Add("pad available %v", p.Open)
	return p.Open
}

func (p *Pad) State() dualsense.State {
	if !p.Open {
		p.Rec.Add("pad read, closed")
		return dualsense.State{}
	}
	st := p.input
	st.OK, st.Pressed, p.pressed = true, p.pressed, 0
	p.Rec.Add("pad read")
	if p.OnRead != nil {
		p.OnRead()
	}
	return st
}

func (p *Pad) SetRumble(left, right uint8) { p.Rec.Add("pad rumble %d %d", left, right) }
func (p *Pad) Close()                      { p.Rec.Add("pad close") }

// Audio stands in for the native haptics stream. Render is what the real
// stream would pull samples from.
type Audio struct {
	Rec    *Recorder
	Up     bool // what Active answers
	Render func(frames []int16)
}

func (a *Audio) Maintain() { a.Rec.Add("audio maintain") }

func (a *Audio) Active() bool {
	a.Rec.Add("audio active %v", a.Up)
	return a.Up
}

func (a *Audio) Close() { a.Rec.Add("audio close") }

// Listen renders frames of native haptics through render, as the audio
// thread would, and says whether each actuator sounds: "synth on off".
// Levels are left out: the synth mixes its layers in map order.
func Listen(render func(frames []int16), frames int) string {
	buf := make([]int16, frames*4)
	render(buf)
	var left, right bool
	for i := 0; i < len(buf); i += 4 {
		left = left || buf[i+2] != 0
		right = right || buf[i+3] != 0
	}
	return fmt.Sprintf("synth %s %s", sounds(left), sounds(right))
}

func sounds(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// Setup stands in for DSX's profile installer.
type Setup struct{ Rec *Recorder }

func (s *Setup) Step()         { s.Rec.Add("setup step") }
func (s *Setup) RequestReset() { s.Rec.Add("setup reset") }

// Compare checks got against the recording at path, or with update writes
// it. Carriage returns are dropped first: Git may check the recordings out
// with CRLF on Windows.
func Compare(t testing.TB, path, got string, update bool) {
	t.Helper()
	if update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to record)", err)
	}
	want := strings.ReplaceAll(string(b), "\r", "")
	if d := firstDiff(want, strings.ReplaceAll(got, "\r", "")); d != "" {
		t.Errorf("%s differs from the recording:\n%s", path, d)
	}
}

// firstDiff names the first line that differs, and the tick it is in.
func firstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	tick := "start"
	for i := range max(len(w), len(g)) {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a != b {
			return fmt.Sprintf("line %d, in %s\nwant: %s\n got: %s", i+1, tick, a, b)
		}
		if strings.HasPrefix(a, "@ ") || strings.HasPrefix(a, "# ") {
			tick = a
		}
	}
	return ""
}
