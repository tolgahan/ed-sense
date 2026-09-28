// Package haptics turns the game and the controller input into haptics:
// native waveforms on the DualSense's actuators (see Synth), or rumble motor
// levels when native haptics are not available. Elite does not report
// firing, thrust or boost, so those are read from the controller.
package haptics

import (
	"math"
	"strings"
	"time"

	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/game"
)

type Side int

const (
	BothSides Side = iota // as the effect is designed
	LeftSide
	RightSide
)

// gains are the actuator gains of a side.
func (s Side) gains() (l, r float64) {
	switch s {
	case LeftSide:
		return 1, 0
	case RightSide:
		return 0, 1
	}
	return 1, 1
}

// Shot is a one-shot effect to play.
type Shot struct {
	Effect string
	At     time.Time
	Side   Side
	Scale  float64 // strength for this play; 0 means 1
	Pan    float64 // -1 left .. +1 right; 0 as designed
}

type Engine struct {
	cfg       *config.Config
	synth     *Synth
	native    bool
	due       []Shot           // native one-shots waiting for their time
	pulses    []pulse          // rumble one-shots
	layers    map[string]Voice // native layers playing
	lastTick  time.Time
	duckUntil time.Time           // the turn feel steps back while a one-shot plays
	onPlay    func(effect string) // tests: native one-shots as they play

	triggers          triggerState
	turn              turnState
	boost             boostState
	boostFromBindings bool // else Circle boosts
	heat              float64
	lastScan          time.Time
	lastShot          time.Time // on foot
	rebootFor         time.Time // the shutdown whose reboot was played
}

func New(cfg *config.Config) *Engine {
	return &Engine{cfg: cfg, layers: map[string]Voice{}, turn: turnState{sticks: defaultTurnSticks, mouseSet: defaultMouse}}
}

// UseSynth switches to native haptics with s, or back to rumble.
func (e *Engine) UseSynth(s *Synth, on bool) {
	if e.native && !on && e.synth != nil {
		e.synth.StopAll()
		e.layers = map[string]Voice{}
	}
	e.synth, e.native = s, on && s != nil
}

// Silence stops everything: the game closed, paused, a menu.
func (e *Engine) Silence() {
	e.pulses, e.due = nil, nil
	if e.synth != nil {
		e.synth.StopAll()
	}
	e.layers = map[string]Voice{}
	e.triggers.held, e.triggers.spin = [2][2]time.Time{}, [2][2]float64{}
	e.turn.gyro, e.heat = 0, 0
	e.resetMouse()
}

// Play plays a one-shot effect.
func (e *Engine) Play(s Shot) {
	if s.Scale == 0 {
		s.Scale = 1
	}
	e.play(s)
}

// SetHeat sets the heat estimate (0-1.2).
func (e *Engine) SetHeat(heat float64) { e.heat = heat }

func (e *Engine) play(s Shot) {
	if e.gain(s.Effect) <= 0 || s.Scale <= 0 {
		return
	}
	if e.native {
		e.due = append(e.due, s)
		return
	}
	e.rumble(s)
}

func (e *Engine) playNow(effect string, now time.Time) {
	e.play(Shot{Effect: effect, At: now, Scale: 1})
}

// gain: an effect's strength setting. A second weapon class on a trigger
// ("fire_primary_2") shares its trigger's; the spin-ups ("fire_primary_spin")
// share one.
func (e *Engine) gain(effect string) float64 {
	if strings.HasSuffix(effect, "_spin") {
		return e.cfg.Gain("spin_up")
	}
	return e.cfg.Gain(strings.TrimSuffix(effect, "_2"))
}

// mix collects a tick's continuous effects: synth layers when native,
// motor levels for rumble.
type mix struct {
	e           *Engine
	layers      map[string]mixLayer
	left, right float64
}

type mixLayer struct {
	v     Voice
	level float64
}

// add plays a continuous effect at level: voice v natively, or the rumble
// effect of that name ("" for none).
func (m *mix) add(effect, rumble string, v Voice, level float64) {
	level *= m.e.gain(effect)
	if level <= 0 {
		return
	}
	if m.e.native {
		if v.L == 0 && v.R == 0 {
			v.L, v.R = 1, 1
		}
		m.layers[effect] = mixLayer{v, level}
		return
	}
	if rumble != "" {
		c := m.e.cfg.Rumble[rumble]
		m.left += c.Left * level
		m.right += c.Right * level
	}
}

