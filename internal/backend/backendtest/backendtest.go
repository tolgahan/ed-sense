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
	"time"

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

	// Stream is what each report carries, for the gyro; nil: a controller
	// at rest.
	Stream *dualsense.State
	report func(dualsense.State, time.Time)
	clock  uint32
}

// reportEvery: the stand-in pad reports at 200 Hz.
const reportEvery = 5 * time.Millisecond

// OnReport sets the report hook, as the real link's does.
func (p *Pad) OnReport(f func(dualsense.State, time.Time)) { p.report = f }

// Emit sends the reports of the span ending at now, as the real link's
// reading goroutine would, with the sensor clock at 3 MHz. It records
// nothing.
func (p *Pad) Emit(now time.Time, span time.Duration) {
	if !p.Open || p.report == nil {
		return
	}
	st := dualsense.State{Accel: [3]int16{0, 8192, 0}}
	if p.Stream != nil {
		st = *p.Stream
	}
	st.OK = true
	for i := int(span/reportEvery) - 1; i >= 0; i-- {
		p.clock += uint32(reportEvery.Seconds() * 3e6)
		st.Clock = p.clock
		p.report(st, now.Add(-time.Duration(i)*reportEvery))
	}
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

// Mouse stands in for the mouse EDSense's gyro moves: it adds the moves up,
// and Flush records them as one line.
type Mouse struct {
	Rec    *Recorder
	mu     sync.Mutex
	dx, dy int64
}

func (m *Mouse) Move(dx, dy int32) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dx += int64(dx)
	m.dy += int64(dy)
	return true
}

// Flush records "mouse dx dy" for the moves since the last call, if any.
func (m *Mouse) Flush() {
	m.mu.Lock()
	dx, dy := m.dx, m.dy
	m.dx, m.dy = 0, 0
	m.mu.Unlock()
	if dx != 0 || dy != 0 {
		m.Rec.Add("mouse %d %d", dx, dy)
	}
}

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
func Listen(render func(frames []int16), frames int) string {
	var e Ear
	return e.Listen(render, frames)
}

// Ear listens like Listen, and also hashes the exact samples it heard, so a
// change in levels, voices or timing shows in Sum. The synth renders the same
// samples for the same calls. Its float math may differ in the last bit from
// one machine to another (math.Exp takes an FMA path where the CPU has one),
// but that is far below one step of an int16 sample, so the hashes hold on the
// dev PC and in CI.
type Ear struct {
	sum   uint32 // FNV-1a over the actuator samples heard since the last Sum
	heard bool
}

func (e *Ear) Listen(render func(frames []int16), frames int) string {
	buf := make([]int16, frames*4)
	render(buf)
	if !e.heard {
		e.sum, e.heard = 2166136261, true
	}
	var left, right bool
	for i := 0; i < len(buf); i += 4 {
		left = left || buf[i+2] != 0
		right = right || buf[i+3] != 0
		for _, v := range buf[i+2 : i+4] {
			for _, b := range [2]byte{byte(v), byte(uint16(v) >> 8)} {
				e.sum = (e.sum ^ uint32(b)) * 16777619
			}
		}
	}
	return fmt.Sprintf("synth %s %s", sounds(left), sounds(right))
}

// Sum is "audio <hash>" of all heard since the last Sum, or "" if nothing
// was rendered.
func (e *Ear) Sum() string {
	if !e.heard {
		return ""
	}
	e.heard = false
	return fmt.Sprintf("audio %08x", e.sum)
}

func sounds(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// Setup stands in for DSX's profile installer. Its profile's gyro is
// unknown until a test sets Known.
type Setup struct {
	Rec          *Recorder
	Known, Mouse bool // GyroToMouse's answer
}

func (s *Setup) Step()                          { s.Rec.Add("setup step") }
func (s *Setup) RequestReset()                  { s.Rec.Add("setup reset") }
func (s *Setup) GyroToMouse() (yes, known bool) { return s.Mouse, s.Known }

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
