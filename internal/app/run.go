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
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
	"github.com/tolgahan/ed-sense/internal/gyro"
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
	warnedDSX   bool // told that the backend does not answer
	context     string
	controllers []int
	active      bool // effects on, last tick
	lastFrame   *backend.Frame
	lastFull    time.Time

	motion     dsxMotion // what DSX's motion page was told
	lastMotion time.Time
	noneSince  time.Time     // DSX was told to pass the motion on, without its mouse
	flightTime time.Duration // for the one-time gyro check
	gyroCheck  bool          // done

	// EDSense's gyro
	hold         gyro.Hold
	seen         gyro.Status // at the last tick
	gyroNoData   bool        // no motion came while DSX passed it on: DSX's gyro aims
	eliteBlocked bool        // Elite runs as administrator
	blockChecked bool
	warnedMove   bool
	saidProfile  bool            // logged that the DSX profile's gyro is no motion to mouse
	saidUse      backend.GyroUse // what the DS4Windows profile's gyro was logged as; -1: nothing yet

	triggersHeld [2]bool // by list
	triggerAt    time.Time
	lastHUDHit   time.Time
	target       string // the target locked, to tell a new one
}

// Run blocks until stop is closed; then it hands the controller back to
// the backend's profile.
func (a *App) Run(stop <-chan struct{}) {
	s := a.newSession()
	if a.appWatch != nil {
		watchStop := make(chan struct{})
		defer close(watchStop)
		go a.watchApps(watchStop)
	}
	if a.hud != nil {
		s.setHUDPalette()
		hudStop := make(chan struct{})
		defer close(hudStop)
		go a.hud.Run(hudStop)
	}
	defer a.closeParts()
	defer a.handBackOnPanic() // runs first, before the parts close

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
	setup := backend.Setup(noSetup{})
	if a.setup != nil {
		setup = a.setup(a.dataDir(), a.tell)
	}
	return &session{
		App:       a,
		game:      game.New(),
		haptics:   haptics.New(a.cfg),
		lights:    lights.New(a.cfg),
		status:    elite.NewStatusReader(journalDir),
		journal:   elite.NewJournalTailer(journalDir),
		bindings:  bindings.NewWatcher(bindingsDir),
		profile:   setup,
		hold:      gyro.HoldStart,
		saidUse:   -1,
		startedAt: time.Now(),
		running:   platform.ProcessRunning(elite.GameExe),
	}
}

func (a *App) dataDir() string { return filepath.Dir(a.cfgPath) }

// noSetup is the setup work of a backend that has none.
type noSetup struct{}

func (noSetup) Step()                 {}
func (noSetup) RequestReset()         {}
func (noSetup) Gyro() backend.GyroUse { return backend.GyroUnknown }

// handBackOnPanic: a backend that keeps its overrides (DS4Windows) would
// keep the triggers set after a crash, so the controller is handed back
// first.
func (a *App) handBackOnPanic() {
	if r := recover(); r != nil {
		if a.caps.KeepsOverrides {
			a.out.ResetToProfile(a.out.Controllers())
			time.Sleep(150 * time.Millisecond) // let the packet go
		}
		panic(r)
	}
}

// watchApps, for "auto": every 3 s while EDSense is not active, which
// controller apps run; when that changes and another app should be used,
// the player is told once. Changing needs a restart.
func (a *App) watchApps(stop <-chan struct{}) {
	told := map[backend.Kind]bool{a.kind: true}
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		a.mu.Lock()
		active := a.status.Active || a.status.Demo
		a.mu.Unlock()
		if active {
			continue
		}
		kind, why, changed := a.appWatch()
		if !changed || told[kind] {
			continue
		}
		told[kind] = true
		log.Printf("Controller app: %s would be used now (%s); EDSense uses %s until it is restarted", kind, why, a.words.Name)
		a.tell("EDSense uses " + a.words.Name + ", but " + kindName(kind) + " is the one to use now (" + why + ").\n\n" +
			"Quit EDSense from its tray menu and start it again to use " + kindName(kind) + ".")
	}
}

// kindName is a backend kind as the player knows it.
func kindName(k backend.Kind) string {
	if k == backend.KindDS4Windows {
		return "DS4Windows"
	}
	return "DSX"
}

func (s *session) tick(now time.Time) {
	s.housekeeping(now)
	if w, ok := s.profile.(backend.GameWatcher); ok {
		w.Game(s.running, s.cfg.GyroAim && s.cfg.GyroBy == config.GyroByEDSense, s.running && s.front())
	}
	s.checkElevated()
	s.readGame(now)
	s.maintainHaptics()
	select {
	case <-s.calibrateRequests:
		s.startCalibration()
	default:
	}
	online := s.checkDSX(now)
	s.logContext()
	controllers := s.out.Controllers()
	controllersChanged := !slices.Equal(controllers, s.controllers)
	if controllersChanged {
		s.controllers, s.lastFrame = controllers, nil
	}
	paused := s.Paused()
	inMenu := s.cfg.GyroOffInMenus && s.game.InMenu(s.cfg.GyroOffGuiFocus)
	own := s.ownGyro()
	st := s.gyroStatus()
	want := motionProfile
	if s.caps.MotionOff {
		want = motionPolicy(s.running, online, paused, s.cfg.GyroAim, inMenu, own, st.Calibrating)
	}
	s.applyMotion(now, want, own, controllersChanged, st.TouchLifts != s.seen.TouchLifts)
	s.holdGyro(now, own, online, paused, inMenu, st)

	active := s.running && s.game.Active() && !paused
	s.publish(Status{Backend: s.words.Name, Online: online, EliteRunning: s.running, Active: active, Paused: paused, Context: s.context})
	s.readHUD(now, active)
	if !active {
		s.idle()
		return
	}
	s.active = true
	s.driveHaptics(now)
	s.sendFrame(now)
}

