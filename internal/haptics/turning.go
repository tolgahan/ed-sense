package haptics

import (
	"math"
	"time"

	"github.com/tolgahan/ed-sense/internal/dualsense"
)

// The ship turning, from gyro aim or a stick bound to yaw, pitch or roll, is
// felt as a faint, even hum that lasts as long as the turn, stronger the
// harder the turn: a plain sine, easy to tune out. A sudden flick of the
// controller adds a soft push. Gyro aim turns the ship only while the
// controller moves, so a gyro turn is felt while it moves, a stick turn while
// the stick is held. The gyro part is off while a finger rests on the
// touchpad (DSX's gyro pause) and with gyro aim off.

type turnState struct {
	gyro     float64 // smoothed controller rotation, deg/s
	amount   float64 // smoothed turn, 0-1
	lastKick time.Time
	sticks   [4]bool // stick axes bound to yaw, pitch or roll

	fastest float64 // diagnostics since the last GyroStats
	touched bool
}

// defaultTurnSticks: without bindings, the left stick turns the ship.
var defaultTurnSticks = [4]bool{true, true, false, false}

func (e *Engine) turning(now time.Time, dt float64, pad dualsense.State, faOff bool, m *mix) {
	t := &e.turn
	raw := pad.AimDegPerSec()
	t.fastest = math.Max(t.fastest, raw)
	if !e.cfg.GyroAim {
		raw = 0 // flying with the sticks: moving the controller turns nothing
	}
	if pad.Touch {
		raw, t.touched = 0, true
	}
	prev := t.gyro
	// smoothed (0.1 s) to even out the hand's uneven speed
	t.gyro += (raw - t.gyro) * (1 - math.Exp(-dt/0.1))
	accel := (t.gyro - prev) / dt // deg/s per second

	target := math.Max(gyroTurn(t.gyro), stickTurn(pad, t.sticks))
	tau := 0.12 // eases in, a short tail when the turn ends
	if target < t.amount {
		tau = 0.25
	}
	t.amount += (target - t.amount) * (1 - math.Exp(-dt/tau))
	if !e.native {
		return
	}
	if level := turnLevel(t.amount); level > 0 {
		if faOff {
			level = math.Min(1, level*1.25) // flight assist off: every turn is felt more
		}
		// stay under the other effects: step back while a one-shot plays
		// and while firing
		switch {
		case now.Before(e.duckUntil):
			level *= 0.4
		case now.Sub(e.triggers.firingAt[0]) < 200*time.Millisecond || now.Sub(e.triggers.firingAt[1]) < 200*time.Millisecond:
			level *= 0.6
		}
		// high and quiet: lower tones are felt as separate taps, a rattle
		m.add("maneuver", "", Voice{Wave: Sine, F0: 200, Amp: 0.25}, level)
	}
	kick := 2000.0
	if faOff {
		kick = 1400
	}
	if accel > kick && t.gyro > 70 && now.Sub(t.lastKick) > 300*time.Millisecond {
		e.play(Shot{Effect: "maneuver_kick", At: now, Scale: math.Min(1, 0.35+accel/10000)})
		t.lastKick = now
	}
}

// stopTurning: not flying; no kick on the first frames back.
func (e *Engine) stopTurning(now time.Time) {
	if e.turn.gyro > 0 || e.turn.amount > 0 {
		e.turn.gyro, e.turn.amount = 0, 0
		e.turn.lastKick = now
	}
}

// GyroStats returns the fastest controller rotation and whether the
// touchpad was touched since the last call.
func (e *Engine) GyroStats() (fastest float64, touched bool) {
	fastest, touched = e.turn.fastest, e.turn.touched
	e.turn.fastest, e.turn.touched = 0, false
	return fastest, touched
}

// gyroTurn: controller rotation as a turn amount (0-1). A shaking hand, under
// 3 deg/s, is ignored; 63 deg/s is a full turn.
func gyroTurn(degPerSec float64) float64 {
	return math.Max(0, math.Min(1, (degPerSec-3)/60))
}

// stickTurn: the largest deflection of a stick axis that turns the ship.
func stickTurn(pad dualsense.State, axes [4]bool) float64 {
	const deadzone = 0.12
	best := 0.0
	for i, on := range axes {
		if on {
			best = math.Max(best, (math.Abs(pad.Sticks[i])-deadzone)/(1-deadzone))
		}
	}
	return math.Min(1, best)
}

// turnLevel: silent when not turning, then felt from the start of a turn and
// rising with it (0.12 at the start, about 0.3 at half, 0.45 at full), under
// the combat effects.
func turnLevel(x float64) float64 {
	if x < 0.02 {
		return 0
	}
	return (0.12 + 0.33*math.Pow(x, 0.9)) * math.Min(1, (x-0.02)/0.04)
}
