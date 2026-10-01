package app

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"
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

// session is the loop's state from Run's start, or a restart, to the next
// restart or the end. adopt lists what the next session may keep of it.
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
	pollMs   int // the ticker's period: poll_ms as the session was built

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
	nativeOn    bool // native haptics play, this tick
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

	// DS4Windows' motion sources
	ms        backend.MotionState // where the motion comes from, as this tick found
	padAim    time.Duration       // the gyro aimed on the virtual DualSense while the UDP server was silent
	otherFor  time.Duration       // the gyro aimed while the UDP server answered without this controller
	noDataFor time.Duration       // the gyro aimed in a row while the UDP server answered with this controller and sent no motion

	frontSince time.Time // Elite has run in front since then; zero: it does not

	triggersHeld [2]bool // by list
	triggerAt    time.Time
	lastHUDHit   time.Time
	target       string // the target locked, to tell a new one
}

// Run blocks until stop is closed; then it hands the controller back to
// the backend's profile. A restart (Restart) ends one session and goes on
// with the next.
func (a *App) Run(stop <-chan struct{}) {
	defer close(a.done)
	a.started.Store(true)
	s := a.newSession()
	if a.hud != nil {
		s.setHUDPalette()
		hudStop := make(chan struct{})
		defer close(hudStop)
		go a.hud.Run(hudStop)
	}
	defer a.closeParts()
	defer a.handBackOnPanic() // runs first, before the parts close
	for s != nil {
		s = s.loop(stop)
	}
}

// loop runs the session until stop is closed (nil) or a restart builds the
// next session (returned). The ticker is the session's, so a new poll_ms
// applies from the next session.
func (s *session) loop(stop <-chan struct{}) *session {
	tick := time.NewTicker(time.Duration(s.pollMs) * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			s.stop()
			s.close(false)
			return nil
		case <-s.demoRequests:
			// the demo keeps the loop: a restart that waits goes first, and
			// the demo plays after it
			select {
			case r := <-s.restarts:
				request(s.demoRequests)
				if next := s.takeRestart(r, stop); next != s {
					return next
				}
			default:
				s.playDemo(stop)
			}
		case r := <-s.restarts:
			if next := s.takeRestart(r, stop); next != s {
				return next
			}
		case <-s.store.Kick():
			s.checkConfig(time.Now(), true)
		case <-tick.C:
			s.tick(time.Now())
		}
	}
}

// newSession is a session built from the loop's settings.
func (a *App) newSession() *session { return a.newSessionWith(KeysOf(a.cfg)) }

// newSessionWith is a session that reads the folders in k and ticks every
// k.PollMs.
func (a *App) newSessionWith(k Keys) *session {
	journalDir := k.JournalDir
	if journalDir == "" {
		journalDir = elite.JournalDir()
	}
	if _, err := os.Stat(journalDir); err != nil {
		log.Printf("Journal folder not found: %s (set \"journal_dir\" in %s)", journalDir, a.store.Path())
	} else {
		log.Printf("Reading %s", journalDir)
	}
	bindingsDir := k.BindingsDir
	if bindingsDir == "" {
		bindingsDir = elite.BindingsDir()
	}
	setup := a.newSetup()
	return &session{
		App:       a,
		game:      game.New(),
		haptics:   haptics.New(a.cfg),
		lights:    lights.New(a.cfg),
		status:    elite.NewStatusReader(journalDir),
		journal:   elite.NewJournalTailer(journalDir),
		bindings:  bindings.NewWatcher(bindingsDir),
		profile:   setup,
		pollMs:    k.PollMs,
		hold:      gyro.HoldStart,
		saidUse:   -1,
		startedAt: time.Now(),
		running:   platform.ProcessRunning(elite.GameExe),
	}
}

// newSetup is the attached backend's setup work for a new session.
func (a *App) newSetup() backend.Setup {
	setup := backend.Setup(noSetup{})
	if a.setup != nil {
		setup = a.setup(a.dataDir(), a.tell)
	}
	if r, ok := setup.(backend.Reporter); ok {
		a.reporter.Store(&reporter{r})
	} else {
		a.reporter.Store(nil)
	}
	return setup
}

func (a *App) dataDir() string { return filepath.Dir(a.store.Path()) }

// noSetup is the setup work of a backend that has none.
type noSetup struct{}

func (noSetup) Step()                 {}
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

