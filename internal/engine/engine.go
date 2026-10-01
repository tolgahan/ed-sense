// Package engine chooses the controller app EDSense drives the controller
// through, DSX or DS4Windows, builds its backend, and switches it while
// EDSense runs: Apply now, a choice in the tray or the window, and Auto
// following the app that runs while EDSense is not driving the controller.
// It never runs on the loop: the app's Restart does the loop's part.
package engine

import (
	"errors"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tolgahan/ed-sense/internal/app"
	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/ds4w"
	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/platform"
	"github.com/tolgahan/ed-sense/internal/wakeup"
)

// Target is what a choice resolves to: the app, why, and where it listens.
type Target struct {
	Kind    backend.Kind
	Why     string       // as the log says it: "set in edsense.json", "-backend", "auto: DSX runs"
	Sure    bool         // auto: the app runs or answered the probe; a choice always is
	DSXPort int          // DSX's port
	DS4Addr *net.UDPAddr // DS4Windows' DSX listener
	DS4Dir  string       // the DS4Windows data folder that gave DS4Addr; "": its default
	DS4Port int          // ds4windows_port: 0 reads it from DS4Windows' settings
}

// State is what the engine runs now, for the tray and the window.
type State struct {
	Choice    string // "auto", "dsx" or "ds4windows"
	Pinned    bool   // -backend decides this run
	Kind      backend.Kind
	Name, Why string
	Addr      string // where the triggers and lights go now
	Switching bool
	Pending   []string // restart keys changed in the file since they were applied
}

// Options set up the engine.
type Options struct {
	Store *config.Store
	// Cfg is the loop's settings, read only by closures that run on the
	// loop (ds4windows_haptics); nil: none.
	Cfg     *config.Config
	Flag    string // -backend: the choice for this run, not saved
	Verbose bool   // log every packet
	// Env is how the apps are seen with the settings c; nil: the system's.
	Env func(c *config.Config) backend.DetectEnv
	// Build builds the backend for t; nil: the real DSX or DS4Windows one.
	Build func(t Target, cfg *config.Config) (*backend.Backend, error)
}

var (
	// ErrBusy: the gyro calibrates (or, for Auto, EDSense drives the
	// controller). Nothing changed.
	ErrBusy = app.ErrBusy
	// ErrStopped: EDSense quits, or its loop does not run. Nothing changed.
	ErrStopped = app.ErrStopped
	// ErrBroken: the settings file does not parse, or could not be read.
	// Nothing changed.
	ErrBroken = errors.New("the settings file has an error")
)

// NotSavedError is Choose's error when the choice works now but the
// settings file could not be written: the next start uses what the file
// holds.
type NotSavedError struct{ Err error }

func (e *NotSavedError) Error() string {
	return "changed for this run only, not saved: " + e.Err.Error()
}
func (e *NotSavedError) Unwrap() error { return e.Err }

// the real backends; a test looks at what buildSystem asks of them
var (
	dsxWith    = backend.DSXWith
	ds4Windows = backend.DS4Windows
)

// appCore is what the engine asks of the app; *app.App is it.
type appCore interface {
	Run(stop <-chan struct{})
	Restart(b *backend.Backend, idleOnly, adopt bool, keys *app.Keys) error
	Busy() bool
	WakeStatus()
	Note(text string)
	Done() <-chan struct{}
}

var _ appCore = (*app.App)(nil)

// Engine owns the controller app choice and the backend it runs on.
type Engine struct {
	o       Options
	store   *config.Store
	build   func(Target, *config.Config) (*backend.Backend, error)
	warned  *ds4w.Once          // DS4Windows' one-time warnings, for every backend of the process
	profile *backend.DSXProfile // DSX's profile installer, for every backend of the process
	every   time.Duration       // how often Auto looks at the apps
	busyFor time.Duration       // how long Apply and Choose wait out a calibration

	op sync.Mutex // one Apply, Choose or Auto switch at a time

	mu        sync.Mutex
	a         appCore
	first     *backend.Backend
	cur       *backend.Backend // the one the app runs on
	target    Target           // what cur was built for
	applied   config.Config    // the settings cur and the session were built from
	choice    string           // as it works now: auto, dsx or ds4windows
	pinned    bool
	flag      string // the -backend choice
	why       string
	switching bool
	running   bool // Run started the app's loop
	closed    bool

	dsxSure atomic.Bool // DSX is chosen or surely runs, so its profile may be added
	w       wakeup.Group

	dmu   sync.Mutex // one detection at a time
	det   Detection
	detAt time.Time

	closeOnce sync.Once
}

