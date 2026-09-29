package app

import (
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/bindings"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/demo"
	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
	"github.com/tolgahan/ed-sense/internal/haptics"
	"github.com/tolgahan/ed-sense/internal/hud"
	"github.com/tolgahan/ed-sense/internal/lights"
	"github.com/tolgahan/ed-sense/internal/platform"
)

// session is the state of one Run.
type session struct {
	*App
	game     *game.State
	haptics  *haptics.Engine
	lights   *lights.Renderer
	status   *elite.StatusReader
	journal  *elite.JournalTailer
	bindings *bindings.Watcher
	detector *bindings.Detector
	profile  backend.Setup

	startedAt         time.Time
	lastConfigCheck   time.Time
	lastProfileStep   time.Time
	lastStatusRequest time.Time
	lastProcessCheck  time.Time

	running     bool
	online      bool
	warnedDSX   bool
	context     string
	controllers []int
	active      bool // effects on, last tick
	lastFrame   *dsx.Frame
	lastFull    time.Time

	motionOff  bool // DSX's motion output is switched off
	lastMotion time.Time
	flightTime time.Duration // for the one-time gyro check
	gyroCheck  bool          // done

	triggersHeld [2]bool // by list
	triggerAt    time.Time
	lastHUDHit   time.Time
	target       string // the target locked, to tell a new one
}

// Run blocks until stop is closed; then it hands the controller back to
// the DSX profile.
func (a *App) Run(stop <-chan struct{}) {
	s := a.newSession()
	if a.hud != nil {
		s.setHUDPalette()
		hudStop := make(chan struct{})
		defer close(hudStop)
		go a.hud.Run(hudStop)
	}
	defer a.closeParts()

	tick := time.NewTicker(time.Duration(a.cfg.PollMs) * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			s.stop()
			return
		case <-a.profileRequests:
			s.profile.RequestReset()
			s.lastProfileStep = time.Time{}
		case <-a.demoRequests:
			s.playDemo(stop)
		case <-tick.C:
			s.tick(time.Now())
		}
	}
}

func (a *App) newSession() *session {
	journalDir := a.cfg.JournalDir
	if journalDir == "" {
		journalDir = elite.JournalDir()
	}
	if _, err := os.Stat(journalDir); err != nil {
		log.Printf("Journal folder not found: %s (set \"journal_dir\" in %s)", journalDir, a.cfgPath)
	} else {
		log.Printf("Reading %s", journalDir)
	}
	bindingsDir := a.cfg.BindingsDir
	if bindingsDir == "" {
		bindingsDir = elite.BindingsDir()
	}
	return &session{
		App:       a,
		game:      game.New(),
		haptics:   haptics.New(a.cfg),
		lights:    lights.New(a.cfg),
		status:    elite.NewStatusReader(journalDir),
		journal:   elite.NewJournalTailer(journalDir),
		bindings:  bindings.NewWatcher(bindingsDir),
		profile:   dsx.NewProfileInstaller(filepath.Join(a.dataDir(), "dsx_profile_backups"), a.tell),
		startedAt: time.Now(),
		running:   platform.ProcessRunning(elite.GameExe),
	}
}

func (a *App) dataDir() string { return filepath.Dir(a.cfgPath) }

func (s *session) tick(now time.Time) {
	s.housekeeping(now)
	s.readGame(now)
	s.maintainHaptics()
	online := s.checkDSX(now)
	s.logContext()
	controllers := s.out.Controllers()
	controllersChanged := !slices.Equal(controllers, s.controllers)
	if controllersChanged {
		s.controllers, s.lastFrame = controllers, nil
	}
	paused := s.Paused()
	inMenu := s.cfg.GyroOffInMenus && s.game.InMenu(s.cfg.GyroOffGuiFocus)
	s.applyMotion(now, motionOff(s.running, online, paused, s.cfg.GyroAim, inMenu), controllersChanged)

	active := s.running && s.game.Active() && !paused
	s.publish(Status{DSXOnline: online, EliteRunning: s.running, Active: active, Paused: paused, Context: s.context})
	s.readHUD(now, active)
	if !active {
		s.idle()
		return
	}
	s.active = true
	s.driveHaptics(now)
	s.sendFrame(now)
}