func (s *session) tick(now time.Time) {
	s.housekeeping(now)
	if w, ok := s.profile.(backend.GameWatcher); ok {
		// DS4Windows' profile is followed with EDSense's gyro off too, to
		// tell when nothing aims
		w.Game(s.running, s.cfg.GyroAim && (s.cfg.GyroBy == config.GyroByEDSense || !s.caps.MotionOff), s.running && s.front())
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
	s.followMotion()
	st := s.gyroStatus()
	s.calibrating.Store(st.Calibrating)
	want := motionProfile
	if s.caps.MotionOff {
		want = motionPolicy(s.running, online, paused, s.cfg.GyroAim, inMenu, own, st.Calibrating)
	}
	s.applyMotion(now, want, own, controllersChanged, st.TouchLifts != s.seen.TouchLifts)
	s.holdGyro(now, own, online, paused, inMenu, st)
	s.checkMotion(own)

	active := s.running && s.game.Active() && !paused
	status := Status{Backend: s.words.Name, Kind: s.kind, Online: online, EliteRunning: s.running, Active: active, Paused: paused, Context: s.context}
	s.publish(status)
	if s.live.watched() {
		s.live.put(s.detail(now, status, own))
	}
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
	s.checkConfig(now, s.store.Recheck())
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
			s.note("Elite Dangerous started")
			s.setHUDPalette() // the colour matrix may have changed while the game was closed
		case !running && s.running:
			log.Print("Elite Dangerous closed")
			s.note("Elite Dangerous closed")
		}
		if running != s.running {
			s.blockChecked, s.eliteBlocked, s.gyroNoData = false, false, false
		}
		s.running = running
	}
}

// checkConfig applies edits to the settings file: every 2 s when the file
// changed, and at once (forced) when the Store wrote it or asks for another
// read. EDSense's own writes apply without a word.
func (s *session) checkConfig(now time.Time, forced bool) {
	if !forced {
		if now.Sub(s.lastConfigCheck) <= 2*time.Second {
			return
		}
		s.lastConfigCheck = now
	}
	st, err := os.Stat(s.store.Path())
	if err != nil || !forced && !st.ModTime().After(s.cfgMod) {
		return
	}
	s.cfgMod = st.ModTime() // a file that does not parse is read again once it changes
	by, outs := s.cfg.GyroBy, s.outputs()
	cfg, own, err := s.store.Reload()
	if errors.Is(err, config.ErrRetry) {
		return // the next tick reads it
	}
	var unread *config.UnreadError
	if errors.As(err, &unread) {
		// another program holds it, say: the next 2 s check reads it
		// again, and this version of the file is said once
		s.cfgMod = time.Time{}
		if !st.ModTime().Equal(s.cfgUnread) {
			s.cfgUnread = st.ModTime()
			log.Printf("Settings not reloaded: %v", err)
		}
		return
	}
	if err != nil {
		log.Printf("Settings not reloaded: %v", err)
		return
	}
	// keys that need a new backend or session wait for the engine, which
	// names them from its own watch of the Store
	*s.cfg = cfg // everything holds this pointer
	if !own {
		log.Print("Settings reloaded")
		s.note("Settings reloaded")
	}
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
	s.nativeOn = s.cfg.Haptics && native && s.audio.Active()
	s.haptics.UseSynth(s.synth, s.nativeOn)
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
			s.note(s.words.Connected)
			// what a crashed EDSense left on a backend that keeps it
			if s.caps.KeepsOverrides {
				s.out.ResetToProfile(s.out.Controllers())
			}
		} else {
			log.Print(s.words.Lost)
			s.note(s.words.Lost)
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
	s.note("Demo started")
	status := Status{Backend: s.words.Name, Kind: s.kind, Online: s.out.Online(), EliteRunning: s.running, Demo: true, Context: "demo"}
	s.publish(status)
	if s.gyro != nil {
		s.gyro.SetHold(gyro.HoldDemo)
	}
	if s.motion != motionProfile {
		s.out.SetMotion(s.controllers, backend.MotionProfile)
	}
	out := s.demoOutput()
	cut := &demoCut{ch: make(chan struct{})}
	s.demoPlaying.Store(cut)
	defer s.demoPlaying.Store(nil)
	out.Stop = cut.ch
	out.Step = func(i, n int) {
		if s.live.watched() {
			l := s.detail(time.Now(), status, false)
			l.DemoStep, l.DemoSteps = i, n
			s.live.put(l)
		}
	}
	demo.Run(s.cfg, out, stop)
	// the demo hands the gyro back to the profile; the next tick applies
	// the policy again
	s.lastFrame, s.active, s.motion = nil, false, motionProfile
}

// demoCut cuts the demo playing short, from the window.
type demoCut struct {
	ch   chan struct{}
	once sync.Once
}

func (c *demoCut) close() { c.once.Do(func() { close(c.ch) }) }

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
