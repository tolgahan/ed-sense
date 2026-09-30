// Package app is EDSense's main loop: it follows the game and drives the
// controller through a backend (DSX or DS4Windows) and its virtual
// DualSense.
package app

import (
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/gyro"
	"github.com/tolgahan/ed-sense/internal/haptics"
	"github.com/tolgahan/ed-sense/internal/hud"
	"github.com/tolgahan/ed-sense/internal/platform"
)

// Status is what the tray shows.
type Status struct {
	Backend      string // the backend's name
	Online       bool   // the backend answers
	EliteRunning bool
	Active       bool
	Paused       bool
	Demo         bool
	Context      string
}

type App struct {
	cfgPath string
	cfg     *config.Config
	cfgMod  time.Time
	out     backend.Output
	pad     backend.Pad
	synth   *haptics.Synth
	audio   backend.Audio
	caps    backend.Caps
	kind    backend.Kind
	words   backend.Words
	bias    string // the gyro calibration's file name
	setup   func(dataDir string, notify func(string)) backend.Setup
	hud     *hud.Watcher // nil where the screen can't be captured

	// appWatch, for "auto": the controller app to use now, and whether the
	// apps that run changed since the last call. nil: not watched.
	appWatch func() (backend.Kind, string, bool)

	gyro    *gyro.Aim   // nil: the backend streams no motion, or the pad has no gyro
	mouse   gyro.Mouse  // where the gyro's movement goes; the golden tests record it
	front   func() bool // Elite's window is in front, for the gyro
	blocked func() bool // Windows keeps EDSense's mouse movement from Elite

	demoRequests      chan struct{}
	profileRequests   chan struct{}
	calibrateRequests chan struct{}

	mu       sync.Mutex
	paused   bool
	notify   func(string)
	onStatus func(Status)
	status   Status
}

func New(cfgPath string, cfg *config.Config, b *backend.Backend) *App {
	synth := haptics.NewSynth()
	pad := b.Pad
	if b.Caps.RumbleMutesHaptics {
		pad = rumbleGate{Pad: b.Pad, cfg: cfg}
	}
	a := &App{
		cfgPath:           cfgPath,
		cfg:               cfg,
		out:               b.Output,
		pad:               pad,
		synth:             synth,
		audio:             b.NewAudio(synth.Render),
		caps:              b.Caps,
		kind:              b.Kind,
		words:             b.Words,
		bias:              b.BiasFile,
		setup:             b.NewSetup,
		demoRequests:      make(chan struct{}, 1),
		profileRequests:   make(chan struct{}, 1),
		calibrateRequests: make(chan struct{}, 1),
	}
	a.mouse = gyro.MouseFunc(platform.MoveMouse)
	a.front = func() bool { return platform.ForegroundIs(elite.GameExe) }
	a.blocked = func() bool { return platform.InputBlocked(elite.GameExe) }
	// EDSense aims only where it can switch the backend's own gyro mouse
	// off, or where the backend's profile leaves the gyro alone (wantGyro),
	// or both would move the mouse.
	if b.Caps.Gyro && b.Motion != nil {
		a.gyro = gyro.New(gyro.MouseFunc(func(dx, dy int32) bool { return a.mouse.Move(dx, dy) }))
		a.gyro.SetSettings(gyroSettings(cfg))
		if bias, ok := gyro.LoadBias(a.biasPath()); ok {
			a.gyro.SetBias(bias)
		}
		b.Motion.OnSample(a.gyro.Feed)
	}
	if g := hud.NewScreenGrabber(); g != nil {
		a.hud = hud.NewWatcher(g)
	}
	if st, err := os.Stat(cfgPath); err == nil {
		a.cfgMod = st.ModTime()
	}
	if a.bias == "" {
		a.bias = gyro.BiasFile
	}
	return a
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
func (a *App) Backend() backend.Kind { return a.kind }

// Words are what the player is told about the backend.
func (a *App) Words() backend.Words { return a.words }

// WatchApps has Run follow which controller apps run, for "auto": check
// returns the app to use now, why, and whether the apps changed. It is
// asked every 3 s while EDSense is not active, off the loop's goroutine.
func (a *App) WatchApps(check func() (backend.Kind, string, bool)) { a.appWatch = check }

func (a *App) SetPaused(p bool) {
	a.mu.Lock()
	a.paused = p
	a.mu.Unlock()
	if p {
		log.Print("Effects paused")
	} else {
		log.Print("Effects resumed")
	}
}

func (a *App) Paused() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.paused
}

// RequestDemo plays the demo once.
func (a *App) RequestDemo() { request(a.demoRequests) }

// RequestDSXProfileReset replaces DSX's "Elite Dangerous" profile with the
// bundled one, as soon as DSX is closed.
func (a *App) RequestDSXProfileReset() { request(a.profileRequests) }

func request(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// SetNotify sets how messages reach the player (the tray: a message box).
func (a *App) SetNotify(f func(string)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.notify = f
}

func (a *App) tell(msg string) {
	a.mu.Lock()
	f := a.notify
	a.mu.Unlock()
	if f != nil {
		f(msg)
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

// reloadConfig picks up edits to the settings file without a restart.
func (a *App) reloadConfig() bool {
	st, err := os.Stat(a.cfgPath)
	if err != nil || !st.ModTime().After(a.cfgMod) {
		return false
	}
	a.cfgMod = st.ModTime()
	cfg, err := config.Load(a.cfgPath)
	if err != nil {
		log.Printf("Settings not reloaded: %v", err)
		return false
	}
	restart := config.RestartKeys(a.cfg, &cfg)
	*a.cfg = cfg // everything holds this pointer
	log.Print("Settings reloaded")
	if len(restart) > 0 {
		log.Printf("%s: read only at start, restart EDSense to apply", strings.Join(restart, ", "))
	}
	return true
}
