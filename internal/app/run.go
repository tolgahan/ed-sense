package app

import (
	"log"
	"os"
	"slices"
	"time"

	"github.com/tolgahan/ed-sense/internal/demo"
	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
	"github.com/tolgahan/ed-sense/internal/haptics"
	"github.com/tolgahan/ed-sense/internal/lights"
	"github.com/tolgahan/ed-sense/internal/platform"
)

// session is the state of one Run.
type session struct {
	*App
	game    *game.State
	haptics *haptics.Engine
	lights  *lights.Renderer
	status  *elite.StatusReader
	journal *elite.JournalTailer

	startedAt         time.Time
	lastConfigCheck   time.Time
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
}

// Run blocks until stop is closed; then it hands the controller back to
// the DSX profile.
func (a *App) Run(stop <-chan struct{}) {
	s := a.newSession()
	defer a.pad.Close()

	tick := time.NewTicker(time.Duration(a.cfg.PollMs) * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			s.stop()
			return
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
	return &session{
		App:       a,
		game:      game.New(),
		haptics:   haptics.New(a.cfg),
		lights:    lights.New(a.cfg),
		status:    elite.NewStatusReader(journalDir),
		journal:   elite.NewJournalTailer(journalDir),
		startedAt: time.Now(),
		running:   platform.ProcessRunning(elite.GameExe),
	}
}

func (s *session) tick(now time.Time) {
	s.housekeeping(now)
	s.readGame(now)
	s.maintainHaptics()
	online := s.checkDSX(now)
	s.logContext()
	controllers := s.dsx.Controllers()
	controllersChanged := !slices.Equal(controllers, s.controllers)
	if controllersChanged {
		s.controllers, s.lastFrame = controllers, nil
	}
	paused := s.Paused()

	active := s.running && s.game.Active() && !paused
	s.publish(Status{DSXOnline: online, EliteRunning: s.running, Active: active, Paused: paused, Context: s.context})
	if !active {
		s.idle()
		return
	}
	s.active = true
	s.driveHaptics(now)
	s.sendFrame(now)
}

// housekeeping: settings edits, DSX's controller list and whether the game
// runs.
func (s *session) housekeeping(now time.Time) {
	if now.Sub(s.lastConfigCheck) > 2*time.Second {
		s.lastConfigCheck = now
		if s.reloadConfig() {
			s.lastFrame = nil
		}
	}
	if now.Sub(s.lastStatusRequest) > 2*time.Second {
		s.lastStatusRequest = now
		s.dsx.RequestStatus()
	}
	if now.Sub(s.lastProcessCheck) > 3*time.Second {
		s.lastProcessCheck = now
		running := platform.ProcessRunning(elite.GameExe)
		switch {
		case running && !s.running:
			log.Print("Elite Dangerous started")
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
	})
	if st, changed := s.status.Poll(); changed {
		if s.game.HaveStatus {
			s.haptics.OnStatus(s.game.Status, st, now)
		}
		s.game.OnStatus(st, now)
	}
}

// maintainHaptics keeps the virtual DualSense open.
func (s *session) maintainHaptics() {
	if s.cfg.Haptics && s.running {
		s.pad.Maintain()
	}
}

func (s *session) checkDSX(now time.Time) (online bool) {
	online = s.dsx.Online()
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
		s.dsx.ResetToProfile(s.controllers)
		log.Print("Controller handed back to your DSX profile")
	}
	s.active, s.lastFrame = false, nil
	s.pad.SetRumble(0, 0)
	_ = s.pad.State() // drop presses made meanwhile
	s.haptics.Silence()
}

func (s *session) driveHaptics(now time.Time) {
	if !s.cfg.Haptics {
		s.pad.SetRumble(0, 0)
		s.haptics.Silence()
		return
	}
	pad := s.pad.State()
	left, right := s.haptics.Tick(now, s.game, pad)
	s.pad.SetRumble(haptics.Motor(left), haptics.Motor(right))
}

// sendFrame sends what changed, and everything every 3 s.
func (s *session) sendFrame(now time.Time) {
	frame := s.lights.Frame(s.game, now)
	prev := s.lastFrame
	if prev == nil || now.Sub(s.lastFull) > 3*time.Second {
		prev, s.lastFull = nil, now
	}
	s.dsx.Send(s.controllers, prev, frame, s.outputs())
	s.lastFrame = &frame
}

func (s *session) playDemo(stop <-chan struct{}) {
	log.Print("Demo started")
	s.publish(Status{DSXOnline: s.dsx.Online(), EliteRunning: s.running, Demo: true, Context: "demo"})
	demo.Run(s.cfg, s.demoOutput(), stop)
	s.lastFrame, s.active = nil, false
}

// PlayDemo plays the demo on its own, without following the game.
func (a *App) PlayDemo(stop <-chan struct{}) {
	defer a.pad.Close()
	demo.Run(a.cfg, a.demoOutput(), stop)
	time.Sleep(200 * time.Millisecond) // let the last packets go
}

func (a *App) demoOutput() demo.Output {
	return demo.Output{DSX: a.dsx, Outputs: a.outputs(), Pad: a.pad}
}

func (s *session) stop() {
	controllers := s.dsx.Controllers()
	if s.active {
		s.dsx.ResetToProfile(controllers)
		time.Sleep(150 * time.Millisecond) // let the packets go
	}
	log.Print("Stopped")
}
