package haptics

import (
	"math"
	"time"

	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
)

// flying: what the controller does while flying (weapons, thrust and boost).
func (e *Engine) flying(now time.Time, g *game.State, pad dualsense.State, m *mix) {
	s := g.Status
	weapons := s.Flags.Has(elite.HardpointsDeployed) && !s.Flags.Has(elite.AnalysisMode)
	flutter := 0.85 + 0.15*math.Sin(2*math.Pi*14*seconds(now))
	if weapons && pad.R2Held() {
		m.add("fire_primary", flutter)
	}
	if weapons && pad.L2Held() {
		m.add("fire_secondary", flutter)
	}
	if pad.Held(dualsense.R1) {
		m.add("thrust", 1)
	}
	if pad.WasPressed(dualsense.Circle) && !s.Flags.Has(elite.Supercruise) {
		e.playNow("boost", now)
	}
}

// onFootShots: a shot on each R2 press, repeating while R2 is held down.
func (e *Engine) onFootShots(now time.Time, pad dualsense.State) {
	if !pad.WasPressed(dualsense.R2) && (pad.R2 <= 180 || now.Sub(e.lastShot) < 125*time.Millisecond) {
		return
	}
	e.playNow("onfoot_shot", now)
	e.lastShot = now
}
