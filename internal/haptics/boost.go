package haptics

import (
	"math"
	"time"

	"github.com/tolgahan/ed-sense/internal/game"
	"github.com/tolgahan/ed-sense/internal/hud"
)

// A boost needs enough charge in the engine capacitor (read from the HUD);
// without it the press is a dud. How much it needs differs between ships,
// so it is learned from what the capacitor does after a press.

type boostState struct {
	pressedAt time.Time
	engAt     float64 // the engine capacitor when pressed
	need      float64 // learned; 0 until then
}

const defaultBoostNeed = 0.25

func (b boostState) needed() float64 {
	if b.need == 0 {
		return defaultBoostNeed
	}
	return b.need
}

// startBoost: the boost feel, or a dud when the engine capacitor can't pay.
func (e *Engine) startBoost(now time.Time, g *game.State) {
	if caps := g.HUD.Capacitors; caps.Fresh(now, 2*time.Second) {
		e.boost.pressedAt, e.boost.engAt = now, caps.Value[hud.ENG]
		if caps.Value[hud.ENG] < e.boost.needed() {
			e.playNow("boost_empty", now)
			return
		}
	}
	e.playNow("boost", now)
}

// learnBoost: 0.8 s after a press, did the engine capacitor drain? Then that
// charge was enough; if not, too little. The need settles between the two.
func (e *Engine) learnBoost(now time.Time, g *game.State) {
	b := &e.boost
	if b.pressedAt.IsZero() || now.Sub(b.pressedAt) < 800*time.Millisecond {
		return
	}
	b.pressedAt = time.Time{}
	caps := g.HUD.Capacitors
	if !caps.Fresh(now, 2*time.Second) {
		return
	}
	boosted := b.engAt-caps.Value[hud.ENG] >= 0.1
	switch need := b.needed(); {
	case boosted && b.engAt < need:
		b.need = math.Max(0.05, b.engAt-0.02)
	case !boosted && b.engAt >= need:
		b.need = math.Min(0.9, b.engAt+0.05)
	}
}
