package diag

import (
	"fmt"
	"log"
	"time"

	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
	"github.com/tolgahan/ed-sense/internal/haptics"
)

// Feel plays the turn and jump feels one after another through the real
// effect code, with a made-up ship and controller, so turn_feel and
// jump_feel can be chosen without flying. Four plain tones come first, to
// tell which pitches the controller plays smoothly.
func Feel(cfg config.Config, done <-chan struct{}) {
	synth := haptics.NewSynth()
	out := dualsense.NewHapticsOut(synth.Render)
	defer out.Close()
	out.Maintain()
	for i := 0; i < 40 && !out.Active(); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if !out.Active() {
		fmt.Println("\nNative haptics could not start (see the log line above).")
		return
	}

	log.Print("TONES: four plain tones, 2 s each. Which feel smooth, which rattle or buzz?")
	for i, hz := range []float64{80, 120, 170, 250} {
		log.Printf("  tone %d: %.0f Hz", i+1, hz)
		tone := haptics.Voice{Wave: haptics.Sine, F0: hz, Amp: 0.3, L: 1, R: 1}
		synth.SetLayer("tone", tone, 1)
		if !wait(done, 2*time.Second) {
			return
		}
		synth.SetLayer("tone", tone, 0)
		if !wait(done, time.Second) {
			return
		}
	}

	for i, feel := range []string{config.TurnWaves, config.TurnPush} {
		log.Printf("TURN %d: turn_feel %q. A turn starts, holds, gets harder and ends", i+1, feel)
		c := cfg
		c.TurnFeel = feel
		h, g := feelRig(&c, synth)
		if !drive(h, g, 9*time.Second, turnScript, done) {
			return
		}
	}

	for i, feel := range []string{config.JumpSwell, config.JumpCalm} {
		log.Printf("JUMP %d: jump_feel %q. A 5 s countdown, then the hyperspace tunnel", i+1, feel)
		c := cfg
		c.JumpFeel = feel
		h, g := feelRig(&c, synth)
		now := time.Now()
		g.OnEvent(elite.Event{"event": "StartJump", "JumpType": "Hyperspace"}, true, now)
		g.OnStatus(elite.Status{Flags: feelShip | elite.Supercruise | elite.FSDCharging | elite.FSDJump}, now)
		if !drive(h, g, 9*time.Second, stillPad, done) {
			return
		}
		g.OnEvent(elite.Event{"event": "FSDJump"}, true, time.Now())
		g.OnStatus(elite.Status{Flags: feelShip | elite.Supercruise}, time.Now())
		if !drive(h, g, time.Second, stillPad, done) {
			return
		}
	}
	synth.StopAll()
	log.Print(`Done. Put the ones you like in edsense.json, for example "turn_feel": "push"; EDSense picks the change up while it runs.`)
}

const feelShip = elite.InMainShip | elite.ShieldsUp

// feelRig: an engine playing on synth, and a ship in normal space.
func feelRig(cfg *config.Config, synth *haptics.Synth) (*haptics.Engine, *game.State) {
	h := haptics.New(cfg)
	h.UseSynth(synth, true)
	g := game.New()
	g.OnStatus(elite.Status{Flags: feelShip}, time.Now())
	return h, g
}

// drive ticks the engine every 25 ms for d, with the controller as pad says
// t s in; false if done was closed meanwhile.
func drive(h *haptics.Engine, g *game.State, d time.Duration, pad func(t float64) dualsense.State, done <-chan struct{}) bool {
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	start := time.Now()
	for now := start; now.Sub(start) < d; {
		select {
		case <-done:
			h.Silence()
			return false
		case now = <-tick.C:
			h.Tick(now, g, pad(now.Sub(start).Seconds()))
		}
	}
	h.Silence()
	return wait(done, time.Second)
}

// turnScript: the left stick pushed to 60% for 2.5 s, then all the way for
// 2.5 s (longer than a "waves" swell), then let go.
func turnScript(t float64) dualsense.State {
	var y float64
	switch {
	case t < 0.5:
	case t < 0.8:
		y = 0.6 * (t - 0.5) / 0.3
	case t < 3.3:
		y = 0.6
	case t < 3.5:
		y = 0.6 + 0.4*(t-3.3)/0.2
	case t < 6.0:
		y = 1
	case t < 6.2:
		y = 1 - (t-6.0)/0.2
	}
	return dualsense.State{OK: true, Sticks: [4]float64{0, -y, 0, 0}}
}

func stillPad(float64) dualsense.State { return dualsense.State{OK: true} }
