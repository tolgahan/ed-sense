// Package app is EDSense's main loop: it follows the game and drives the
// controller through a backend (DSX or DS4Windows) and its virtual
// DualSense.
package app

import (
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/ds4w"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/gyro"
	"github.com/tolgahan/ed-sense/internal/haptics"
	"github.com/tolgahan/ed-sense/internal/hud"
	"github.com/tolgahan/ed-sense/internal/platform"
)

// Status is what the tray shows.
type Status struct {
	Backend      string       // the backend's name
	Kind         backend.Kind // the backend's kind; the same for a whole session
	Online       bool         // the backend answers
	EliteRunning bool
	Active       bool
	Paused       bool
	Demo         bool
	Context      string
}

type App struct {
	store     *config.Store
	cfg       *config.Config
	cfgMod    time.Time // the settings file's time when the loop last read it
	cfgUnread time.Time // its time when it last could not be read, as logged
	synth     *haptics.Synth
	hud       *hud.Watcher // nil where the screen can't be captured

	// the backend attached now, and its parts; a restart changes them on
	// the loop, so only the loop reads them
	b     *backend.Backend
	out   backend.Output
	pad   backend.Pad
	audio backend.Audio
	caps  backend.Caps
	kind  backend.Kind
	words backend.Words
	bias  string // the gyro calibration's file name
	setup func(dataDir string, notify func(string)) backend.Setup
	gyro  *gyro.Aim // nil: the backend streams no motion, or the pad has no gyro

	// what other goroutines may read of the backend and the session
	ident       atomic.Pointer[ident]
	reporter    atomic.Pointer[reporter] // the session's setup, when it reports
	calibrating atomic.Bool              // the gyro calibrates, as the last tick found

	mouse   gyro.Mouse  // where the gyro's movement goes; the golden tests record it
	front   func() bool // Elite's window is in front, for the gyro
	blocked func() bool // Windows keeps EDSense's mouse movement from Elite

	demoRequests      chan struct{}
	demoPlaying       atomic.Pointer[demoCut] // nil: no demo plays in the loop
	calibrateRequests chan struct{}
	calls             chan func(s *session) // run on the loop
	restarts          chan restartReq
	started           atomic.Bool   // Run has started
	done              chan struct{} // closed when Run has ended

	live    liveStore // for the window
	notices notices

	mu       sync.Mutex
	paused   bool
	notify   func(id int64, msg string)
	onStatus func(Status)
	status   Status
}

// ident is what the tray and the window may read of the backend attached.
type ident struct {
	kind    backend.Kind
	words   backend.Words
	hasGyro bool
}

// reporter is a session's setup that publishes what it found.
type reporter struct{ r backend.Reporter }

// New builds the App on the backend b. cfg is the loop's settings, which
// it changes as the file in store changes.
func New(store *config.Store, cfg *config.Config, b *backend.Backend) *App {
	a := &App{
		store:             store,
		cfg:               cfg,
		synth:             haptics.NewSynth(),
		demoRequests:      make(chan struct{}, 1),
		calibrateRequests: make(chan struct{}, 1),
		calls:             make(chan func(s *session), 1),
		restarts:          make(chan restartReq),
		done:              make(chan struct{}),
	}
	a.mouse = gyro.MouseFunc(platform.MoveMouse)
	a.front = func() bool { return platform.ForegroundIs(elite.GameExe) }
	a.blocked = func() bool { return platform.InputBlocked(elite.GameExe) }
	if g := hud.NewScreenGrabber(); g != nil {
		a.hud = hud.NewWatcher(g)
	}
	if st, err := os.Stat(store.Path()); err == nil {
		a.cfgMod = st.ModTime()
	}
	a.attach(b)
	return a
}

// attach makes b the backend the loop drives the controller through.
func (a *App) attach(b *backend.Backend) {
	pad := b.Pad
	if b.Caps.RumbleMutesHaptics {
		pad = rumbleGate{Pad: b.Pad, cfg: a.cfg}
	}
	a.b, a.out, a.pad, a.caps, a.kind, a.words, a.setup = b, b.Output, pad, b.Caps, b.Kind, b.Words, b.NewSetup
	a.bias = b.BiasFile
	if a.bias == "" {
		a.bias = gyro.BiasFile
	}
	a.audio = b.NewAudio(a.synth.Render)
	// EDSense aims only where it can switch the backend's own gyro mouse
	// off, or where the backend's profile leaves the gyro alone (wantGyro),
	// or both would move the mouse.
	a.gyro = nil
	if b.Caps.Gyro && b.Motion != nil {
		a.gyro = gyro.New(gyro.MouseFunc(func(dx, dy int32) bool { return a.mouse.Move(dx, dy) }))
		a.gyro.SetSettings(gyroSettings(a.cfg))
		if bias, ok := gyro.LoadBias(a.biasPath()); ok {
			a.gyro.SetBias(bias)
		}
		b.Motion.OnSample(a.gyro.Feed)
	}
	a.ident.Store(&ident{kind: b.Kind, words: b.Words, hasGyro: a.gyro != nil})
}

