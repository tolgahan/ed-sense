package lights

import (
	"math"
	"time"

	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
)

// flash: the lightbar's answer to a moment.
type flash struct {
	color string
	dur   time.Duration
	blink float64 // Hz, 0 = steady
}

var flashes = map[game.MomentKind]flash{
	game.HullDamaged:    {"hit", 600 * time.Millisecond, 8},
	game.ShieldsRaised:  {"shields_up", 900 * time.Millisecond, 0},
	game.ShieldsLost:    {"shields_down", 1500 * time.Millisecond, 6},
	game.Attacked:       {"hit", 250 * time.Millisecond, 0},
	game.Kill:           {"kill", 700 * time.Millisecond, 0},
	game.HeatWarning:    {"overheat", 600 * time.Millisecond, 8},
	game.JetConeBoost:   {"jet_cone", 1200 * time.Millisecond, 0},
	game.Interdicted:    {"interdiction", time.Second, 4},
	game.DockingGranted: {"docking_ok", time.Second, 0},
	game.DockingDenied:  {"docking_no", time.Second, 4},
}

// lightbar: the hull colour, or what the situation calls for.
func (r *Renderer) lightbar(g *game.State, now time.Time) (color [3]int, brightness int) {
	cfg := r.cfg
	s := g.Status
	full := float64(cfg.Brightness)
	onFoot, parked := s.OnFoot(), s.Parked()
	_, countdown := g.HyperspaceCountdown(now)
	hyperspace := countdown || s.Flags.Has(elite.FSDJump)

	color, bright := r.hullColor(g.Hull), full
	switch {
	case onFoot:
		health := 1.0
		if s.Health != nil {
			health = *s.Health
		}
		color = r.hullColor(health)
		if s.Flags2.Has(elite.LowOxygen) && blinkOn(now, 2) {
			color = cfg.Color("low_oxygen")
		}
	case s.Flags.Has(elite.InSRV):
		color = cfg.Color("srv")
	case parked:
		color, bright = cfg.Color("docked"), full*0.35
	case hyperspace:
		color, bright = cfg.Color("hyperspace"), full*breathe(now, 1.5, 0.4)
	case s.Flags.Has(elite.Supercruise):
		color = cfg.Color("supercruise")
	}

	// the situation, lowest priority first: later ones win
	blinking := func(key string, hz float64) {
		color, bright = cfg.Color(key), full
		if !blinkOn(now, hz) {
			bright = 0
		}
	}
	if !onFoot && !parked {
		if g.InCombat() {
			bright = full * breathe(now, 1, 0.35)
		}
		if s.Flags.Has(elite.ScoopingFuel) {
			color, bright = cfg.Color("fuel_scoop"), full*breathe(now, 1.2, 0.3)
		}
		if s.Flags.Has(elite.SilentRunning) {
			bright = 0
		}
		if s.InShip() && g.ShieldsSeen && !s.Flags.Has(elite.ShieldsUp) && !hyperspace && !s.Flags.Has(elite.Supercruise) {
			blinking("shields_down", 2)
		}
		if g.FSDCharging(now) {
			charge := now.Sub(g.FSDChargeStart).Seconds() / 5
			color, bright = cfg.Color("fsd_charge"), full*(0.2+0.8*math.Min(1, charge))
		}
		if s.Flags.Has(elite.Overheating) {
			blinking("overheat", 4)
		}
		if s.Flags.Has(elite.BeingInterdicted) {
			color, bright = cfg.Color("interdiction"), full*breathe(now, 3, 0.2)
		}
	}
	if f, ok := latestFlash(g.Moments, now); ok {
		blinking(f.color, f.blink)
	}
	if !g.DiedAt.IsZero() && now.Sub(g.DiedAt) < 5*time.Second {
		blinking("died", 3)
	}
	return color, clampByte(int(math.Round(bright)))
}

// latestFlash: the flash of the latest moment still showing one.
func latestFlash(moments []game.Moment, now time.Time) (flash, bool) {
	for i := len(moments) - 1; i >= 0; i-- {
		m := moments[i]
		if f, ok := flashes[m.Kind]; ok && !now.Before(m.At) && now.Before(m.At.Add(f.dur)) {
			return f, true
		}
	}
	return flash{}, false
}

// hullColor: green at 1, amber at 0.5, red at 0.2 and below.
func (r *Renderer) hullColor(h float64) [3]int {
	full, half, low := r.cfg.Color("hull_full"), r.cfg.Color("hull_half"), r.cfg.Color("hull_low")
	if h >= 0.5 {
		return mix(half, full, (h-0.5)/0.5)
	}
	return mix(low, half, (h-0.2)/0.3)
}

// mix goes from a to b as t goes from 0 to 1.
func mix(a, b [3]int, t float64) [3]int {
	t = math.Max(0, math.Min(1, t))
	var out [3]int
	for i := range out {
		out[i] = int(math.Round(float64(a[i]) + (float64(b[i])-float64(a[i]))*t))
	}
	return out
}

func clampByte(v int) int { return min(max(v, 0), 255) }