// New picks the controller app from the settings and -backend, builds its
// backend and says so, as EDSense always did at start.
func New(o Options) (*Engine, error) {
	e := &Engine{o: o, store: o.Store, build: o.Build, warned: &ds4w.Once{}, profile: &backend.DSXProfile{},
		every: 3 * time.Second, busyFor: 3 * time.Second}
	if e.build == nil {
		e.build = e.buildSystem
	}
	cfg := o.Store.Snapshot().Config.Clone()
	e.choice = cfg.BackendChoice()
	if o.Flag != "" {
		e.pinned, e.flag = true, o.Flag
		if !known(o.Flag) {
			log.Printf("Unknown controller app %q, picking one", o.Flag)
			e.flag = config.BackendAuto
		}
		e.choice = e.flag
	}
	t := e.resolve(&cfg, e.choice, e.pinned)
	b, err := e.build(t, o.Cfg)
	if err != nil {
		log.Printf("Cannot open the UDP socket: %v", err)
		return nil, err
	}
	e.first, e.cur, e.target, e.applied, e.why = b, b, t, cfg, t.Why
	e.dsxSure.Store(t.Kind == backend.KindDSX && t.Sure)
	e.logBackend(b, t)
	return e, nil
}

// known: choice is one of the backend settings.
func known(choice string) bool {
	switch choice {
	case config.BackendAuto, config.BackendDSX, config.BackendDS4Windows:
		return true
	}
	return false
}

// Backend is the backend New built: for app.New, and the command-line
// modes that use it directly.
func (e *Engine) Backend() *backend.Backend { return e.first }

// Attach gives the engine the app that runs on its backend.
func (e *Engine) Attach(a *app.App) { e.attach(a) }

func (e *Engine) attach(a appCore) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.a = a
}

// Run runs the app until stop is closed, with Auto watching the apps.
func (e *Engine) Run(stop <-chan struct{}) {
	e.mu.Lock()
	a := e.a
	e.running = a != nil
	e.mu.Unlock()
	if a == nil {
		<-stop
		return
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() { e.watchApps(stop) })
	wg.Go(func() { e.watchStore(done) })
	a.Run(stop)
	close(done)
	wg.Wait()
}

// State is what runs now.
func (e *Engine) State() State {
	snap := e.store.Snapshot()
	e.mu.Lock()
	defer e.mu.Unlock()
	st := State{Choice: e.choice, Pinned: e.pinned, Kind: e.cur.Kind, Name: e.cur.Name, Why: e.why,
		Switching: e.switching, Pending: e.pending(&snap.Config)}
	if e.cur.Addr != nil {
		st.Addr = e.cur.Addr()
	}
	return st
}

// pending: the restart keys in cfg that differ from what was applied;
// the backend is left out while -backend decides it. Under e.mu.
func (e *Engine) pending(cfg *config.Config) []string {
	keys := config.RestartKeys(&e.applied, cfg)
	if e.pinned {
		keys = slices.DeleteFunc(keys, func(k string) bool { return k == "backend" })
	}
	return keys
}

// Watch wakes the returned channel whenever State may have changed, until
// stop is called.
func (e *Engine) Watch() (wake <-chan struct{}, stop func()) { return e.w.Add() }

// changed wakes the engine's watchers and the app's status watchers.
func (e *Engine) changed() {
	e.w.Wake()
	e.mu.Lock()
	a := e.a
	e.mu.Unlock()
	if a != nil {
		a.WakeStatus()
	}
}

// Apply applies the restart keys as the file holds them now ("Apply now"):
// a new backend when the app or its address changed, else a new session
// when journal_dir, bindings_dir or poll_ms did. why names who asked, for
// the log.
func (e *Engine) Apply(why string) (State, error) {
	e.op.Lock()
	defer e.op.Unlock()
	err := e.apply(why, "")
	return e.State(), err
}

// Choose makes choice ("auto", "dsx" or "ds4windows") the controller app
// at once, and then saves it in the settings file. A choice ends -backend's
// say. from names who chose: "tray", "window". A switch that worked but
// could not be saved gives a *NotSavedError. The caller logs an error.
func (e *Engine) Choose(choice, from string) (State, error) {
	if !known(choice) {
		return e.State(), fmt.Errorf("unknown controller app %q", choice)
	}
	e.op.Lock()
	defer e.op.Unlock()
	// the file as it is now, which the Snapshot may not show yet: a choice
	// that cannot be saved is not made
	if fe, err := e.store.Check(); err != nil {
		if fe != nil {
			return e.State(), e.broken(fe)
		}
		return e.State(), err
	}
	if err := e.apply("chosen in the "+from, choice); err != nil {
		return e.State(), err
	}
	if err := e.store.Set(func(c *config.Config) { c.Backend = choice }, from); err != nil {
		return e.State(), &NotSavedError{err}
	}
	log.Printf("Controller app set to %s", choice)
	return e.State(), nil
}

