package app

import (
	"strings"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/elite"
)

// freeGyro: the DS4Windows profile leaves the gyro alone (Output Mode
// Passthru, or Controls with nothing on the gyro).
func freeGyro(p *player) { p.setup.Known, p.setup.Free = true, true }

// Sessions on the DS4Windows backend: its dialect puts every value in
// DS4Windows' ranges and never sends a ToMode, the controller is handed
// back (type 7) whenever DS4Windows comes online, and EDSense's gyro aims
// only while the DS4Windows profile leaves the gyro alone.
var ds4wScripts = []script{
	{"ds4w-late", nil, func(p *player) {
		p.writeStatus(elite.Status{Flags: ship})
		p.run(6 * time.Second)
		p.dsx.Answering = true
		p.note("DS4Windows answers")
		p.run(4 * time.Second)
		p.stop()
	}},
	{"ds4w-drops", nil, func(p *player) {
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		p.writeStatus(elite.Status{Flags: weaponsOut})
		p.run(time.Second)
		p.dsx.Answering = false
		p.note("DS4Windows stops answering (the game-mod option unticked)")
		p.run(7 * time.Second)
		p.dsx.Answering = true
		p.note("DS4Windows answers again")
		p.run(time.Second)
		p.stop()
	}},
	{"ds4w-flight", nil, func(p *player) {
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		p.writeStatus(elite.Status{Flags: ship})
		p.run(time.Second)
		p.writeStatus(elite.Status{Flags: weaponsOut})
		p.note("hardpoints out")
		p.run(time.Second)
		p.pad.Hold(held(dualsense.R2, 220, 0))
		p.note("R2 held")
		p.run(time.Second)
		p.pad.Hold(dualsense.State{})
		p.writeStatus(elite.Status{Flags: weaponsOut | elite.AnalysisMode})
		p.note("analysis mode: the scanner triggers")
		p.run(time.Second)
		p.writeStatus(elite.Status{Flags: ship | elite.Supercruise})
		p.note("supercruise")
		p.run(time.Second)
		p.writeStatus(elite.Status{Flags: ship | elite.Supercruise, GuiFocus: 6})
		p.note("galaxy map")
		p.run(time.Second)
		p.writeStatus(elite.Status{Flags: ship})
		p.note("back in normal space")
		p.run(time.Second)
		p.stop()
	}},
	{"ds4w-outputs", nil, func(p *player) {
		p.dsx.Answering = true
		p.writeStatus(elite.Status{Flags: ship})
		p.run(time.Second)
		p.config(func(c *config.Config) { c.Lightbar, c.MicLED = false, false })
		p.run(4 * time.Second)
		p.config(func(c *config.Config) { c.Triggers, c.PlayerLEDs = false, false })
		p.run(4 * time.Second)
		p.config(func(c *config.Config) { c.Triggers = true })
		p.run(3 * time.Second)
		p.stop()
	}},
	// the window's own writes apply without "Settings reloaded": at once on
	// the Store's kick, or at the 2 s check
	{"window-patch-ds4w", nil, func(p *player) {
		p.dsx.Answering = true
		p.writeStatus(elite.Status{Flags: ship})
		p.run(time.Second)
		p.patch(`{"control_triggers":false}`)
		p.s.checkConfig(p.now, true)
		p.section("# the loop applies it on the Store's kick")
		p.run(3 * time.Second)
		p.patch(`{"control_triggers":true}`)
		p.run(3 * time.Second)
		p.stop()
	}},
	{"ds4w-ranges", func(c *config.Config) {
		c.TriggerFX["ship_weapons_r"] = config.Trigger{Mode: "WEAPON", Params: []int{1, 9, 12}}
		c.TriggerFX["ship_weapons_l"] = config.Trigger{Mode: "FEEDBACK", Params: []int{12, -3}}
		c.TriggerFX["ship_overheat_r"] = config.Trigger{Mode: "VIBRATION", Params: []int{2, 6, 300}}
		c.TriggerFX["ship_overheat_l"] = config.Trigger{Mode: "SLOPE_FEEDBACK", Params: []int{5, 3, 0, 9}}
		c.TriggerFX["hit"] = config.Trigger{Mode: "VIBRATION", Params: []int{1, 5}}
		c.Colors["hull_full"] = [3]int{300, -20, 60}
	}, func(p *player) {
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		p.writeStatus(elite.Status{Flags: weaponsOut})
		p.run(2 * time.Second)
		p.writeStatus(elite.Status{Flags: weaponsOut | elite.Overheating})
		p.note("overheating")
		p.run(time.Second)
		p.writeStatus(elite.Status{Flags: weaponsOut})
		p.event(`{"event":"UnderAttack","Target":"You"}`)
		p.note("under attack")
		p.run(time.Second)
		p.stop()
	}},
	{"ds4w-gyro-none", ownGyro, func(p *player) {
		freeGyro(p)
		flying(p)
		p.run(time.Second)
		p.pad.Stream = turn(20, 0, 0, false)
		p.note("turning left at 20 deg/s")
		p.run(time.Second)
		p.writeStatus(elite.Status{Flags: ship, GuiFocus: 6})
		p.note("galaxy map")
		p.run(time.Second)
		p.writeStatus(elite.Status{Flags: ship})
		p.front = false
		p.note("alt-tab")
		p.run(time.Second)
		p.front = true
		p.note("back in Elite")
		p.run(time.Second)
		p.stop()
	}},
	{"ds4w-gyro-mouse", ownGyro, func(p *player) {
		p.setup.Known, p.setup.Mouse = true, true // the profile's gyro is Mouse
		flying(p)
		p.pad.Stream = turn(20, 0, 0, false)
		p.run(2 * time.Second)
		p.setup.Mouse, p.setup.Free = false, true
		p.note("the profile's gyro set to Passthru, and saved")
		p.run(time.Second)
		p.stop()
	}},
	{"ds4w-gyro-unknown", ownGyro, func(p *player) {
		flying(p)
		p.pad.Stream = turn(20, 0, 0, false)
		p.run(6 * time.Second)
		p.stop()
	}},
	{"ds4w-nodata", ownGyro, func(p *player) {
		freeGyro(p)
		flying(p)
		p.pad.Stream = &dualsense.State{Accel: [3]int16{0, 0, -8192}}
		p.note("reports without motion data, as DS4Windows' virtual pad sends them")
		p.run(4 * time.Second)
		p.setup.Free = false
		p.note("the profile's gyro set to Mouse Joystick")
		p.run(500 * time.Millisecond)
		p.setup.Free = true
		p.pad.Stream = turn(20, 0, 0, false)
		p.note("the profile's gyro set to Passthru, now with motion")
		p.run(time.Second)
		p.stop()
	}},
	// the rumble fallback would put the controller in rumble emulation,
	// which mutes native haptics: the motors stay still while native
	// haptics are lost
	{"ds4w-audio-flips", nil, func(p *player) {
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		p.writeStatus(elite.Status{Flags: weaponsOut})
		p.run(time.Second)
		p.pad.Hold(held(dualsense.R2, 220, 0))
		p.run(time.Second)
		p.audio.Up = false
		p.note("native haptics lost")
		p.run(time.Second)
		p.audio.Up = true
		p.note("native haptics back")
		p.run(time.Second)
		p.stop()
	}},
	// haptics_mode "rumble" is the player's choice: it rumbles, and the stop
	// when it is set back lets native haptics play again
	{"ds4w-rumble-mode", func(c *config.Config) { c.HapticsMode = config.HapticsRumble }, func(p *player) {
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		p.writeStatus(elite.Status{Flags: weaponsOut})
		p.run(time.Second)
		p.pad.Hold(held(dualsense.R2, 220, 0))
		p.note("R2 held")
		p.run(time.Second)
		p.config(func(c *config.Config) { c.HapticsMode = config.HapticsAuto })
		p.run(3 * time.Second)
		p.stop()
	}},
	{"ds4w-demo", func(c *config.Config) { c.Lightbar, c.MicLED = false, false }, func(p *player) {
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
		p.writeStatus(elite.Status{Flags: weaponsOut})
		p.run(time.Second)
		quit, reads := make(chan struct{}), 0
		p.pad.OnRead = func() {
			if reads++; reads == 2 {
				close(quit)
			}
		}
		p.s.playDemo(quit)
		p.pad.OnRead = nil
		p.section("# the tray's Play demo, then Quit")
		p.run(time.Second)
		p.stop()
	}},
	// the demo without native haptics says why there are none
	{"ds4w-demo-no-audio", func(c *config.Config) { c.Lightbar, c.MicLED = false, false }, func(p *player) {
		p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, false
		p.writeStatus(elite.Status{Flags: weaponsOut})
		p.run(time.Second)
		quit, reads := make(chan struct{}), 0
		p.pad.OnRead = func() {
			if reads++; reads == 2 {
				close(quit)
			}
		}
		p.s.playDemo(quit)
		p.pad.OnRead = nil
		p.section("# the tray's Play demo, then Quit")
		p.run(time.Second)
		p.stop()
	}},
}

