package haptics

import (
	"math"

	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
)

// Elite reports heat only above 100% (Overheating), so the heat is
// estimated (0-1.2) from what heats a ship: weapons fired, silent running,
// fuel scooping. It cools otherwise, and is pinned to 100% while the game
// says Overheating.

func (e *Engine) updateHeat(dt float64, g *game.State, weapons float64) {
	s := g.Status
	heatIn := weapons
	if s.InShip() && !s.Parked() {
		if s.Flags.Has(elite.SilentRunning) {
			heatIn += 0.08
		}
		if s.Flags.Has(elite.ScoopingFuel) {
			heatIn += 0.07
		}
	}
	e.heat += (heatIn - 0.05) * dt
	switch {
	case s.Flags.Has(elite.Overheating):
		e.heat = math.Max(e.heat, 1)
	case e.heat > 0.97:
		e.heat = 0.97
	}
	e.heat = math.Max(0, math.Min(1.2, e.heat))
}

// heatFeel: a slow throb that speeds up and roughens as the ship heats up.
func (e *Engine) heatFeel(s elite.Status, m *mix) {
	if !s.InShip() || s.Parked() || e.heat <= 0.4 {
		return
	}
	p := math.Min(1, (e.heat-0.4)/0.6)
	m.add("heat_build", "", Voice{Wave: Noise, F0: 80 + 60*p, Amp: 0.55, TremHz: 0.7 + 2.3*p, TremDepth: 0.75}, 0.12+0.5*math.Pow(p, 1.3))
}
