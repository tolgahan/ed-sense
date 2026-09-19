// Package demo plays every effect once, driving the real effect code with
// made-up game states, so the effects can be felt without launching Elite.
// Steps about firing, thrust and boost read the real controller: the player
// presses the buttons the step names.
package demo

import (
	"log"
	"time"

	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/game"
	"github.com/tolgahan/ed-sense/internal/haptics"
	"github.com/tolgahan/ed-sense/internal/lights"
)

// Output is where the demo plays.
type Output struct {
	DSX     *dsx.Client
	Outputs dsx.Outputs
	Pad     *dualsense.Link
}

type step struct {
	label string
	dur   time.Duration
	setup func(g *game.State, h *haptics.Engine, now time.Time)
}

// Run plays the demo until the end or until done is closed.
func Run(cfg *config.Config, out Output, done <-chan struct{}) {
	prepare(out)
	finish := func() {
		out.Pad.SetRumble(0, 0)
		controllers := out.DSX.Controllers()
		out.DSX.ResetToProfile(controllers)
		time.Sleep(150 * time.Millisecond)
	}
	all := steps()
	for i, s := range all {
		g := game.New()
		g.HaveStatus, g.ShieldsSeen = true, true
		h := haptics.New(cfg)
		r := lights.New(cfg)
		start := time.Now()
		s.setup(g, h, start)
		log.Printf("[%d/%d] %s", i+1, len(all), s.label)
		_ = out.Pad.State() // forget presses from the step before
		var last *dsx.Frame
		for time.Since(start) < s.dur {
			select {
			case <-done:
				finish()
				return
			default:
			}
			now := time.Now()
			frame := r.Frame(g, now)
			out.DSX.Send(out.DSX.Controllers(), last, frame, out.Outputs)
			last = &frame
			if cfg.Haptics {
				left, right := h.Tick(now, g, out.Pad.State())
				out.Pad.SetRumble(haptics.Motor(left), haptics.Motor(right))
			}
			time.Sleep(time.Duration(cfg.PollMs) * time.Millisecond)
		}
		out.Pad.SetRumble(0, 0)
		h.Silence()
	}
	finish()
	log.Print("Demo done, the controller is back on your DSX profile.")
}

// prepare connects to the controller.
func prepare(out Output) {
	out.DSX.RequestStatus()
	out.Pad.Maintain()
	time.Sleep(300 * time.Millisecond)
	if !out.DSX.Online() {
		log.Print("DSX did not answer yet; sending anyway (check Incoming UDP in DSX's settings)")
	}
	if !out.Pad.Available() {
		log.Print("Haptics: no virtual DualSense, the rumble steps will be silent (use DSX's DualSense emulation)")
	}
}
