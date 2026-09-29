// Package app is EDSense's main loop: it follows the game and drives the
// controller through DSX and DSX's virtual DualSense.
package app

import (
	"log"
	"os"
	"sync"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/haptics"
	"github.com/tolgahan/ed-sense/internal/hud"
)

// Status is what the tray shows.
type Status struct {
	DSXOnline    bool
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
	setup   func(dataDir string, notify func(string)) backend.Setup
	hud     *hud.Watcher // nil where the screen can't be captured

	demoRequests    chan struct{}
	profileRequests chan struct{}

	mu       sync.Mutex
	paused   bool
	notify   func(string)
	onStatus func(Status)
	status   Status
}

func New(cfgPath string, cfg *config.Config, b *backend.Backend) *App {
	synth := haptics.NewSynth()
	a := &App{
		cfgPath:         cfgPath,
		cfg:             cfg,
		out:             b.Output,
		pad:             b.Pad,
		synth:           synth,
		audio:           b.NewAudio(synth.Render),
		caps:            b.Caps,
		setup:           b.NewSetup,
		demoRequests:    make(chan struct{}, 1),
		profileRequests: make(chan struct{}, 1),
	}
	if g := hud.NewScreenGrabber(); g != nil {
		a.hud = hud.NewWatcher(g)
	}
	if st, err := os.Stat(cfgPath); err == nil {
		a.cfgMod = st.ModTime()
	}
	return a
}

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
	restart := cfg.DSXPort != a.cfg.DSXPort || cfg.JournalDir != a.cfg.JournalDir
	*a.cfg = cfg // everything holds this pointer
	log.Print("Settings reloaded")
	if restart {
		log.Print("dsx_port and journal_dir changes need a restart of EDSense")
	}
	return true
}