// housekeeping: settings edits, the DSX profile, DSX's controller list, the
// bindings, and whether the game runs.
func (s *session) housekeeping(now time.Time) {
	if now.Sub(s.lastConfigCheck) > 2*time.Second {
		s.lastConfigCheck = now
		if s.reloadConfig() {
			s.lastFrame = nil
			s.setHUDPalette()
		}
	}
	if now.Sub(s.lastProfileStep) > 3*time.Second && now.Sub(s.startedAt) > 3*time.Second {
		s.lastProfileStep = now
		s.profile.Step()
	}
	if now.Sub(s.lastStatusRequest) > 2*time.Second {
		s.lastStatusRequest = now
		s.out.RequestStatus()
	}
	if s.running && s.bindings.Poll(now) {
		b := s.bindings.Bindings()
		s.detector = bindings.NewDetector(b)
		s.haptics.SetBindings(b)
	}
	if now.Sub(s.lastProcessCheck) > 3*time.Second {
		s.lastProcessCheck = now
		running := platform.ProcessRunning(elite.GameExe)
		switch {
		case running && !s.running:
			log.Print("Elite Dangerous started")
			s.setHUDPalette() // the colour matrix may have changed while the game was closed
		case !running && s.running:
			log.Print("Elite Dangerous closed")
		}
		s.running = running
	}
}

func (s *session) readGame(now time.Time) {
	s.journal.Poll(func(ev elite.Event, live bool) {
		s.game.OnEvent(ev, live, now)
		if live {
			s.haptics.OnEvent(ev, s.game, now)
		}
		switch {
		case ev.Name() == "Loadout" && s.hud != nil:
			s.hud.SetLoadout(s.game.Modules)
		case ev.Name() == "ShipTargeted" && live:
			s.targetChanged(ev)
		}
	})
	if st, changed := s.status.Poll(); changed {
		if s.game.HaveStatus {
			s.haptics.OnStatus(s.game.Status, st, now)
		}
		s.game.OnStatus(st, now)
	}
}

// maintainHaptics keeps the virtual DualSense and its audio device open, and
// picks native haptics when the audio works.
func (s *session) maintainHaptics() {
	native := s.cfg.HapticsMode != config.HapticsRumble
	if s.cfg.Haptics && s.running {
		s.pad.Maintain()
		if native {
			s.audio.Maintain()
		}
	}
	s.haptics.UseSynth(s.synth, s.cfg.Haptics && native && s.audio.Active())
}

func (s *session) checkDSX(now time.Time) (online bool) {
	online = s.out.Online()
	if !online && !s.online && !s.warnedDSX && now.Sub(s.startedAt) > 5*time.Second {
		log.Print("DSX is not answering. Is DSX running, with Settings > Networking > Incoming UDP on?")
		s.warnedDSX = true
	}
	if online != s.online {
		if online {
			log.Print("DSX connected")
		} else {
			log.Print("DSX not answering (is DSX running with Incoming UDP on?)")
		}
		s.online, s.lastFrame = online, nil
	}
	return online
}

func (s *session) logContext() {
	context := "Elite not running"
	if s.running {
		context = s.game.Context()
	}
	if context != s.context {
		log.Printf("State: %s", context)
		s.context = context
	}
}

// idle: not in the game, or paused. The controller goes back to the DSX
// profile.
func (s *session) idle() {
	if s.active {
		s.out.ResetToProfile(s.controllers)
		log.Print("Controller handed back to your DSX profile")
	}
	s.active, s.lastFrame = false, nil
	s.pad.SetRumble(0, 0)
	_ = s.pad.State() // drop presses made meanwhile
	s.haptics.Silence()
	s.detector.ResetHeadlook()
	s.haptics.SetHeadlook(false)
}