// TestDS4WindowsHandBack: DS4Windows keeps what it was told, so a panic in
// the loop hands the controller back before the program dies.
func TestDS4WindowsHandBack(t *testing.T) {
	p := newDS4WPlayer(t, nil)
	p.dsx.Answering = true
	p.rec.Take()
	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic was swallowed")
			}
		}()
		defer p.s.handBackOnPanic()
		panic("boom")
	}()
	if got := strings.Join(p.rec.Take(), "\n"); !strings.Contains(got, `"type":7`) {
		t.Errorf("no hand-back: %s", got)
	}
	d := newPlayer(t, nil)
	d.rec.Take()
	func() {
		defer func() { _ = recover() }()
		defer d.s.handBackOnPanic()
		panic("boom")
	}()
	if got := strings.Join(d.rec.Take(), "\n"); strings.Contains(got, `"type":7`) {
		t.Errorf("DSX forgets by itself, yet was handed back: %s", got)
	}
}

// panicky is a setup whose Game panics, as a bug in the loop would.
type panicky struct{ backend.Setup }

func (panicky) Game(running, aim, front bool) { panic("boom") }

// TestDS4WindowsRunPanic: the same through Run, on the loop's own
// goroutine: a panic in the first tick hands the controller back before the
// parts close, and still ends the program.
func TestDS4WindowsRunPanic(t *testing.T) {
	p := newDS4WPlayer(t, nil)
	p.dsx.Answering = true
	p.watch = panicky{p.watch}
	p.rec.Take()
	var got any
	func() {
		defer func() { got = recover() }()
		p.s.App.Run(make(chan struct{}))
	}()
	if got != "boom" {
		t.Fatalf("Run ended with %v, want the panic", got)
	}
	lines := p.rec.Take()
	handBack, closed := -1, -1
	for i, l := range lines {
		if handBack < 0 && strings.Contains(l, `"type":7`) {
			handBack = i
		}
		if closed < 0 && (l == "audio close" || l == "pad close") {
			closed = i
		}
	}
	if handBack < 0 || closed < 0 || handBack > closed {
		t.Errorf("no hand-back before the parts close:\n%s", strings.Join(lines, "\n"))
	}
}
