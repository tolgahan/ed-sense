package engine

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/app"
	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/ds4w"
	"github.com/tolgahan/ed-sense/internal/dsx"
)

// sys is the system as the fake Env shows it: which apps run, and who
// answers on which port.
type sys struct {
	mu       sync.Mutex
	dsx, ds4 bool
	version  string
	answers  map[int]dsx.Dialect
	probes   int
	onProbe  func() // run by each probe
}

const (
	dsxDefault = 6969
	ds4Default = 26760
)

func (s *sys) set(f func(s *sys)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s)
}

func (s *sys) probed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.probes
}

func (s *sys) env(c *config.Config) backend.DetectEnv {
	dsxPort, ds4Port := c.DSXPort, c.DS4WindowsPort
	if dsxPort == 0 {
		dsxPort = dsxDefault
	}
	if ds4Port == 0 {
		ds4Port = ds4Default
	}
	return backend.DetectEnv{
		Running: func(exe string) bool {
			s.mu.Lock()
			defer s.mu.Unlock()
			switch exe {
			case backend.DSXExe:
				return s.dsx
			case ds4w.Exe:
				return s.ds4
			}
			return false
		},
		DS4Version: func() string {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.version
		},
		Probe: func(addr *net.UDPAddr) (dsx.Dialect, bool) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.probes++
			if s.onProbe != nil {
				s.onProbe()
			}
			d, ok := s.answers[addr.Port]
			return d, ok
		},
		DSXAddr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: dsxPort},
		DS4Addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: ds4Port},
	}
}

// journal records what happens to the backends, in order.
type journal struct {
	mu    sync.Mutex
	lines []string
}

func (j *journal) add(format string, args ...any) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.lines = append(j.lines, fmt.Sprintf(format, args...))
}

func (j *journal) take() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := j.lines
	j.lines = nil
	return out
}

// builder builds backends from fake parts. Each one's output holds a
// goroutine until the backend is closed, so a backend left open shows.
type builder struct {
	j       *journal
	mu      sync.Mutex
	targets []Target
	outs    []*fakeOut
	fail    error
	hold    chan struct{} // set: a build waits for it
	holding chan struct{} // told when a build waits
}

func (f *builder) build(t Target, _ *config.Config) (*backend.Backend, error) {
	f.mu.Lock()
	hold, holding, fail := f.hold, f.holding, f.fail
	f.mu.Unlock()
	if hold != nil {
		holding <- struct{}{}
		<-hold
	}
	if fail != nil {
		return nil, fail
	}
	f.mu.Lock()
	n := len(f.targets) + 1
	f.targets = append(f.targets, t)
	addr := fmt.Sprintf("127.0.0.1:%d", t.DSXPort)
	if t.Kind == backend.KindDS4Windows {
		addr = t.DS4Addr.String()
	}
	out := newFakeOut()
	f.outs = append(f.outs, out)
	f.mu.Unlock()
	f.j.add("build %d %s %s", n, t.Kind, addr)
	parts := backend.Parts{
		Output: out,
		Pad:    &fakePad{j: f.j, n: n},
		Audio: func(func([]int16)) backend.Audio {
			return &fakeAudio{j: f.j, n: n}
		},
		Profile: func(string, func(string)) backend.Setup { return fakeSetup{} },
		Close: func() {
			out.close()
			f.j.add("close %d", n)
		},
		Addr: func() string { return addr },
	}
	if t.Kind == backend.KindDS4Windows {
		return backend.NewDS4Windows(parts), nil
	}
	return backend.NewDSX(parts), nil
}

func (f *builder) built() []Target {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.targets)
}

// open is how many built backends were never closed.
func (f *builder) open() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, o := range f.outs {
		select {
		case <-o.stop:
		default:
			n++
		}
	}
	return n
}

type fakeOut struct {
	stop chan struct{}
	once sync.Once
}

func newFakeOut() *fakeOut {
	o := &fakeOut{stop: make(chan struct{})}
	go func() { <-o.stop }()
	return o
}

func (o *fakeOut) close()                                                     { o.once.Do(func() { close(o.stop) }) }
func (o *fakeOut) Send([]int, *backend.Frame, backend.Frame, backend.Outputs) {}
func (o *fakeOut) SetMotion([]int, backend.MotionMode)                        {}
func (o *fakeOut) ResetToProfile([]int)                                       {}
func (o *fakeOut) RequestStatus()                                             {}
func (o *fakeOut) Controllers() []int                                         { return nil }
func (o *fakeOut) Online() bool                                               { return false }

type fakePad struct {
	j *journal
	n int
}

func (p *fakePad) Maintain()            {}
func (p *fakePad) Available() bool      { return false }
func (p *fakePad) State() backend.Input { return backend.Input{} }
func (p *fakePad) SetRumble(_, _ uint8) {}
func (p *fakePad) Close()               { p.j.add("pad close %d", p.n) }

type fakeAudio struct {
	j *journal
	n int
}

func (a *fakeAudio) Maintain()    {}
func (a *fakeAudio) Active() bool { return false }
func (a *fakeAudio) Close()       { a.j.add("audio close %d", a.n) }

type fakeSetup struct{}

func (fakeSetup) Step()                 {}
func (fakeSetup) RequestReset()         {}
func (fakeSetup) Gyro() backend.GyroUse { return backend.GyroUnknown }

// fakeApp stands in for the app: it attaches and closes backends as the
// app does, and answers restarts as told.
type fakeApp struct {
	j        *journal
	path     string // the settings file, read at each restart
	mu       sync.Mutex
	busy     bool
	errs     []error // the next restarts' answers, then nil
	during   func()  // run by each restart, before it answers
	calls    []string
	keys     []app.Keys // what each restart's session is built from
	on       *backend.Backend
	notes    []string
	wakes    int
	done     chan struct{}
	runStart chan struct{}
}

func newFakeApp(j *journal, path string, b *backend.Backend) *fakeApp {
	return &fakeApp{j: j, path: path, on: b, done: make(chan struct{}), runStart: make(chan struct{})}
}

func (f *fakeApp) Run(stop <-chan struct{}) {
	close(f.runStart)
	<-stop
	f.mu.Lock()
	b := f.on
	f.mu.Unlock()
	b.Pad.Close() // the parts; the backend itself is the engine's
	f.j.add("app done")
	close(f.done)
}