func (s *session) driveHaptics(now time.Time) {
	if !s.cfg.Haptics {
		s.pad.SetRumble(0, 0)
		s.haptics.Silence()
		s.detector.ResetHeadlook()
		s.haptics.SetHeadlook(false)
		return
	}
	pad := s.pad.State()
	if pad.R2Held() || pad.L2Held() {
		s.game.FiredAt, s.triggerAt = now, now
	}
	s.triggersHeld = [2]bool{hud.Secondary: pad.OK && pad.L2Held(), hud.Primary: pad.OK && pad.R2Held()}
	var keyDown func(vk int) bool // keys count only while Elite is in front
	if eliteInFront() {
		keyDown = platform.KeyDown
	}
	st := s.game.Status
	if !st.InShip() {
		s.detector.ResetHeadlook()
	}
	s.detector.SetShipControls(st.InShip() && !st.InPanel())
	s.haptics.OnActions(s.detector.Update(pad, keyDown), s.game, now)
	s.haptics.SetHeadlook(s.detector.Headlook())
	left, right := s.haptics.Tick(now, s.game, pad)
	s.pad.SetRumble(haptics.Motor(left), haptics.Motor(right))
	if pad.OK {
		s.checkGyro()
	}
}

// checkGyro: once, after a minute of flight, whether gyro data reaches
// EDSense (the turn feel needs it).
func (s *session) checkGyro() {
	st := s.game.Status
	if !s.cfg.GyroAim || s.gyroCheck || !st.InShip() || st.Parked() || st.InPanel() {
		return
	}
	s.flightTime += time.Duration(s.cfg.PollMs) * time.Millisecond
	if s.flightTime <= time.Minute {
		return
	}
	fastest, touched := s.haptics.GyroStats()
	if fastest < 1 {
		log.Print("Gyro: no motion data from the virtual DualSense in a minute of flight. The turn feel needs DSX's Motion passthrough (Motion page, \"Passthrough\" on)")
	} else {
		log.Printf("Gyro: OK (fastest turn %.0f deg/s, touchpad %v)", fastest, touched)
	}
	s.gyroCheck = true
}

// sendFrame sends what changed, and everything every 3 s.
func (s *session) sendFrame(now time.Time) {
	frame := s.lights.Frame(s.game, now)
	prev := s.lastFrame
	if prev == nil || now.Sub(s.lastFull) > 3*time.Second {
		prev, s.lastFull = nil, now
	}
	s.out.Send(s.controllers, prev, frame, s.outputs())
	s.lastFrame = &frame
}

func (s *session) playDemo(stop <-chan struct{}) {
	log.Print("Demo started")
	s.publish(Status{DSXOnline: s.out.Online(), EliteRunning: s.running, Demo: true, Context: "demo"})
	demo.Run(s.cfg, s.demoOutput(), stop)
	s.lastFrame, s.active = nil, false
}

// PlayDemo plays the demo on its own, without following the game.
func (a *App) PlayDemo(stop <-chan struct{}) {
	defer a.closeParts()
	demo.Run(a.cfg, a.demoOutput(), stop)
	time.Sleep(200 * time.Millisecond) // let the last packets go
}

// closeParts closes the audio, then the pad, when Run or PlayDemo ends.
func (a *App) closeParts() {
	a.audio.Close()
	a.pad.Close()
}

// eliteInFront: the keyboard counts only then. Tests replace it, so a
// game in front of the developer's desktop cannot reach them.
var eliteInFront = func() bool { return platform.ForegroundIs(elite.GameExe) }

func (a *App) demoOutput() demo.Output {
	return demo.Output{Out: a.out, Outputs: a.outputs(), Pad: a.pad, Synth: a.synth, Audio: a.audio}
}

func (s *session) stop() {
	controllers := s.out.Controllers()
	if s.motionOff {
		s.out.SetMotionOff(controllers, false)
	}
	if s.active {
		s.out.ResetToProfile(controllers)
	}
	if s.active || s.motionOff {
		time.Sleep(150 * time.Millisecond) // let the packets go
	}
	log.Print("Stopped")
}
