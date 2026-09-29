// Package demo plays every effect once, driving the real effect code with
// made-up game states, so the effects can be felt without launching Elite.
// Steps about firing, thrust and boost read the real controller: the player
// presses the buttons the step names.
package demo

import (
	"log"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/game"
	"github.com/tolgahan/ed-sense/internal/haptics"
	"github.com/tolgahan/ed-sense/internal/lights"
)

// Output is where the demo plays.
type Output struct {
	Out     backend.Output
	Outputs backend.Outputs
	Pad     backend.Pad
	Synth   *haptics.Synth
	Audio   backend.Audio
}

type step struct {
	label string
	dur   time.Duration
	setup func(g *game.State, h *haptics.Engine, now time.Time)
	after func() // when the step ends
}

// Run plays the demo until the end or until done is closed.
func Run(cfg *config.Config, out Output, done <-chan struct{}) {
	native := prepare(cfg, out)
	finish := func() {
		out.Pad.SetRumble(0, 0)
		out.Synth.StopAll()
		controllers := out.Out.Controllers()
		out.Out.SetMotionOff(controllers, false) // in case the gyro step was cut short
		out.Out.ResetToProfile(controllers)
		sleep(150 * time.Millisecond)
	}
	all := steps(out.Out)
	for i, s := range all {
		g := game.New()
		g.HaveStatus, g.ShieldsSeen = true, true
		h := haptics.New(cfg)
		h.UseSynth(out.Synth, native)
		r := lights.New(cfg)
		start := clock()
		s.setup(g, h, start)
		log.Printf("[%d/%d] %s", i+1, len(all), s.label)
		_ = out.Pad.State() // forget presses from the step before
		var last *backend.Frame
		for clock().Sub(start) < s.dur {
			select {
			case <-done:
				finish()
				return
			default:
			}
			now := clock()
			frame := r.Frame(g, now)
			out.Out.Send(out.Out.Controllers(), last, frame, out.Outputs)
			last = &frame
			if cfg.Haptics {
				left, right := h.Tick(now, g, out.Pad.State())
				out.Pad.SetRumble(haptics.Motor(left), haptics.Motor(right))
			}
			sleep(time.Duration(cfg.PollMs) * time.Millisecond)
		}
		out.Pad.SetRumble(0, 0)
		h.Silence()
		if s.after != nil {
			s.after()
		}
	}
	finish()
	log.Print("Demo done, the controller is back on your DSX profile.")
}

// prepare connects to the controller and reports whether native haptics work.
func prepare(cfg *config.Config, out Output) (native bool) {
	out.Out.RequestStatus()
	out.Pad.Maintain()
	if cfg.HapticsMode != config.HapticsRumble {
		out.Audio.Maintain()
		for i := 0; i < 20 && !out.Audio.Active(); i++ {
			sleep(50 * time.Millisecond)
		}
	}
	native = cfg.Haptics && cfg.HapticsMode != config.HapticsRumble && out.Audio.Active()
	if native {
		log.Print("Haptics: native (virtual DualSense audio)")
	} else {
		log.Print("Haptics: rumble fallback")
	}
	sleep(300 * time.Millisecond)
	if !out.Out.Online() {
		log.Print("DSX did not answer yet; sending anyway (check Incoming UDP in DSX's settings)")
	}
	if !out.Pad.Available() {
		log.Print("Haptics: no virtual DualSense, the rumble steps will be silent (use DSX's DualSense emulation)")
	}
	return native
}

// The demo's clock; tests replace it.
var (
	clock = time.Now
	sleep = time.Sleep
)