// housekeeping: settings edits, the backend's setup and controller list,
// the bindings, and whether the game runs.
func (s *session) housekeeping(now time.Time) {
	if now.Sub(s.lastConfigCheck) > 2*time.Second {
		s.lastConfigCheck = now
		by, outs := s.cfg.GyroBy, s.outputs()
		if s.reloadConfig() {
			s.handBackOutputs(outs)
			s.lastFrame = nil
			s.setHUDPalette()
			if s.gyro != nil {
				s.gyro.SetSettings(gyroSettings(s.cfg))
			}
			if s.cfg.GyroBy != by {
				s.gyroNoData = false // try EDSense's gyro again
			}
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
		if running != s.running {
			s.blockChecked, s.eliteBlocked, s.gyroNoData = false, false, false
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
// picks native haptics when the audio works. EDSense's gyro needs the pad
// open too.
func (s *session) maintainHaptics() {
	native := s.caps.Haptics && s.cfg.HapticsMode != config.HapticsRumble
	if (s.cfg.Haptics || s.wantGyro()) && s.running {
		s.pad.Maintain()
		if s.cfg.Haptics && native {
			s.audio.Maintain()
		}
	}
	s.haptics.UseSynth(s.synth, s.cfg.Haptics && native && s.audio.Active())
}

// handBackOutputs: an output switched off while EDSense drives the
// controller. A backend that keeps its overrides would keep the last
// setting, so the controller goes back to the profile, and the next frame
// sets the outputs still on.
func (s *session) handBackOutputs(before backend.Outputs) {
	after := s.outputs()
	off := before.Triggers && !after.Triggers || before.Lightbar && !after.Lightbar ||
		before.PlayerLEDs && !after.PlayerLEDs || before.Mic && !after.Mic
	if s.caps.KeepsOverrides && s.active && off {
		s.out.ResetToProfile(s.controllers)
		log.Print(s.words.OutputsOff)
	}
}

func (s *session) checkDSX(now time.Time) (online bool) {
	online = s.out.Online()
	if !online && !s.online && !s.warnedDSX && now.Sub(s.startedAt) > 5*time.Second {
		log.Print(s.words.NotAnswering)
		s.warnedDSX = true
	}
	if online != s.online {
		if online {
			log.Print(s.words.Connected)
			// what a crashed EDSense left on a backend that keeps it
			if s.caps.KeepsOverrides {
				s.out.ResetToProfile(s.out.Controllers())
			}
		} else {
			log.Print(s.words.Lost)
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

// idle: not in the game, or paused. The controller goes back to the
// backend's profile.
func (s *session) idle() {
	if s.active {
		s.out.ResetToProfile(s.controllers)
		log.Print(s.words.HandedBack)
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
	// with EDSense's gyro, DSX passes the motion on anyway, and
	// checkOwnGyro watches it
	ownAims := s.motion == motionNone
	if !s.caps.MotionOff {
		ownAims = s.hold == 0
	}
	if !s.caps.Gyro || !s.cfg.GyroAim || s.gyroCheck || ownAims || !st.InShip() || st.Parked() || st.InPanel() {
		return
	}
	s.flightTime += time.Duration(s.cfg.PollMs) * time.Millisecond
	if s.flightTime <= time.Minute {
		return
	}
	fastest, touched := s.haptics.GyroStats()
	if fastest < 1 {
		log.Print(s.words.NoMotionHint)
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
	s.publish(Status{Backend: s.words.Name, Online: s.out.Online(), EliteRunning: s.running, Demo: true, Context: "demo"})
	if s.gyro != nil {
		s.gyro.SetHold(gyro.HoldDemo)
	}
	if s.motion != motionProfile {
		s.out.SetMotion(s.controllers, backend.MotionProfile)
	}
	demo.Run(s.cfg, s.demoOutput(), stop)
	// the demo hands the gyro back to the profile; the next tick applies
	// the policy again
	s.lastFrame, s.active, s.motion = nil, false, motionProfile
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
	return demo.Output{Out: a.out, Outputs: a.outputs(), Pad: a.pad, Synth: a.synth, Audio: a.audio, Words: a.words}
}

func (s *session) stop() {
	if s.gyro != nil {
		s.gyro.SetHold(gyro.HoldStart) // before DSX's own gyro is back
	}
	controllers := s.out.Controllers()
	handBack := s.motion != motionProfile
	if handBack {
		s.out.SetMotion(controllers, backend.MotionProfile)
	}
	if s.active {
		s.out.ResetToProfile(controllers)
	}
	if s.active || handBack {
		time.Sleep(150 * time.Millisecond) // let the packets go
	}
	s.stopGyro()
	log.Print("Stopped")
}
