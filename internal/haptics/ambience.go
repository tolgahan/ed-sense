package haptics

import (
	"math"
	"time"

	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
)

// Continuous effects that follow the ship's state.

// shipAmbience: the drive charging, the hyperspace tunnel, fuel scooping,
// overheating and interdiction.
func (e *Engine) shipAmbience(now time.Time, g *game.State, m *mix) {
	s := g.Status
	if !s.InShip() || s.Parked() {
		return
	}
	t := seconds(now)
	if s.Flags.Has(elite.FSDCharging) {
		p := math.Min(1, now.Sub(g.FSDChargeStart).Seconds()/5)
		m.add("fsd_charge", 0.15+0.85*p)
	}
	if _, counting := g.HyperspaceCountdown(now); counting && s.Flags.Has(elite.FSDJump) {
		m.add("hyperspace", 0.6+0.4*math.Sin(2*math.Pi*0.5*t))
	}
	if s.Flags.Has(elite.ScoopingFuel) {
		m.add("fuel_scoop", 0.8+0.2*math.Sin(2*math.Pi*3*t))
	}
	if s.Flags.Has(elite.Overheating) && math.Mod(t, 0.5) < 0.25 {
		m.add("overheat", 1)
	}
	if s.Flags.Has(elite.BeingInterdicted) {
		m.add("interdiction", 0.6+0.4*math.Sin(2*math.Pi*1.5*t))
	}
}