// apply resolves the settings file's restart keys, with choice in place of
// its backend when it is not "", and switches to them. Under e.op.
func (e *Engine) apply(reason, choice string) error {
	snap := e.store.Snapshot()
	if snap.Problem != nil {
		return e.broken(snap.Problem)
	}
	cfg := snap.Config.Clone()
	e.mu.Lock()
	a, cur, curT, applied, pinned, closed := e.a, e.cur, e.target, e.applied, e.pinned, e.closed
	use := cfg.BackendChoice()
	if pinned {
		use = e.flag
	}
	e.mu.Unlock()
	if closed || a == nil {
		return ErrStopped
	}
	if choice != "" {
		cfg.Backend, use, pinned = choice, choice, false
	}
	t := e.resolve(&cfg, use, pinned)
	if !t.Sure && t.Kind != cur.Kind {
		// a fallback never moves EDSense off the app it runs on
		log.Printf("Controller app: %s would be used (%s), keeping %s", nameOf(t.Kind), strings.TrimPrefix(t.Why, "auto: "), cur.Name)
		t.Kind, t.Why = cur.Kind, fmt.Sprintf("%s, so %s is kept", t.Why, cur.Name)
	}
	same := sameBackend(t, curT, cur)
	session := cfg.JournalDir != applied.JournalDir || cfg.BindingsDir != applied.BindingsDir || cfg.PollMs != applied.PollMs
	if same && !session {
		e.mu.Lock()
		e.applied, e.choice, e.pinned, e.why = cfg, use, pinned, t.Why
		e.mu.Unlock()
		e.dsxSure.Store(t.Kind == backend.KindDSX && t.Sure)
		e.changed()
		return nil
	}
	var b *backend.Backend
	if !same {
		var err error
		if b, err = e.open(t, reason); err != nil {
			return err
		}
	}
	adopt := cfg.JournalDir == applied.JournalDir && cfg.BindingsDir == applied.BindingsDir
	next := app.KeysOf(&cfg)
	if err := e.restart(a, b, adopt, &next); err != nil {
		e.drop(b)
		return err
	}
	keys := config.RestartKeys(&applied, &cfg)
	if pinned { // -backend decides it, whatever the file says
		keys = slices.DeleteFunc(keys, func(k string) bool { return k == "backend" })
	}
	e.took(a, b, t, cfg, use, pinned, keys)
	return nil
}

// broken is ErrBroken with where the file breaks.
func (e *Engine) broken(p *config.FileError) error {
	if p.Line == 0 { // UTF-16, or not read: the message names the file
		return fmt.Errorf("%w: %s", ErrBroken, p.Msg)
	}
	return fmt.Errorf("%w: %s line %d, column %d: %s", ErrBroken, filepath.Base(e.store.Path()), p.Line, p.Col, p.Msg)
}

// open builds the backend for t, saying the switch starts. Under e.op.
func (e *Engine) open(t Target, reason string) (*backend.Backend, error) {
	log.Printf("Controller app: switching to %s (%s)", nameOf(t.Kind), reason)
	e.setSwitching(true)
	b, err := e.build(t, e.o.Cfg)
	if err != nil {
		log.Printf("Cannot open the UDP socket: %v", err)
		e.setSwitching(false)
		return nil, err
	}
	e.mu.Lock()
	closed := e.closed
	e.mu.Unlock()
	if closed { // EDSense quit while it was built
		e.drop(b)
		return nil, ErrStopped
	}
	return b, nil
}

// drop closes b, which the app never attached, and ends the switch.
func (e *Engine) drop(b *backend.Backend) {
	if b == nil {
		return
	}
	b.Discard()
	e.setSwitching(false)
}

func (e *Engine) setSwitching(on bool) {
	e.mu.Lock()
	e.switching = on
	e.mu.Unlock()
	e.changed()
}