func (f *fakeApp) Restart(b *backend.Backend, idleOnly, adopt bool, keys *app.Keys) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case <-f.done:
		return app.ErrStopped
	default:
	}
	if f.during != nil {
		f.during()
	}
	kind := "same"
	if b != nil {
		kind = string(b.Kind)
	}
	cfg, _, _ := config.Read(f.path)
	f.calls = append(f.calls, fmt.Sprintf("restart %s idleOnly=%v adopt=%v file=%s", kind, idleOnly, adopt, cfg.Backend))
	if keys == nil {
		f.calls = append(f.calls, "restart without keys")
	} else {
		f.keys = append(f.keys, *keys)
	}
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return err
		}
	}
	if idleOnly && f.busy {
		return app.ErrBusy
	}
	if b != nil {
		old := f.on
		old.Pad.Close()
		old.Close()
		f.on = b
	}
	return nil
}

func (f *fakeApp) Busy() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.busy
}

func (f *fakeApp) WakeStatus() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wakes++
}

func (f *fakeApp) Note(text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notes = append(f.notes, text)
}

func (f *fakeApp) Done() <-chan struct{} { return f.done }

func (f *fakeApp) woken() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.wakes
}

func (f *fakeApp) set(fn func(f *fakeApp)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeApp) takeCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}

func (f *fakeApp) takeKeys() []app.Keys {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.keys
	f.keys = nil
	return out
}

func (f *fakeApp) takeNotes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.notes
	f.notes = nil
	return out
}

// logs collects the log lines.
type logs struct {
	mu    sync.Mutex
	lines []string
	dir   string
}

func (l *logs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		l.lines = append(l.lines, strings.ReplaceAll(line, l.dir, "<tmp>"))
	}
	return len(p), nil
}

func (l *logs) take() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.lines
	l.lines = nil
	return out
}

func captureLog(t *testing.T, dir string) *logs {
	l := &logs{dir: dir}
	out, flags := log.Writer(), log.Flags()
	log.SetOutput(l)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(out)
		log.SetFlags(flags)
	})
	return l
}

// rig is an engine on fake parts.
type rig struct {
	t     *testing.T
	dir   string
	path  string
	sys   *sys
	j     *journal
	bld   *builder
	store *config.Store
	loop  *config.Config // the loop's settings
	log   *logs
	e     *Engine
	a     *fakeApp
}

type rigOptions struct {
	file    string // the settings file's name; "": edsense.json
	backend string
	flag    string
	edit    func(c *config.Config)
	sys     func(s *sys)
	system  bool // the engine's own builder, with dsxWith and ds4Windows replaced
}

func newRig(t *testing.T, o rigOptions) *rig {
	t.Helper()
	r := &rig{t: t, dir: t.TempDir(), sys: &sys{answers: map[int]dsx.Dialect{}}, j: &journal{}}
	if o.file == "" {
		o.file = "edsense.json"
	}
	r.path = filepath.Join(r.dir, o.file)
	cfg := config.Default()
	cfg.Backend = o.backend
	cfg.JournalDir, cfg.BindingsDir = r.dir, r.dir
	if o.edit != nil {
		o.edit(&cfg)
	}
	if err := config.Save(r.path, cfg); err != nil {
		t.Fatal(err)
	}
	if o.sys != nil {
		o.sys(r.sys)
	}
	r.log = captureLog(t, r.dir)
	store, _, err := config.OpenStore(r.path)
	if err != nil {
		t.Fatal(err)
	}
	r.store = store
	r.bld = &builder{j: r.j}
	loop := store.Snapshot().Config.Clone()
	r.loop = &loop
	build := r.bld.build
	if o.system {
		build = nil
	}
	r.e, err = New(Options{Store: store, Cfg: r.loop, Flag: o.flag, Env: r.sys.env, Build: build})
	if err != nil {
		t.Fatal(err)
	}
	r.e.busyFor = 300 * time.Millisecond
	r.a = newFakeApp(r.j, r.path, r.e.Backend())
	r.e.attach(r.a)
	return r
}

// run runs the engine's Run until the test ends.
func (r *rig) run() {
	stop, ran := make(chan struct{}), make(chan struct{})
	go func() {
		r.e.Run(stop)
		close(ran)
	}()
	<-r.a.runStart
	r.t.Cleanup(func() {
		select {
		case <-stop:
		default:
			close(stop)
		}
		<-ran
	})
}

// set changes the settings file through the Store, as the window or the
// tray does.
func (r *rig) set(change func(c *config.Config)) {
	r.t.Helper()
	if err := r.store.Set(change, "window"); err != nil {
		r.t.Fatal(err)
	}
}

func same(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s:\n got %q\nwant %q", what, got, want)
	}
}

func (r *rig) logLine(kind, why string) []string {
	if kind == "dsx" {
		return []string{"Controller app: DSX (" + why + ")", fmt.Sprintf("DSX UDP port %d, settings <tmp>%sedsense.json", dsxDefault, string(os.PathSeparator))}
	}
	return []string{"Controller app: DS4Windows (" + why + ")",
		fmt.Sprintf("DS4Windows UDP 127.0.0.1:%d (DS4Windows' settings not found, so its default), settings <tmp>%sedsense.json", ds4Default, string(os.PathSeparator))}
}

