package haptics

import (
	"math"
	"time"
)

// Without native haptics, effects fall back to the two rumble motors, which
// DSX's "Rumble to Haptics" turns into haptics on the real controller: the
// left motor for the left side, the right for the right.

type pulse struct {
	start       time.Time
	dur         time.Duration
	left, right float64
}

// rumble queues a one-shot as a motor pulse.
func (e *Engine) rumble(s Shot) {
	c := e.cfg.Rumble[s.Effect]
	if c.Ms <= 0 || c.Left <= 0 && c.Right <= 0 {
		return
	}
	l, r := c.Left, c.Right
	switch s.Side {
	case LeftSide:
		l, r = math.Max(l, r), 0
	case RightSide:
		l, r = 0, math.Max(l, r)
	}
	g := e.gain(s.Effect) * math.Min(1, s.Scale)
	e.pulses = append(e.pulses, pulse{start: s.At, dur: time.Duration(c.Ms) * time.Millisecond, left: l * g, right: r * g})
}

// flushRumble adds the pulses playing to the continuous levels. Short pulses
// are flat; longer ones fade out after 30%.
func (e *Engine) flushRumble(now time.Time, m *mix) (left, right float64) {
	left, right = m.left, m.right
	playing := e.pulses[:0]
	for _, p := range e.pulses {
		el := now.Sub(p.start)
		if el >= p.dur {
			continue
		}
		playing = append(playing, p)
		if el < 0 {
			continue
		}
		f := 1.0
		if x := float64(el) / float64(p.dur); p.dur > 150*time.Millisecond && x > 0.3 {
			f = 1 - (x-0.3)/0.7
		}
		left += p.left * f
		right += p.right * f
	}
	e.pulses = playing
	k := e.cfg.HapticsStrength
	return math.Min(1, left*k), math.Min(1, right*k)
}

// Motor converts a level (0-1) to a rumble motor value.
func Motor(v float64) uint8 {
	return uint8(math.Round(math.Max(0, math.Min(1, v)) * 255))
}