// Tick drives the haptics at now. Natively it feeds the synth and returns
// 0, 0; otherwise it returns the rumble motor levels (0-1).
func (e *Engine) Tick(now time.Time, g *game.State, pad dualsense.State) (left, right float64) {
	dt := 0.025
	if !e.lastTick.IsZero() {
		dt = math.Max(0.005, math.Min(0.2, now.Sub(e.lastTick).Seconds()))
	}
	e.lastTick = now
	m := &mix{e: e, layers: map[string]mixLayer{}}
	s := g.Status
	dead := g.ShutdownPhase(now) == game.SystemsDead
	if g.ShutdownPhase(now) == game.Rebooting && !e.rebootFor.Equal(g.ShutdownAt) {
		e.rebootFor = g.ShutdownAt
		e.playNow("systems_reboot", now)
	}

	heatIn := 0.0 // heat the weapons add per second
	if pad.OK && !s.InPanel() && !dead {
		switch {
		case s.InShip() && !s.Parked():
			heatIn = e.flying(now, dt, g, pad, m)
		case s.OnFoot() && !s.Flags2.Has(elite.OnFootSocialSpace|elite.OnFootInStation):
			e.onFootShots(now, s, pad)
		}
	}
	if !pad.OK || s.InPanel() {
		e.triggers.held, e.triggers.spin = [2][2]time.Time{}, [2][2]float64{}
	}
	if !pad.OK || s.InPanel() || !s.InShip() || s.Parked() || dead {
		e.stopTurning(now, s.InShip() && !s.Parked() && !dead)
	}

	if !dead {
		e.shipAmbience(now, g, m)
	}
	e.updateHeat(now, dt, g, heatIn)
	if !dead {
		e.heatFeel(g.Status, m)
		e.planetAmbience(now, g, m)
	}
	e.thargoidAmbience(g, m)
	if !dead {
		e.damageAmbience(now, g, m)
	}

	if e.native {
		e.flushNative(now, m)
		return 0, 0
	}
	return e.flushRumble(now, m)
}

// flushNative sets the synth's layers and plays the one-shots that are due.
func (e *Engine) flushNative(now time.Time, m *mix) {
	e.synth.SetMaster(e.cfg.HapticsStrength)
	for k, l := range m.layers {
		e.synth.SetLayer(k, l.v, l.level)
		e.layers[k] = l.v
	}
	for k, v := range e.layers {
		if _, ok := m.layers[k]; !ok {
			e.synth.SetLayer(k, v, 0)
			delete(e.layers, k)
		}
	}
	waiting := e.due[:0]
	for _, s := range e.due {
		if now.Before(s.At) {
			waiting = append(waiting, s)
			continue
		}
		voices, ok := effects[s.Effect]
		if !ok {
			continue
		}
		e.synth.Play(panned(sided(voices, s.Side), s.Pan), e.gain(s.Effect)*s.Scale)
		if e.onPlay != nil {
			e.onPlay(s.Effect)
		}
		if s.Effect != "maneuver_kick" {
			e.duckUntil = now.Add(350 * time.Millisecond)
		}
	}
	e.due = waiting
}

// sided moves an effect to one side.
func sided(vs []Voice, side Side) []Voice {
	if side == BothSides {
		return vs
	}
	out := make([]Voice, len(vs))
	for i, v := range vs {
		v.L, v.R = side.gains()
		out[i] = v
	}
	return out
}

// panned weights the actuators: -1 left, +1 right.
func panned(vs []Voice, pan float64) []Voice {
	if pan == 0 {
		return vs
	}
	l := math.Max(0.25, math.Min(1, 1-pan))
	r := math.Max(0.25, math.Min(1, 1+pan))
	out := make([]Voice, len(vs))
	for i, v := range vs {
		v.L *= l
		v.R *= r
		out[i] = v
	}
	return out
}

// seconds: a clock for slow wobbles.
func seconds(now time.Time) float64 { return float64(now.UnixMilli()) / 1000 }