// TestNew: the app picked at start, and the lines that say so, are main's
// as EDSense always logged them.
func TestNew(t *testing.T) {
	sep := string(os.PathSeparator)
	for _, c := range []struct {
		name   string
		o      rigOptions
		kind   backend.Kind
		lines  []string
		state  State
		dsxAdd bool
	}{
		{"dsx", rigOptions{backend: "dsx"}, backend.KindDSX,
			[]string{"Controller app: DSX (set in edsense.json)", "DSX UDP port 6969, settings <tmp>" + sep + "edsense.json"},
			State{Choice: "dsx", Kind: backend.KindDSX, Name: "DSX", Why: "set in edsense.json", Addr: "127.0.0.1:6969"}, true},
		{"ds4windows", rigOptions{backend: "ds4windows"}, backend.KindDS4Windows,
			[]string{"Controller app: DS4Windows (set in edsense.json)", "DS4Windows UDP 127.0.0.1:26760 (DS4Windows' settings not found, so its default), settings <tmp>" + sep + "edsense.json"},
			State{Choice: "ds4windows", Kind: backend.KindDS4Windows, Name: "DS4Windows", Why: "set in edsense.json", Addr: "127.0.0.1:26760"}, false},
		{"another file", rigOptions{file: "my.json", backend: "dsx"}, backend.KindDSX,
			[]string{"Controller app: DSX (set in my.json)", "DSX UDP port 6969, settings <tmp>" + sep + "my.json"},
			State{Choice: "dsx", Kind: backend.KindDSX, Name: "DSX", Why: "set in my.json", Addr: "127.0.0.1:6969"}, true},
		{"-backend", rigOptions{backend: "ds4windows", flag: "dsx"}, backend.KindDSX,
			[]string{"Controller app: DSX (-backend)", "DSX UDP port 6969, settings <tmp>" + sep + "edsense.json"},
			State{Choice: "dsx", Pinned: true, Kind: backend.KindDSX, Name: "DSX", Why: "-backend", Addr: "127.0.0.1:6969"}, true},
		{"auto, DSX runs", rigOptions{backend: "auto", sys: func(s *sys) { s.dsx = true }}, backend.KindDSX,
			[]string{"Controller app: DSX (auto: DSX runs)", "DSX UDP port 6969, settings <tmp>" + sep + "edsense.json"},
			State{Choice: "auto", Kind: backend.KindDSX, Name: "DSX", Why: "auto: DSX runs", Addr: "127.0.0.1:6969"}, true},
		{"not asked yet, neither runs", rigOptions{}, backend.KindDSX,
			[]string{"Controller app: DSX (auto: neither DSX nor DS4Windows runs yet)", "DSX UDP port 6969, settings <tmp>" + sep + "edsense.json"},
			State{Choice: "auto", Kind: backend.KindDSX, Name: "DSX", Why: "auto: neither DSX nor DS4Windows runs yet", Addr: "127.0.0.1:6969"}, false},
		{"auto, DS4Windows answers", rigOptions{backend: "auto", sys: func(s *sys) { s.answers[ds4Default] = dsx.DS4Windows }}, backend.KindDS4Windows,
			[]string{"Controller app: DS4Windows (auto: DS4Windows answers on its port)", "DS4Windows UDP 127.0.0.1:26760 (DS4Windows' settings not found, so its default), settings <tmp>" + sep + "edsense.json"},
			State{Choice: "auto", Kind: backend.KindDS4Windows, Name: "DS4Windows", Why: "auto: DS4Windows answers on its port", Addr: "127.0.0.1:26760"}, false},
		{"-backend unknown", rigOptions{backend: "dsx", flag: "xbox", sys: func(s *sys) { s.ds4 = true }}, backend.KindDS4Windows,
			[]string{`Unknown controller app "xbox", picking one`, "Controller app: DS4Windows (auto: DS4Windows runs)",
				"DS4Windows UDP 127.0.0.1:26760 (DS4Windows' settings not found, so its default), settings <tmp>" + sep + "edsense.json"},
			State{Choice: "auto", Pinned: true, Kind: backend.KindDS4Windows, Name: "DS4Windows", Why: "auto: DS4Windows runs", Addr: "127.0.0.1:26760"}, false},
		{"ports", rigOptions{backend: "ds4windows", edit: func(c *config.Config) { c.DS4WindowsPort = 7000 }}, backend.KindDS4Windows,
			[]string{"Controller app: DS4Windows (set in edsense.json)", "DS4Windows UDP 127.0.0.1:7000 (DS4Windows' settings not found, so its default), settings <tmp>" + sep + "edsense.json"},
			State{Choice: "ds4windows", Kind: backend.KindDS4Windows, Name: "DS4Windows", Why: "set in edsense.json", Addr: "127.0.0.1:7000"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, c.o)
			same(t, "log", r.log.take(), c.lines)
			if b := r.e.Backend(); b.Kind != c.kind {
				t.Errorf("backend %s", b.Kind)
			}
			if st := r.e.State(); fmt.Sprint(st) != fmt.Sprint(c.state) {
				t.Errorf("state\n got %+v\nwant %+v", st, c.state)
			}
			if r.e.dsxSure.Load() != c.dsxAdd {
				t.Errorf("DSX's profile may be added: %v", !c.dsxAdd)
			}
			if got := r.bld.built(); len(got) != 1 || got[0].DS4Port != r.store.Snapshot().Config.DS4WindowsPort {
				t.Errorf("built %+v", got)
			}
		})
	}
}

// TestNewFails: a backend that cannot be built stops EDSense, as before.
func TestNewFails(t *testing.T) {
	dir := t.TempDir()
	l := captureLog(t, dir)
	store, _, err := config.OpenStore(filepath.Join(dir, "edsense.json"))
	if err != nil {
		t.Fatal(err)
	}
	bld := &builder{j: &journal{}, fail: errors.New("no socket")}
	if _, err := New(Options{Store: store, Env: (&sys{}).env, Build: bld.build}); err == nil {
		t.Fatal("no error")
	}
	same(t, "log", l.take(), []string{"Cannot open the UDP socket: no socket"})
}

// TestApplyNothing: Apply with nothing to apply builds and restarts
// nothing; a choice that resolves to the app that runs only changes the
// choice and the file.
func TestApplyNothing(t *testing.T) {
	r := newRig(t, rigOptions{backend: "auto", sys: func(s *sys) { s.dsx = true }})
	r.log.take()
	r.set(func(c *config.Config) { c.GyroAim = false; c.DS4WindowsHaptics = config.DS4WHapticsController })
	st, err := r.e.Apply("Apply now")
	if err != nil {
		t.Fatal(err)
	}
	if st.Kind != backend.KindDSX || st.Why != "auto: DSX runs" || len(st.Pending) != 0 {
		t.Errorf("state %+v", st)
	}
	st, err = r.e.Choose("dsx", "window")
	if err != nil {
		t.Fatal(err)
	}
	if st.Choice != "dsx" || st.Why != "set in edsense.json" || st.Kind != backend.KindDSX {
		t.Errorf("chosen: %+v", st)
	}
	same(t, "restarts", r.a.takeCalls(), nil)
	same(t, "backends", r.j.take(), []string{"build 1 dsx 127.0.0.1:6969"})
	same(t, "log", r.log.take(), []string{"Controller app set to dsx"})
	if cfg, _, _ := config.Read(r.path); cfg.Backend != "dsx" {
		t.Errorf("file says %q", cfg.Backend)
	}
	if !r.e.dsxSure.Load() {
		t.Error("DSX chosen, and its profile may not be added")
	}
}