// restart has the app restart on b (nil: the same backend), building the
// next session from keys and waiting out a calibration for up to busyFor.
func (e *Engine) restart(a appCore, b *backend.Backend, adopt bool, keys *app.Keys) error {
	end := time.Now().Add(e.busyFor)
	for {
		err := a.Restart(b, false, adopt, keys)
		if !errors.Is(err, app.ErrBusy) || time.Now().After(end) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// took makes b (nil: the same backend) and t what runs now, once the app
// restarted on them, and says so. keys are the restart keys applied.
func (e *Engine) took(a appCore, b *backend.Backend, t Target, cfg config.Config, choice string, pinned bool, keys []string) {
	e.mu.Lock()
	closed := e.closed
	if b != nil {
		e.cur, e.target = b, t
	}
	e.applied, e.choice, e.pinned, e.why, e.switching = cfg, choice, pinned, t.Why, false
	e.mu.Unlock()
	e.dsxSure.Store(t.Kind == backend.KindDSX && t.Sure)
	if b != nil {
		if closed && b.Close != nil { // Close came while the app restarted: b is the last one
			b.Close()
		}
		e.logBackend(b, t)
		a.Note("Now using " + b.Name)
	} else {
		log.Printf("Settings applied: %s", strings.Join(keys, ", "))
	}
	e.changed()
}

// Close closes the backend EDSense runs on, once its loop has ended (up to
// 1 s, so the controller is handed back first).
func (e *Engine) Close() {
	e.closeOnce.Do(func() {
		e.mu.Lock()
		a, running := e.a, e.running
		e.mu.Unlock()
		if running {
			select {
			case <-a.Done():
			case <-time.After(time.Second):
			}
		}
		e.mu.Lock()
		e.closed = true
		b := e.cur
		e.mu.Unlock()
		if b.Close != nil {
			b.Close()
		}
	})
}

// resolve is what choice resolves to with the settings cfg: the app and
// why, as EDSense always logged it.
func (e *Engine) resolve(cfg *config.Config, choice string, pinned bool) Target {
	env, at := e.where(cfg)
	t := Target{Kind: backend.Kind(choice), Why: "set in " + filepath.Base(e.store.Path()), Sure: true,
		DSXPort: at.dsxPort, DS4Addr: at.ds4Addr, DS4Dir: at.ds4Dir, DS4Port: cfg.DS4WindowsPort}
	if pinned {
		t.Why = "-backend"
	}
	switch t.Kind {
	case backend.KindDSX, backend.KindDS4Windows:
	default:
		var why string
		t.Kind, why, t.Sure = backend.DetectSure(env)
		t.Why = "auto: " + why
	}
	return t
}

// place is where the apps listen.
type place struct {
	dsxPort int
	ds4Addr *net.UDPAddr
	ds4Dir  string
}

// where is how the apps are seen with the settings cfg, and where they
// listen. It reads DSX's port file and DS4Windows' settings: off the loop.
func (e *Engine) where(cfg *config.Config) (backend.DetectEnv, place) {
	if e.o.Env != nil {
		env := e.o.Env(cfg)
		return env, place{dsxPort: env.DSXAddr.Port, ds4Addr: env.DS4Addr}
	}
	port := dsx.Port(cfg.DSXPort)
	addr, dir := backend.DS4WindowsListener(cfg.DS4WindowsPort)
	return backend.DetectEnv{
		Running:    platform.ProcessRunning,
		DS4Window:  backend.DS4WindowsRunning,
		DS4Version: backend.DS4WindowsVersion,
		Probe:      func(addr *net.UDPAddr) (dsx.Dialect, bool) { return dsx.Probe(addr, backend.ProbeTimeout) },
		DSXAddr:    &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port},
		DS4Addr:    addr,
	}, place{port, addr, dir}
}

// buildSystem builds the real backend for t.
func (e *Engine) buildSystem(t Target, cfg *config.Config) (*backend.Backend, error) {
	if t.Kind == backend.KindDS4Windows {
		var haptics func() string
		if cfg != nil {
			haptics = func() string { return cfg.DS4WindowsHaptics }
		}
		return ds4Windows(backend.DS4WindowsOptions{Addr: t.DS4Addr, Port: t.DS4Port, Follow: true,
			Verbose: e.o.Verbose, Haptics: haptics, Warned: e.warned})
	}
	return dsxWith(backend.DSXOptions{Port: t.DSXPort, Verbose: e.o.Verbose, FirstAdd: e.dsxSure.Load, Profile: e.profile})
}

// logBackend says what b is and where it sends, as EDSense always did at
// start.
func (e *Engine) logBackend(b *backend.Backend, t Target) {
	log.Printf("Controller app: %s (%s)", b.Name, t.Why)
	if t.Kind == backend.KindDS4Windows {
		log.Printf("DS4Windows UDP %s (%s), settings %s", t.DS4Addr, backend.DS4WindowsSettingsFrom(t.DS4Dir), e.store.Path())
	} else {
		log.Printf("DSX UDP port %d, settings %s", t.DSXPort, e.store.Path())
	}
}

// sameBackend: t is the app cur runs, at the same address.
func sameBackend(t, curT Target, cur *backend.Backend) bool {
	if t.Kind != cur.Kind {
		return false
	}
	if t.Kind != backend.KindDS4Windows {
		return t.DSXPort == curT.DSXPort
	}
	if t.DS4Port != curT.DS4Port {
		return false
	}
	// the backend follows DS4Windows' listener by itself
	return sameAddr(t.DS4Addr, curT.DS4Addr) || t.DS4Addr != nil && cur.Addr != nil && cur.Addr() == t.DS4Addr.String()
}

func sameAddr(a, b *net.UDPAddr) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.String() == b.String()
}

// nameOf is the app's name, as the backend for kind calls itself.
func nameOf(kind backend.Kind) string { return backend.WordsFor(kind).Name }