// detach closes the backend attached: its motion stream, the audio and
// the pad, then what it opened itself.
func (a *App) detach() {
	if a.b.Motion != nil {
		a.b.Motion.OnSample(nil)
	}
	a.closeParts()
	if a.b.Close != nil {
		a.b.Close()
	}
}

// rumbleGate keeps the motors still on a pad whose rumble would mute native
// haptics (Caps.RumbleMutesHaptics), unless haptics_mode is "rumble": the
// rumble fallback, the demo and the idle stops all pass through it.
type rumbleGate struct {
	backend.Pad
	cfg *config.Config
}

func (g rumbleGate) SetRumble(left, right uint8) {
	if g.cfg.HapticsMode != config.HapticsRumble {
		left, right = 0, 0
	}
	g.Pad.SetRumble(left, right)
}

// Backend is the kind of backend EDSense drives the controller through.
func (a *App) Backend() backend.Kind { return a.ident.Load().kind }

// Words are what the player is told about the backend.
func (a *App) Words() backend.Words { return a.ident.Load().words }

// SetupReport is what the session's setup found last (DS4Windows: the
// profile in use and its checks); nil when the setup reports nothing, or
// before its first check.
func (a *App) SetupReport() *ds4w.Report {
	if r := a.reporter.Load(); r != nil {
		return r.r.Report()
	}
	return nil
}

// CheckSetup asks the session's setup to check again at once, when it
// can (DS4Windows' profile checks); a later SetupReport has what it found.
// It never waits.
func (a *App) CheckSetup() {
	if r := a.reporter.Load(); r != nil {
		if c, ok := r.r.(interface{ CheckNow() }); ok {
			c.CheckNow()
		}
	}
}

func (a *App) SetPaused(p bool) {
	a.mu.Lock()
	a.paused = p
	a.mu.Unlock()
	if p {
		log.Print("Effects paused")
		a.note("Effects paused")
	} else {
		log.Print("Effects resumed")
		a.note("Effects resumed")
	}
}

func (a *App) Paused() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.paused
}

// RequestDemo plays the demo once.
func (a *App) RequestDemo() { request(a.demoRequests) }

// StopDemo cuts the demo playing short; with none playing it does
// nothing.
func (a *App) StopDemo() {
	if c := a.demoPlaying.Load(); c != nil {
		c.close()
	}
}

// RequestDSXProfileReset replaces DSX's "Elite Dangerous" profile with the
// bundled one, as soon as DSX is closed. The session's setup does it, on
// the loop, until the install service owns the reset.
func (a *App) RequestDSXProfileReset() {
	a.onLoop(func(s *session) {
		s.profile.RequestReset()
		s.lastProfileStep = time.Time{}
	})
}

// onLoop runs f on the loop at its next turn. A call made while another
// one waits is dropped.
func (a *App) onLoop(f func(s *session)) {
	select {
	case a.calls <- f:
	default:
	}
}

func request(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// SetNotify sets how messages reach the player (the tray: the window or a
// message box). id is the message's notice.
func (a *App) SetNotify(f func(id int64, msg string)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.notify = f
}

// Note keeps a line for the window's Activity.
func (a *App) Note(text string) { a.note(text) }

func (a *App) tell(msg string) {
	a.mu.Lock()
	f := a.notify
	a.mu.Unlock()
	id := a.notices.add(true, msg)
	if f != nil {
		f(id, msg)
	}
}

// OnStatus calls f now and whenever the status changes (from the loop's
// goroutine).
func (a *App) OnStatus(f func(Status)) {
	a.mu.Lock()
	a.onStatus = f
	st := a.status
	a.mu.Unlock()
	if f != nil {
		f(st)
	}
}

// Status is the latest status the loop published.
func (a *App) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

// Busy: EDSense drives the controller (active, and the backend answers),
// a demo plays, or the gyro calibrates.
func (a *App) Busy() bool {
	st := a.Status()
	return st.Active && st.Online || st.Demo || a.demoPlaying.Load() != nil || a.calibrating.Load()
}

func (a *App) publish(st Status) {
	a.mu.Lock()
	changed := st != a.status
	a.status = st
	f := a.onStatus
	a.mu.Unlock()
	if changed && f != nil {
		f(st)
	}
}

// outputs: what the settings leave to EDSense, of what the backend can set.
func (a *App) outputs() backend.Outputs {
	c := a.caps
	return backend.Outputs{Triggers: a.cfg.Triggers && c.Triggers, Lightbar: a.cfg.Lightbar && c.Lightbar, PlayerLEDs: a.cfg.PlayerLEDs && c.PlayerLEDs, Mic: a.cfg.MicLED && c.Mic}
}