// TestApplySwitches: Apply now applies the file's restart keys: another
// app or port gets a new backend, the folders and poll_ms a new session.
func TestApplySwitches(t *testing.T) {
	r := newRig(t, rigOptions{backend: "dsx"})
	r.log.take()
	r.j.take()
	wake, stop := r.e.Watch()
	defer stop()

	r.set(func(c *config.Config) { c.Backend = "ds4windows" })
	if st := r.e.State(); !slices.Equal(st.Pending, []string{"backend"}) || st.Kind != backend.KindDSX {
		t.Errorf("before Apply: %+v", st)
	}
	st, err := r.e.Apply("Apply now")
	if err != nil {
		t.Fatal(err)
	}
	if st.Kind != backend.KindDS4Windows || st.Name != "DS4Windows" || st.Choice != "ds4windows" || len(st.Pending) != 0 || st.Switching ||
		st.Addr != "127.0.0.1:26760" {
		t.Errorf("after Apply: %+v", st)
	}
	same(t, "restarts", r.a.takeCalls(), []string{"restart ds4windows idleOnly=false adopt=true file=ds4windows"})
	same(t, "backends", r.j.take(), []string{"build 2 ds4windows 127.0.0.1:26760", "pad close 1", "close 1"})
	same(t, "log", r.log.take(), append([]string{"Controller app: switching to DS4Windows (Apply now)"}, r.logLine("ds4windows", "set in edsense.json")...))
	same(t, "notes", r.a.takeNotes(), []string{"Now using DS4Windows"})
	select {
	case <-wake:
	default:
		t.Error("Watch not woken")
	}
	if r.a.woken() == 0 {
		t.Error("the status not woken")
	}

	// poll_ms: a new session on the same backend, keeping the game
	r.set(func(c *config.Config) { c.PollMs = 40 })
	if _, err := r.e.Apply("Apply now"); err != nil {
		t.Fatal(err)
	}
	same(t, "poll_ms", r.a.takeCalls(), []string{"restart same idleOnly=false adopt=true file=ds4windows"})
	same(t, "poll_ms log", r.log.take(), []string{"Settings applied: poll_ms"})

	// journal_dir: a whole new session
	other := t.TempDir()
	r.set(func(c *config.Config) { c.JournalDir = other })
	if _, err := r.e.Apply("Apply now"); err != nil {
		t.Fatal(err)
	}
	same(t, "journal_dir", r.a.takeCalls(), []string{"restart same idleOnly=false adopt=false file=ds4windows"})
	same(t, "journal_dir log", r.log.take(), []string{"Settings applied: journal_dir"})

	// ds4windows_port: the same app on another port is a new backend
	r.set(func(c *config.Config) { c.DS4WindowsPort = 7001 })
	if st := r.e.State(); !slices.Equal(st.Pending, []string{"ds4windows_port"}) {
		t.Errorf("pending %v", st.Pending)
	}
	if st, err = r.e.Apply("Apply now"); err != nil {
		t.Fatal(err)
	}
	same(t, "port", r.a.takeCalls(), []string{"restart ds4windows idleOnly=false adopt=true file=ds4windows"})
	same(t, "port backends", r.j.take(), []string{"build 3 ds4windows 127.0.0.1:7001", "pad close 2", "close 2"})
	if st.Addr != "127.0.0.1:7001" {
		t.Errorf("addr %q", st.Addr)
	}
	r.log.take()

	// dsx_port while DS4Windows runs: nothing to apply to the backend
	r.set(func(c *config.Config) { c.DSXPort = 7002 })
	if _, err := r.e.Apply("Apply now"); err != nil {
		t.Fatal(err)
	}
	same(t, "dsx_port", r.a.takeCalls(), nil)
	if st := r.e.State(); len(st.Pending) != 0 {
		t.Errorf("pending %v", st.Pending)
	}
}

