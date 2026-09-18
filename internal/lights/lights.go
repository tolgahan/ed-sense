// Package lights decides what the controller shows: the lightbar, the
// adaptive triggers, the player LEDs and the mic LED.
package lights

import (
	"math"
	"time"

	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
)

type Renderer struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Renderer { return &Renderer{cfg: cfg} }

// Frame is what the controller shows at now.
func (r *Renderer) Frame(g *game.State, now time.Time) dsx.Frame {
	f := dsx.DarkFrame()
	f.Lightbar, f.Brightness = r.lightbar(g, now)
	f.Left, f.Right = r.triggers(g, now)
	f.PlayerLEDs = playerLEDs(g, now)
	f.Mic = micLED(g.Status)
	return f
}

func playerLEDs(g *game.State, now time.Time) dsx.PlayerLEDs {
	s := g.Status
	if left, counting := g.HyperspaceCountdown(now); counting {
		return dsx.LitLEDs(int(math.Ceil(left * 5)))
	}
	if s.InShip() && !s.OnFoot() {
		return dsx.LitLEDs(s.FireGroup + 1)
	}
	return dsx.LEDsOff
}

func micLED(s elite.Status) dsx.MicLED {
	switch {
	case s.Flags.Has(elite.LowFuel) && !s.OnFoot():
		return dsx.MicPulse
	case s.Flags.Has(elite.SilentRunning):
		return dsx.MicOn
	}
	return dsx.MicOff
}

// blinkOn: on for the first half of each period at hz (always on at 0).
func blinkOn(now time.Time, hz float64) bool {
	if hz <= 0 {
		return true
	}
	period := 1000.0 / hz
	return math.Mod(float64(now.UnixMilli()), period) < period/2
}

// breathe: a brightness factor between low and 1.
func breathe(now time.Time, hz, low float64) float64 {
	t := float64(now.UnixMilli()) / 1000.0
	return low + (1-low)*(0.5+0.5*math.Sin(2*math.Pi*hz*t))
}
