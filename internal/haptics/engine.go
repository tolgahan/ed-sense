// Package haptics turns the game and the controller input into rumble for
// DSX's virtual DualSense, which DSX's "Rumble to Haptics" turns into haptics
// on the controller. Elite does not report firing, thrust or boost, so those
// are read from the controller.
package haptics

import (
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

// Shot is a one-shot effect to play.
type Shot struct {
	Effect string
	At     time.Time
	Side   Side
	Scale  float64 // strength for this play; 0 means 1
}

type Engine struct {
	cfg      *config.Config
	pulses   []pulse // rumble one-shots
	lastScan time.Time
	lastShot time.Time // on foot
}

func New(cfg *config.Config) *Engine {
	return &Engine{cfg: cfg}
}

// Silence stops everything: the game closed, paused, a menu.
func (e *Engine) Silence() {
	e.pulses = nil
}

// Play plays a one-shot effect.
func (e *Engine) Play(s Shot) {
	if s.Scale == 0 {
		s.Scale = 1
	}
	e.play(s)
}

func (e *Engine) play(s Shot) {
	if e.cfg.Gain(s.Effect) <= 0 || s.Scale <= 0 {
		return
	}
	e.rumble(s)
}

func (e *Engine) playNow(effect string, now time.Time) {
	e.play(Shot{Effect: effect, At: now, Scale: 1})
}

// mix collects a tick's continuous effects as motor levels.
type mix struct {
	e           *Engine
	left, right float64
}

// add plays a continuous effect at level: its rumble, scaled by its gain.
func (m *mix) add(effect string, level float64) {
	level *= m.e.cfg.Gain(effect)
	if level <= 0 {
		return
	}
	c := m.e.cfg.Rumble[effect]
	m.left += c.Left * level
	m.right += c.Right * level
}

// Tick drives the haptics at now and returns the rumble motor levels (0-1).
func (e *Engine) Tick(now time.Time, g *game.State, pad dualsense.State) (left, right float64) {
	m := &mix{e: e}
	s := g.Status
	if pad.OK && !s.InPanel() {
		switch {
		case s.InShip() && !s.Parked():
			e.flying(now, g, pad, m)
		case s.OnFoot() && !s.Flags2.Has(elite.OnFootSocialSpace|elite.OnFootInStation):
			e.onFootShots(now, pad)
		}
	}
	e.shipAmbience(now, g, m)
	return e.flushRumble(now, m)
}

// seconds: a clock for slow wobbles.
func seconds(now time.Time) float64 { return float64(now.UnixMilli()) / 1000 }