// TestApplyBroken: a settings file that does not parse applies nothing.
func TestApplyBroken(t *testing.T) {
	r := newRig(t, rigOptions{backend: "dsx"})
	if err := os.WriteFile(r.path, []byte("{\n  \"backend\": \"ds4windows\",,\n}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.store.Reload(); err == nil {
		t.Fatal("the file parses")
	}
	if _, err := r.e.Apply("Apply now"); !errors.Is(err, ErrBroken) ||
		!strings.HasPrefix(err.Error(), "the settings file has an error: edsense.json line 2, column ") {
		t.Errorf("Apply: %v", err)
	}
	if _, err := r.e.Choose("ds4windows", "tray"); !errors.Is(err, ErrBroken) {
		t.Errorf("Choose: %v", err)
	}
	// a problem without a place names the file once
	if err := os.WriteFile(r.path, []byte("\xff\xfe{\x00}\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.e.Choose("ds4windows", "tray"); !errors.Is(err, ErrBroken) ||
		err.Error() != "the settings file has an error: edsense.json is saved as UTF-16; save it as UTF-8" {
		t.Errorf("UTF-16: %v", err)
	}
	same(t, "restarts", r.a.takeCalls(), nil)
	if n := len(r.bld.built()); n != 1 {
		t.Errorf("%d backends built", n)
	}
}

// TestChooseReadsFile: a choice reads the file first, so one broken since
// the loop last read it changes nothing; a file that breaks while the
// switch runs leaves the switch made but not saved, and the error says so.
func TestChooseReadsFile(t *testing.T) {
	r := newRig(t, rigOptions{backend: "dsx"})
	r.log.take()
	r.j.take()
	good, err := os.ReadFile(r.path)
	if err != nil {
		t.Fatal(err)
	}
	broken := []byte("{\n  \"backend\": \"dsx\",,\n}")
	if err := os.WriteFile(r.path, broken, 0o644); err != nil {
		t.Fatal(err)
	}
	if r.store.Snapshot().Problem != nil {
		t.Fatal("the Snapshot has the problem already")
	}
	if _, err := r.e.Choose("ds4windows", "tray"); !errors.Is(err, ErrBroken) || !strings.Contains(err.Error(), "edsense.json line 2, column ") {
		t.Errorf("Choose: %v", err)
	}
	same(t, "restarts", r.a.takeCalls(), nil)
	same(t, "backends", r.j.take(), nil)
	same(t, "log", r.log.take(), nil)
	if st := r.e.State(); st.Kind != backend.KindDSX || st.Choice != "dsx" {
		t.Errorf("state %+v", st)
	}

	// broken while the app restarts
	if err := os.WriteFile(r.path, good, 0o644); err != nil {
		t.Fatal(err)
	}
	r.a.set(func(f *fakeApp) {
		f.during = func() {
			if err := os.WriteFile(r.path, broken, 0o644); err != nil {
				t.Error(err)
			}
		}
	})
	_, err = r.e.Choose("ds4windows", "tray")
	var ns *NotSavedError
	if !errors.As(err, &ns) || !strings.HasPrefix(err.Error(), "changed for this run only, not saved: edsense.json: ") {
		t.Errorf("Choose: %v", err)
	}
	if st := r.e.State(); st.Kind != backend.KindDS4Windows || st.Choice != "ds4windows" {
		t.Errorf("state %+v", st)
	}
	// the caller logs the error; the engine only the switch
	same(t, "log", r.log.take(), append([]string{"Controller app: switching to DS4Windows (chosen in the tray)"},
		r.logLine("ds4windows", "set in edsense.json")...))
	if b, _ := os.ReadFile(r.path); string(b) != string(broken) {
		t.Errorf("file %s", b)
	}
}

// TestChoose: a choice switches first and is saved only once the switch
// worked; a calibration is waited out for a while.
func TestChoose(t *testing.T) {
	r := newRig(t, rigOptions{backend: "dsx"})
	r.log.take()
	r.j.take()
	st, err := r.e.Choose("ds4windows", "tray")
	if err != nil {
		t.Fatal(err)
	}
	if st.Kind != backend.KindDS4Windows || st.Choice != "ds4windows" {
		t.Errorf("state %+v", st)
	}
	// the file still said dsx while the app restarted
	same(t, "restarts", r.a.takeCalls(), []string{"restart ds4windows idleOnly=false adopt=true file=dsx"})
	same(t, "log", r.log.take(), append(append([]string{"Controller app: switching to DS4Windows (chosen in the tray)"},
		r.logLine("ds4windows", "set in edsense.json")...), "Controller app set to ds4windows"))
	if cfg, _, _ := config.Read(r.path); cfg.Backend != "ds4windows" {
		t.Errorf("file says %q", cfg.Backend)
	}
	r.j.take()

	// a restart that fails: nothing saved, the new backend closed
	r.a.set(func(f *fakeApp) { f.errs = []error{app.ErrStopped} })
	if _, err := r.e.Choose("dsx", "tray"); !errors.Is(err, ErrStopped) {
		t.Errorf("stopped: %v", err)
	}
	same(t, "stopped", r.j.take(), []string{"build 3 dsx 127.0.0.1:6969", "pad close 3", "close 3"})
	if cfg, _, _ := config.Read(r.path); cfg.Backend != "ds4windows" {
		t.Errorf("file says %q", cfg.Backend)
	}
	if st := r.e.State(); st.Kind != backend.KindDS4Windows || st.Switching || st.Choice != "ds4windows" {
		t.Errorf("after a failed switch: %+v", st)
	}

	// a build that fails
	r.bld.fail = errors.New("no socket")
	r.log.take()
	if _, err := r.e.Choose("dsx", "tray"); err == nil || err.Error() != "no socket" {
		t.Errorf("build: %v", err)
	}
	same(t, "build log", r.log.take(), []string{"Controller app: switching to DSX (chosen in the tray)", "Cannot open the UDP socket: no socket"})
	r.bld.fail = nil
	if cfg, _, _ := config.Read(r.path); cfg.Backend != "ds4windows" {
		t.Errorf("file says %q", cfg.Backend)
	}

	// a calibration that ends soon
	r.a.set(func(f *fakeApp) { f.errs = []error{app.ErrBusy, app.ErrBusy} })
	r.a.takeCalls()
	if _, err := r.e.Choose("dsx", "tray"); err != nil {
		t.Fatalf("after a calibration: %v", err)
	}
	if n := len(r.a.takeCalls()); n != 3 {
		t.Errorf("%d restarts", n)
	}
	if cfg, _, _ := config.Read(r.path); cfg.Backend != "dsx" {
		t.Errorf("file says %q", cfg.Backend)
	}

	// one that goes on
	r.j.take()
	busy := make([]error, 100)
	for i := range busy {
		busy[i] = app.ErrBusy
	}
	r.a.set(func(f *fakeApp) { f.errs = busy })
	start := time.Now()
	if _, err := r.e.Choose("ds4windows", "tray"); !errors.Is(err, ErrBusy) {
		t.Errorf("calibrating: %v", err)
	}
	if d := time.Since(start); d < r.e.busyFor || d > r.e.busyFor+time.Second {
		t.Errorf("waited %v", d)
	}
	same(t, "busy", r.j.take(), []string{"build 5 ds4windows 127.0.0.1:26760", "pad close 5", "close 5"})
	if cfg, _, _ := config.Read(r.path); cfg.Backend != "dsx" {
		t.Errorf("file says %q", cfg.Backend)
	}
	r.a.set(func(f *fakeApp) { f.errs = nil })

	if _, err := r.e.Choose("xbox", "tray"); err == nil {
		t.Error("an unknown app chosen")
	}
}

// TestPinned: -backend decides the app until a choice; the file's backend
// is not pending meanwhile.
func TestPinned(t *testing.T) {
	r := newRig(t, rigOptions{backend: "ds4windows", flag: "dsx"})
	r.run()
	r.log.take()
	r.set(func(c *config.Config) { c.Backend = "auto" })
	waitFor(t, "the engine's look at the file", func() bool { return r.a.woken() > 0 })
	same(t, "the file's backend while pinned", r.log.take(), nil)
	if st := r.e.State(); !st.Pinned || len(st.Pending) != 0 || st.Choice != "dsx" {
		t.Errorf("pinned: %+v", st)
	}
	if _, err := r.e.Apply("Apply now"); err != nil {
		t.Fatal(err)
	}
	same(t, "Apply", r.a.takeCalls(), nil)
	r.set(func(c *config.Config) { c.PollMs = 40 })
	if _, err := r.e.Apply("Apply now"); err != nil {
		t.Fatal(err)
	}
	same(t, "Apply poll_ms", r.a.takeCalls(), []string{"restart same idleOnly=false adopt=true file=auto"})
	if lines := r.log.take(); !slices.Contains(lines, "Settings applied: poll_ms") {
		t.Errorf("log %q", lines)
	}

	if st, err := r.e.Choose("ds4windows", "window"); err != nil || st.Pinned || st.Kind != backend.KindDS4Windows || st.Why != "set in edsense.json" {
		t.Errorf("chosen: %+v %v", st, err)
	}
	r.log.take()
	r.set(func(c *config.Config) { c.Backend = "dsx" })
	if st := r.e.State(); !slices.Equal(st.Pending, []string{"backend"}) {
		t.Errorf("unpinned: %v", st.Pending)
	}
	waitFor(t, "the backend's line", func() bool {
		r.log.mu.Lock()
		defer r.log.mu.Unlock()
		return slices.Contains(r.log.lines, "backend: changed; they apply with Apply now in the EDSense window, or at the next start")
	})
}

// TestApplyAutoUnsure: Auto with a pick that is not sure keeps the app
// EDSense runs on.
func TestApplyAutoUnsure(t *testing.T) {
	r := newRig(t, rigOptions{backend: "auto", sys: func(s *sys) { s.ds4 = true }})
	r.log.take()
	r.j.take()
	r.sys.set(func(s *sys) { s.ds4 = false }) // closed for its profile, say
	r.set(func(c *config.Config) { c.DS4WindowsPort = 7003 })
	st, err := r.e.Apply("Apply now")
	if err != nil {
		t.Fatal(err)
	}
	if st.Kind != backend.KindDS4Windows || st.Why != "auto: neither DSX nor DS4Windows runs yet, so DS4Windows is kept" {
		t.Errorf("state %+v", st)
	}
	same(t, "backends", r.j.take(), []string{"build 2 ds4windows 127.0.0.1:7003", "pad close 1", "close 1"})
	lines := r.log.take()
	if len(lines) == 0 || lines[0] != "Controller app: DSX would be used (neither DSX nor DS4Windows runs yet), keeping DS4Windows" {
		t.Errorf("log %q", lines)
	}

	// an explicit choice, then auto again while neither runs
	r2 := newRig(t, rigOptions{backend: "ds4windows"})
	r2.log.take()
	if st, err := r2.e.Choose("auto", "window"); err != nil || st.Kind != backend.KindDS4Windows || st.Choice != "auto" {
		t.Errorf("auto: %+v %v", st, err)
	}
	same(t, "auto restarts", r2.a.takeCalls(), nil)
}

// look runs Auto's look at the apps once, as its ticker would.
func (r *rig) look(aw *autoWatch) {
	aw.check(nil)
}

// TestAuto: Auto switches only on a sure change to another app, and only
// while EDSense does not drive the controller; it probes only when the
// apps that run changed.
func TestAuto(t *testing.T) {
	r := newRig(t, rigOptions{backend: "auto"})
	r.log.take()
	r.j.take()
	aw := &autoWatch{e: r.e}

	r.look(aw)
	probes := r.sys.probed()
	r.look(aw)
	r.look(aw)
	if r.sys.probed() != probes {
		t.Errorf("probed with no change: %d, then %d", probes, r.sys.probed())
	}
	same(t, "nothing changed", r.a.takeCalls(), nil)

	// DS4Windows starts: sure, another app
	r.sys.set(func(s *sys) { s.ds4 = true })
	r.look(aw)
	same(t, "to DS4Windows", r.a.takeCalls(), []string{"restart ds4windows idleOnly=true adopt=true file=auto"})
	same(t, "to DS4Windows log", r.log.take(), append([]string{"Controller app: switching to DS4Windows (auto: DS4Windows runs)"},
		r.logLine("ds4windows", "auto: DS4Windows runs")...))
	same(t, "notes", r.a.takeNotes(), []string{"Now using DS4Windows"})
	same(t, "backends", r.j.take(), []string{"build 2 ds4windows 127.0.0.1:26760", "pad close 1", "close 1"})

	// it quits: a fallback never switches, and is told once
	r.sys.set(func(s *sys) { s.ds4 = false })
	r.look(aw)
	r.look(aw)
	same(t, "fallback", r.a.takeCalls(), nil)
	same(t, "fallback log", r.log.take(), []string{"Controller app: DSX would be used (neither DSX nor DS4Windows runs yet), keeping DS4Windows"})
	if st := r.e.State(); st.Kind != backend.KindDS4Windows {
		t.Errorf("state %+v", st)
	}

	// back, and both run with neither answering: no switch either
	r.sys.set(func(s *sys) { s.ds4, s.dsx = true, true })
	r.look(aw)
	same(t, "both", r.a.takeCalls(), nil)
	r.log.take()

	// DSX alone while EDSense drives the controller: no look at all
	r.a.set(func(f *fakeApp) { f.busy = true })
	r.sys.set(func(s *sys) { s.ds4 = false })
	probes = r.sys.probed()
	r.look(aw)
	if r.sys.probed() != probes {
		t.Error("probed while busy")
	}
	same(t, "busy", r.a.takeCalls(), nil)
	r.a.set(func(f *fakeApp) { f.busy = false })
	r.look(aw)
	same(t, "to DSX", r.a.takeCalls(), []string{"restart dsx idleOnly=true adopt=true file=auto"})
	if st := r.e.State(); st.Kind != backend.KindDSX || st.Why != "auto: DSX runs" {
		t.Errorf("state %+v", st)
	}
	if !r.e.dsxSure.Load() {
		t.Error("DSX surely runs, and its profile may not be added")
	}
	r.log.take()
	r.j.take()

	// busy as the switch is made: the backend goes, and the next idle look
	// switches
	r.a.set(func(f *fakeApp) { f.errs = []error{app.ErrBusy} })
	r.sys.set(func(s *sys) { s.dsx, s.ds4 = false, true })
	r.look(aw)
	same(t, "refused", r.j.take(), []string{"build 4 ds4windows 127.0.0.1:26760", "pad close 4", "close 4"})
	same(t, "refused log", r.log.take(), []string{"Controller app: switching to DS4Windows (auto: DS4Windows runs)",
		"Controller app: DS4Windows runs now; EDSense switches once it is not driving the controller"})
	if st := r.e.State(); st.Kind != backend.KindDSX || st.Switching {
		t.Errorf("state %+v", st)
	}
	r.look(aw)
	same(t, "retried", r.j.take(), []string{"build 5 ds4windows 127.0.0.1:26760", "pad close 3", "close 3"})
	if st := r.e.State(); st.Kind != backend.KindDS4Windows || r.e.dsxSure.Load() {
		t.Errorf("state %+v", st)
	}
	r.a.takeCalls()

	// a choice ends Auto
	if _, err := r.e.Choose("ds4windows", "tray"); err != nil {
		t.Fatal(err)
	}
	r.sys.set(func(s *sys) { s.dsx, s.ds4 = true, false })
	probes = r.sys.probed()
	r.look(aw)
	same(t, "chosen", r.a.takeCalls(), nil)
	if r.sys.probed() != probes || aw.w != nil {
		t.Error("Auto looked while an app is chosen")
	}
}

// TestAutoPinned: -backend auto is Auto too.
func TestAutoPinned(t *testing.T) {
	r := newRig(t, rigOptions{backend: "dsx", flag: "auto", sys: func(s *sys) { s.dsx = true }})
	aw := &autoWatch{e: r.e}
	r.look(aw)
	r.sys.set(func(s *sys) { s.dsx, s.ds4 = false, true })
	r.look(aw)
	same(t, "switch", r.a.takeCalls(), []string{"restart ds4windows idleOnly=true adopt=true file=dsx"})
	if st := r.e.State(); !st.Pinned || st.Kind != backend.KindDS4Windows || len(st.Pending) != 0 {
		t.Errorf("state %+v", st)
	}
}

// TestAutoRuns: Run looks at the apps on its own every few seconds (here
// faster) and stops looking once it ends.
func TestAutoRuns(t *testing.T) {
	r := newRig(t, rigOptions{backend: "auto"})
	r.e.every = 10 * time.Millisecond
	r.run()
	r.sys.set(func(s *sys) { s.ds4 = true })
	waitFor(t, "the switch", func() bool { return r.e.State().Kind == backend.KindDS4Windows })
}

// TestAutoKeepsPending: Auto's switch builds the session from the
// settings applied, so a poll_ms that waits in the file keeps waiting and
// stays pending, until Apply now applies it.
func TestAutoKeepsPending(t *testing.T) {
	r := newRig(t, rigOptions{backend: "auto"})
	aw := &autoWatch{e: r.e}
	r.look(aw)
	applied := app.KeysOf(&r.store.Snapshot().Config)
	r.set(func(c *config.Config) { c.PollMs = applied.PollMs + 25 })
	r.sys.set(func(s *sys) { s.ds4 = true })
	r.look(aw)
	same(t, "switch", r.a.takeCalls(), []string{"restart ds4windows idleOnly=true adopt=true file=auto"})
	if k := r.a.takeKeys(); len(k) != 1 || k[0] != applied {
		t.Errorf("Auto's session built from %+v, want %+v", k, applied)
	}
	if st := r.e.State(); st.Kind != backend.KindDS4Windows || !slices.Equal(st.Pending, []string{"poll_ms"}) {
		t.Errorf("state %+v", st)
	}
	if _, err := r.e.Apply("Apply now"); err != nil {
		t.Fatal(err)
	}
	if k := r.a.takeKeys(); len(k) != 1 || k[0].PollMs != applied.PollMs+25 {
		t.Errorf("Apply now's session built from %+v", k)
	}
	if st := r.e.State(); len(st.Pending) != 0 {
		t.Errorf("pending %v", st.Pending)
	}
}

// TestAutoQuitting: once EDSense quits, Auto looks at nothing; quitting
// while it probes, it builds, logs and switches nothing.
func TestAutoQuitting(t *testing.T) {
	r := newRig(t, rigOptions{backend: "auto"})
	aw := &autoWatch{e: r.e}
	r.look(aw)
	r.log.take()
	r.j.take()
	r.sys.set(func(s *sys) {
		s.dsx, s.ds4 = true, true
		s.answers[ds4Default] = dsx.DS4Windows
	})
	quit := make(chan struct{})
	close(quit)
	probes := r.sys.probed()
	aw.check(quit)
	if r.sys.probed() != probes {
		t.Error("probed after the quit")
	}

	quit = make(chan struct{})
	var once sync.Once
	r.sys.set(func(s *sys) { s.onProbe = func() { once.Do(func() { close(quit) }) } })
	aw.check(quit)
	if r.sys.probed() == probes {
		t.Fatal("no probe")
	}
	same(t, "restarts", r.a.takeCalls(), nil)
	same(t, "backends", r.j.take(), nil)
	same(t, "log", r.log.take(), nil)
	if st := r.e.State(); st.Kind != backend.KindDSX || st.Switching {
		t.Errorf("state %+v", st)
	}
}

// TestBuildSystem: the real backends are built with what the process
// shares: one set of DS4Windows warnings, one DSX profile installer whose
// first add waits until DSX is chosen or surely runs; DS4Windows is
// followed at its listener, its haptics read from the loop's settings.
func TestBuildSystem(t *testing.T) {
	oldDSX, oldDS4 := dsxWith, ds4Windows
	defer func() { dsxWith, ds4Windows = oldDSX, oldDS4 }()
	var mu sync.Mutex
	var dsxOpts []backend.DSXOptions
	var ds4Opts []backend.DS4WindowsOptions
	parts := func() backend.Parts {
		out := newFakeOut()
		return backend.Parts{Output: out, Pad: &fakePad{j: &journal{}},
			Audio:   func(func([]int16)) backend.Audio { return &fakeAudio{j: &journal{}} },
			Profile: func(string, func(string)) backend.Setup { return fakeSetup{} },
			Close:   out.close}
	}
	dsxWith = func(o backend.DSXOptions) (*backend.Backend, error) {
		mu.Lock()
		defer mu.Unlock()
		dsxOpts = append(dsxOpts, o)
		return backend.NewDSX(parts()), nil
	}
	ds4Windows = func(o backend.DS4WindowsOptions) (*backend.Backend, error) {
		mu.Lock()
		defer mu.Unlock()
		ds4Opts = append(ds4Opts, o)
		return backend.NewDS4Windows(parts()), nil
	}
	r := newRig(t, rigOptions{backend: "auto", system: true, edit: func(c *config.Config) { c.DS4WindowsPort = 7005 }})
	defer r.e.Close()
	if len(dsxOpts) != 1 || len(ds4Opts) != 0 {
		t.Fatalf("built %d DSX, %d DS4Windows", len(dsxOpts), len(ds4Opts))
	}
	first := dsxOpts[0]
	if first.Port != dsxDefault || first.Profile == nil || first.FirstAdd == nil || first.FirstAdd() {
		t.Errorf("DSX on the fallback: port %d, profile %v, or it may add its profile", first.Port, first.Profile)
	}
	for _, choice := range []string{"ds4windows", "dsx", "ds4windows", "dsx"} {
		if _, err := r.e.Choose(choice, "tray"); err != nil {
			t.Fatal(err)
		}
	}
	if len(dsxOpts) != 3 || len(ds4Opts) != 2 {
		t.Fatalf("built %d DSX, %d DS4Windows", len(dsxOpts), len(ds4Opts))
	}
	for i, o := range dsxOpts {
		if o.Profile != first.Profile || !o.FirstAdd() {
			t.Errorf("DSX %d: another profile installer, or DSX is chosen and its profile may not be added", i)
		}
	}
	for i, o := range ds4Opts {
		if o.Warned == nil || o.Warned != ds4Opts[0].Warned || !o.Follow || o.Port != 7005 ||
			o.Addr == nil || o.Addr.Port != 7005 || o.Haptics == nil {
			t.Errorf("DS4Windows %d: %+v", i, o)
			continue
		}
		r.loop.DS4WindowsHaptics = config.DS4WHapticsController
		if o.Haptics() != config.DS4WHapticsController {
			t.Errorf("DS4Windows %d: the haptics do not follow the loop's settings", i)
		}
	}
}

// TestPendingLog: a restart key that changes in the file is named once,
// from the engine's own watch, and only while it waits to be applied.
func TestPendingLog(t *testing.T) {
	r := newRig(t, rigOptions{backend: "dsx"})
	r.run()
	r.log.take()
	wake, stop := r.e.Watch()
	defer stop()
	r.set(func(c *config.Config) { c.DSXPort = 7004 })
	select {
	case <-wake:
	case <-time.After(5 * time.Second):
		t.Fatal("Watch not woken by the file")
	}
	waitFor(t, "the line", func() bool {
		r.log.mu.Lock()
		defer r.log.mu.Unlock()
		return len(r.log.lines) > 0
	})
	same(t, "log", r.log.take(), []string{"dsx_port: changed; they apply with Apply now in the EDSense window, or at the next start"})
	if st := r.e.State(); !slices.Equal(st.Pending, []string{"dsx_port"}) {
		t.Errorf("pending %v", st.Pending)
	}

	// a setting that applies at once, then a choice: nothing to name
	r.set(func(c *config.Config) { c.GyroAim = false })
	if _, err := r.e.Choose("ds4windows", "tray"); err != nil {
		t.Fatal(err)
	}
	// the choice applied dsx_port too
	if st := r.e.State(); len(st.Pending) != 0 {
		t.Errorf("pending %v", st.Pending)
	}
	// the watch takes the Snapshots in order: once a later key is named,
	// it has looked at the ones before
	r.set(func(c *config.Config) { c.DSXPort = 7005 })
	changed := func() []string {
		r.log.mu.Lock()
		defer r.log.mu.Unlock()
		return slices.DeleteFunc(slices.Clone(r.log.lines), func(l string) bool { return !strings.Contains(l, "changed;") })
	}
	waitFor(t, "the later line", func() bool { return len(changed()) > 0 })
	same(t, "named", changed(), []string{"dsx_port: changed; they apply with Apply now in the EDSense window, or at the next start"})
}

// TestDetect: who runs and who answers, probing each address once; kept
// for 2 s, a fresh one at most once a second.
func TestDetect(t *testing.T) {
	r := newRig(t, rigOptions{backend: "dsx", sys: func(s *sys) {
		s.dsx, s.ds4, s.version = true, true, "5.0.12.0"
		s.answers[ds4Default] = dsx.DS4Windows
	}})
	probes := r.sys.probed()
	d := r.e.Detect(false)
	d.T = 0
	want := Detection{
		DSX:  DSXSeen{Running: true, Addr: "127.0.0.1:6969"},
		DS4W: DS4WSeen{Running: true, Version: "5.0.12.0", Addr: "127.0.0.1:26760", Answers: "ds4windows"},
		Auto: AutoPick{Kind: backend.KindDS4Windows, Why: "both run, DS4Windows answers", Sure: true},
	}
	if d != want {
		t.Errorf("detection\n got %+v\nwant %+v", d, want)
	}
	if n := r.sys.probed() - probes; n != 2 {
		t.Errorf("%d probes", n)
	}
	probes = r.sys.probed()
	r.e.Detect(false)
	r.e.Detect(true)
	if r.sys.probed() != probes {
		t.Error("probed again within a second")
	}
	r.e.detAt = r.e.detAt.Add(-1500 * time.Millisecond)
	r.e.Detect(false)
	if r.sys.probed() != probes {
		t.Error("probed again within 2 s")
	}
	r.e.Detect(true)
	if r.sys.probed() == probes {
		t.Error("a fresh detection after a second probed nothing")
	}

	// one address for both: probed once
	r2 := newRig(t, rigOptions{backend: "dsx", edit: func(c *config.Config) { c.DS4WindowsPort = dsxDefault }, sys: func(s *sys) {
		s.answers[dsxDefault] = dsx.DSX
	}})
	probes = r2.sys.probed()
	d = r2.e.Detect(false)
	if n := r2.sys.probed() - probes; n != 1 || d.DSX.Answers != "dsx" || d.DS4W.Answers != "dsx" || d.Auto.Sure {
		t.Errorf("%d probes, %+v", n, d)
	}
}

// TestClose: Close closes the backend once, after the loop has ended or
// 1 s.
func TestClose(t *testing.T) {
	r := newRig(t, rigOptions{backend: "dsx"})
	r.j.take()
	r.e.Close()
	r.e.Close()
	same(t, "not run", r.j.take(), []string{"close 1"})

	r = newRig(t, rigOptions{backend: "dsx"})
	stop, ran := make(chan struct{}), make(chan struct{})
	go func() {
		r.e.Run(stop)
		close(ran)
	}()
	<-r.a.runStart
	r.j.take()
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(stop)
	}()
	r.e.Close()
	<-ran
	same(t, "after the loop", r.j.take(), []string{"pad close 1", "app done", "close 1"})
	if _, err := r.e.Apply("Apply now"); !errors.Is(err, ErrStopped) {
		t.Errorf("Apply after Close: %v", err)
	}

	// a loop that does not end in time
	r = newRig(t, rigOptions{backend: "dsx"})
	stop = make(chan struct{})
	defer close(stop)
	go r.e.Run(stop)
	<-r.a.runStart
	start := time.Now()
	r.e.Close()
	if d := time.Since(start); d < time.Second || d > 3*time.Second {
		t.Errorf("waited %v", d)
	}
}

// TestQuitWhileBuilding: a backend built while EDSense quits is closed.
func TestQuitWhileBuilding(t *testing.T) {
	r := newRig(t, rigOptions{backend: "dsx"})
	stop, ran := make(chan struct{}), make(chan struct{})
	go func() {
		r.e.Run(stop)
		close(ran)
	}()
	<-r.a.runStart
	r.bld.mu.Lock()
	r.bld.hold, r.bld.holding = make(chan struct{}), make(chan struct{})
	r.bld.mu.Unlock()
	errc := make(chan error)
	go func() {
		_, err := r.e.Choose("ds4windows", "tray")
		errc <- err
	}()
	<-r.bld.holding
	close(stop)
	<-ran
	r.e.Close()
	close(r.bld.hold)
	if err := <-errc; !errors.Is(err, ErrStopped) {
		t.Errorf("Choose: %v", err)
	}
	if n := r.bld.open(); n != 0 {
		t.Errorf("%d backends left open", n)
	}
	if cfg, _, _ := config.Read(r.path); cfg.Backend != "dsx" {
		t.Errorf("file says %q", cfg.Backend)
	}
}

// waitFor waits up to 5 s for ok.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); !ok(); time.Sleep(time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("no %s", what)
		}
	}
}
