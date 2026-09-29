package haptics

import (
	"math"
	"time"

	"github.com/tolgahan/ed-sense/internal/config"
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
	jump := e.cfg.JumpFeel
	if g.FSDCharging(now) && jump == config.JumpCalm {
		// a soft pulse every second while the drive charges
		p := now.Sub(g.FSDChargeStart).Seconds()
		m.add("fsd_charge", "fsd_charge", Voice{Wave: Sine, F0: 170, Amp: 0.2}, 0.5-0.5*math.Cos(2*math.Pi*p))
	}
	if d, ok := g.HyperspaceTunnel(now); ok && jump != config.JumpOff {
		// into the tunnel: one soft swell, then quiet for the rest of the jump
		if level := hyperspaceSwell(d.Seconds()); level > 0 {
			m.add("hyperspace", "hyperspace", Voice{Wave: Sine, F0: 170, Amp: 0.3}, level)
		}
	}
	if s.Flags.Has(elite.ScoopingFuel) {
		m.add("fuel_scoop", "fuel_scoop", Voice{Wave: Noise, F0: 700, Amp: 0.25, TremHz: 0.8, TremDepth: 0.5}, 0.8+0.2*math.Sin(2*math.Pi*3*t))
	}
	if s.Flags.Has(elite.Overheating) {
		if e.native {
			// boiling: irregular bubbles over a hot hiss
			bubbles := 0.55 + 0.45*math.Sin(2*math.Pi*2.3*t)*math.Sin(2*math.Pi*0.9*t+1)
			m.add("overheat", "", Voice{Wave: Noise, F0: 170, Amp: 0.6, GateHz: 7, GateDuty: 0.4}, bubbles)
		} else if math.Mod(t, 0.5) < 0.25 {
			m.add("overheat", "overheat", Voice{}, 1)
		}
	}
	if s.Flags.Has(elite.BeingInterdicted) {
		m.add("interdiction", "interdiction", Voice{Wave: Sine, F0: 30, Amp: 0.8, TremHz: 1.5, TremDepth: 0.7}, 0.6+0.4*math.Sin(2*math.Pi*1.5*t))
		m.add("interdiction_noise", "", Voice{Wave: Noise, F0: 250, Amp: 0.4, TremHz: 3, TremDepth: 0.5}, 1)
	}
}

// hyperspaceSwell: t s into the hyperspace tunnel, the swell's level. It
// rises for half a second and dies away by 2.5 s.
func hyperspaceSwell(t float64) float64 {
	const rise, end = 0.5, 2.5
	smooth := func(x float64) float64 { return x * x * (3 - 2*x) }
	switch {
	case t < 0 || t >= end:
		return 0
	case t < rise:
		return smooth(t / rise)
	}
	return 1 - smooth((t-rise)/(end-rise))
}

// planetAmbience: the atmospheric glide, and the ground rushing up when
// dropping fast near the surface.
func (e *Engine) planetAmbience(now time.Time, g *game.State, m *mix) {
	s := g.Status
	if !s.InShip() {
		return
	}
	if s.Flags2.Has(elite.GlideMode) {
		m.add("glide", "", Voice{Wave: NormNoise, F0: 150, Amp: 0.45, TremHz: 1.7, TremDepth: 0.5}, 0.8)
		m.add("glide_low", "", Voice{Wave: Sine, F0: 24, Amp: 0.35, TremHz: 0.9, TremDepth: 0.6}, 1)
		return
	}
	if alt, rate, ok := g.Descent(now); ok && !s.Parked() && alt < 2500 && rate > 15 {
		near := 1 - alt/2500
		level := math.Min(1, (rate-15)/120) * (0.35 + 0.65*near)
		m.add("ground_rush", "", Voice{Wave: NormNoise, F0: 70 + 90*near, Amp: 0.55, TremHz: 3 + 5*near, TremDepth: 0.35}, level)
	}
}

// thargoidAmbience: with Thargoid music, a slow throb of two beats drifting
// against each other.
func (e *Engine) thargoidAmbience(g *game.State, m *mix) {
	s := g.Status
	if !(s.InShip() || s.Flags.Has(elite.InSRV)) || !elite.IsThargoidMusic(g.Music) {
		return
	}
	r1, r2 := 0.55, 0.37
	if g.Music == "Combat_Unknown" {
		r1, r2 = 0.9, 0.61
	}
	m.add("thargoid", "", Voice{Wave: Sine, F0: 22, Amp: 0.7, TremHz: r1, TremDepth: 0.95}, 0.6)
	m.add("thargoid_pulse", "", Voice{Wave: NormNoise, F0: 55, Amp: 0.35, TremHz: r2, TremDepth: 0.9}, 0.6)
}

// damageAmbience: from the HUD, sustained fire sizzling on the shields, low
// shields crackling and a weak hull creaking; the hull rattling with the
// shields down.
func (e *Engine) damageAmbience(now time.Time, g *game.State, m *mix) {
	s := g.Status
	if !s.InShip() || s.Parked() {
		return
	}
	hs := g.HUD
	if s.Flags.Has(elite.ShieldsUp) {
		if now.Sub(hs.Shield.At) < 400*time.Millisecond && hs.Splash > 0.5 {
			m.add("shield_sizzle", "", Voice{Wave: NormNoise, F0: 1200, Amp: 0.45, TremHz: 25, TremDepth: 0.6}, math.Min(1, hs.Splash/5))
		}
		if hs.Shield.Fresh(now, 3*time.Second) && hs.Shield.Value <= 40 {
			p := float64(40-hs.Shield.Value) / 40
			m.add("shield_low", "", Voice{Wave: NormNoise, F0: 600, Amp: 0.4, GateHz: 9 + 12*p, GateDuty: 0.25}, 0.15+0.5*p)
		}
	}
	if s.Flags.Has(elite.Supercruise | elite.FSDJump) {
		return
	}
	if hs.Hull.Fresh(now, 5*time.Second) && hs.Hull.Value <= 30 {
		p := float64(30-hs.Hull.Value) / 30
		m.add("hull_creak", "", Voice{Wave: NormNoise, F0: 40 + 25*p, Amp: 0.6, TremHz: 0.6 + 0.9*p, TremDepth: 0.85}, 0.2+0.6*p)
	}
	if g.ShieldsSeen && !s.Flags.Has(elite.ShieldsUp) {
		m.add("shields_offline", "", Voice{Wave: Saw, F0: 28, Amp: 0.4, TremHz: 5, TremDepth: 0.6}, 0.45)
	}
}
