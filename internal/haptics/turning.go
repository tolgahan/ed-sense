package haptics

import (
	"math"
	"time"

	"github.com/tolgahan/ed-sense/internal/bindings"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/game"
)

// The ship turning, from gyro aim or a stick bound to yaw, pitch or roll, is
// felt only outside the throttle's blue zone (read from the HUD): in the
// blue zone, where the ship turns best, nothing is felt. Outside it, as
// turn_feel says: "waves", soft swells about every 2 s while the ship turns,
// stronger the harder the turn; "push", a soft push when a turn starts,
// changes or ends, and nothing while it holds; or "off". Both glide out
// (slower with flight assist off, when the ship keeps rotating), and a
// sudden flick of the controller adds a soft push. The actuators are smooth
// around 170 Hz: lower tones are felt as a rattle, higher ones as a buzz,
// and a steady tone of any pitch soon grates, so nothing here is steady.
//
// Gyro aim reaches Elite as mouse movement (DSX's motion to mouse), and
// Elite's mouse deflects a virtual stick. With mouse decay off the
// deflection holds when the controller stops: the ship keeps turning until
// the controller turns back or MouseReset centres it. So the gyro part
// follows that virtual stick, worked out from the controller's rotation, and
// a controller held tilted is felt like a held stick. With decay on the
// virtual stick springs back, and a gyro turn is felt while the controller
// moves. The gyro part pauses while a finger rests on the touchpad (DSX's
// gyro pause) and while mouse headlook has the mouse, and is off with gyro
// aim off or when the mouse turns nothing. In the hyperspace tunnel the ship
// does not turn, and nothing is felt.

type turnState struct {
	gyro     float64    // smoothed controller rotation, deg/s
	mouse    [2]float64 // Elite's virtual mouse stick, X and Y, -1..1
	amount   float64    // smoothed turn, 0-1
	blueZone float64    // smoothed "throttle in the blue zone", 0-1
	felt     float64    // the turn feel's level before a swell, for tests

	// "waves": the swell's phase in cycles (0 is a crest), and the swell
	// level, eased so a restart does not click
	phase, swell float64
	// "push": the turn with its direction (the turning stick axes, then the
	// virtual mouse X and Y), and the same followed slowly; a push is the
	// difference, so a reversal is felt like a start
	vec, settled [6]float64

	lastKick time.Time
	sticks   [4]bool // stick axes bound to yaw, pitch or roll
	mouseSet bindings.Mouse
	headlook bool // the mouse moves the view

	fastest float64 // diagnostics since the last GyroStats
	touched bool
}

// defaultTurnSticks: without bindings, the left stick turns the ship.
var defaultTurnSticks = [4]bool{true, true, false, false}

// defaultMouse: without bindings, the mouse turns the ship and springs back,
// so a gyro turn is felt while the controller moves.
var defaultMouse = bindings.Mouse{Turns: [2]bool{true, true}}

const (
	// mouseFullTurn: the controller rotation, in degrees, that deflects the
	// virtual stick fully, with DSX's bundled profile or EDSense's gyro at
	// sensitivity 1. A guess: Elite does not say, and it depends on Elite's
	// mouse sensitivity.
	mouseFullTurn = 20.0
	// mouseStill: slower rotation, in deg/s, is a steady hand
	mouseStill = 2.0
	// mouseSpring: how fast a decaying virtual stick springs back, s
	mouseSpring = 0.25
	// mouseForget: a held deflection fades over this, s, so the estimate
	// does not stay off for long when it went wrong (the reset key pressed
	// while Elite was not in front, the controller turned in the pause menu)
	mouseForget = 30.0
)

func (e *Engine) turning(now time.Time, dt float64, pad dualsense.State, faOff bool, g *game.State, m *mix) {
	t := &e.turn
	raw := pad.AimDegPerSec()
	t.fastest = math.Max(t.fastest, raw)
	// yaw and a share of roll move the mouse sideways, as DSX's motion to
	// mouse and EDSense's gyro both do; pitch moves it up and down
	rates := [2]float64{pad.GyroDegPerSec(1) + e.cfg.GyroRollMix*pad.GyroDegPerSec(2), pad.GyroDegPerSec(0)}
	if !e.cfg.GyroAim || t.mouseSet.Turns == ([2]bool{}) {
		raw, rates = 0, [2]float64{} // flying with the sticks: moving the controller turns nothing
	}
	if pad.Touch {
		raw, rates, t.touched = 0, [2]float64{}, true
	}
	if t.headlook {
		raw, rates = 0, [2]float64{}
	}
	prev := t.gyro
	// smoothed (0.1 s) to even out the hand's uneven speed
	t.gyro += (raw - t.gyro) * (1 - math.Exp(-dt/0.1))
	accel := (t.gyro - prev) / dt // deg/s per second
	full := [2]float64{mouseFullTurn, mouseFullTurn}
	if e.cfg.GyroBy == config.GyroByEDSense {
		full = [2]float64{mouseFullTurn / e.cfg.GyroSensitivityX, mouseFullTurn / e.cfg.GyroSensitivityY}
	}
	t.moveMouse(rates, dt, full)
	if _, tunnel := g.HyperspaceTunnel(now); tunnel {
		e.stopTurning(now, true) // the virtual stick holds for the arrival
		return
	}

	target := math.Max(t.mouseTurn(), stickTurn(pad, t.sticks))
	tau := 0.15 // eases in, glides out when the turn ends
	switch {
	case target < t.amount && faOff:
		tau = 1.5
	case target < t.amount:
		tau = 0.45
	}
	t.amount += (target - t.amount) * (1 - math.Exp(-dt/tau))
	change := 0.0
	for i, v := range t.turnVector(pad) {
		t.vec[i] += (v - t.vec[i]) * (1 - math.Exp(-dt/tau))
		t.settled[i] += (t.vec[i] - t.settled[i]) * (1 - math.Exp(-dt/0.35))
		change += (t.vec[i] - t.settled[i]) * (t.vec[i] - t.settled[i])
	}
	// a swell about every 2.2 s; a turn that starts, or grows much harder in
	// a trough, brings the next crest forward
	crest := 0.5 + 0.5*math.Cos(2*math.Pi*t.phase)
	if t.amount < 0.02 || (target-t.amount > 0.15 && crest < 0.5) {
		t.phase, crest = 0, 1
	}
	t.phase = math.Mod(t.phase+0.45*dt, 1)
	t.swell += (crest - t.swell) * (1 - math.Exp(-dt/0.1))
	blue := 0.0
	if z := g.HUD.BlueZone; z.Fresh(now, 2*time.Second) && z.Value {
		blue = 1
	}
	t.blueZone += (blue - t.blueZone) * (1 - math.Exp(-dt/0.2))
	if math.Abs(blue-t.blueZone) < 0.01 {
		t.blueZone = blue
	}
	feel := e.cfg.TurnFeel
	t.felt = 0
	if !e.native || feel == config.TurnOff {
		return
	}
	outside := 1 - t.blueZone
	felt, swell, voice := t.amount, t.swell, Voice{Wave: Sine, F0: 170, Amp: 0.25}
	if feel == config.TurnPush {
		// about a second per push, fading out; a small change is not felt
		felt = math.Max(0, math.Min(1, 2*math.Sqrt(change))-0.1) / 0.9
		swell, voice = 1, Voice{Wave: Sine, F0: 170, Amp: 0.3}
	}
	if level := turnLevel(felt) * outside; level > 0 {
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
		t.felt = level
		m.add("maneuver", "", voice, level*swell)
	}
	kick := 2000.0
	if faOff {
		kick = 1400
	}
	if accel > kick && t.gyro > 70 && now.Sub(t.lastKick) > 300*time.Millisecond {
		if outside > 0.5 {
			e.play(Shot{Effect: "maneuver_kick", At: now, Scale: math.Min(1, 0.35+accel/10000)})
		}
		t.lastKick = now
	}
}

// moveMouse moves the virtual mouse stick with the controller's rotation
// (deg/s, sideways and up-down); full is the rotation, in degrees, that
// deflects it fully.
func (t *turnState) moveMouse(rates [2]float64, dt float64, full [2]float64) {
	for i, r := range rates {
		if !t.mouseSet.Turns[i] {
			t.mouse[i] = 0
			continue
		}
		if math.Abs(r) < mouseStill {
			r = 0
		}
		back := mouseSpring
		if t.mouseSet.Holds[i] {
			back = mouseForget
		}
		t.mouse[i] = math.Max(-1, math.Min(1, t.mouse[i]*math.Exp(-dt/back)+r*dt/full[i]))
	}
}

// mouseTurn: the virtual mouse stick's deflection as a turn amount (0-1).
// What is left after turning back, a few degrees, is not felt: the estimate
// is not that exact.
func (t *turnState) mouseTurn() float64 {
	return math.Max(0, math.Min(1, (math.Hypot(t.mouse[0], t.mouse[1])-0.08)/0.92))
}

// turnVector: the turn with its direction, for "push": each stick axis that
// turns the ship past its dead zone, and the virtual mouse stick scaled to
// its turn amount.
func (t *turnState) turnVector(pad dualsense.State) (v [6]float64) {
	const deadzone = 0.12
	for i, on := range t.sticks {
		if on {
			a := math.Max(0, (math.Abs(pad.Sticks[i])-deadzone)/(1-deadzone))
			v[i] = math.Copysign(math.Min(1, a), pad.Sticks[i])
		}
	}
	if h := math.Hypot(t.mouse[0], t.mouse[1]); h > 0 {
		m := t.mouseTurn()
		v[4], v[5] = t.mouse[0]/h*m, t.mouse[1]/h*m
	}
	return v
}

// resetMouse: the mouse reset key centres the virtual stick.
func (e *Engine) resetMouse() { e.turn.mouse = [2]float64{} }

// SetHeadlook: whether mouse headlook has the mouse, so gyro aim moves the
// view and leaves the ship's virtual stick alone.
func (e *Engine) SetHeadlook(on bool) { e.turn.headlook = on }

// stopTurning: not flying; no kick on the first frames back. The virtual
// mouse stick holds through menus and panels, as it does in Elite, and is
// centred when the ship is left or parked.
func (e *Engine) stopTurning(now time.Time, flying bool) {
	t := &e.turn
	if t.gyro > 0 || t.amount > 0 || t.vec != [6]float64{} || t.settled != [6]float64{} {
		t.gyro, t.amount, t.felt = 0, 0, 0
		t.vec, t.settled = [6]float64{}, [6]float64{}
		t.lastKick = now
	}
	t.phase, t.swell = 0, 0
	if !flying {
		e.resetMouse()
	}
}

// GyroStats returns the fastest controller rotation and whether the
// touchpad was touched since the last call.
func (e *Engine) GyroStats() (fastest float64, touched bool) {
	fastest, touched = e.turn.fastest, e.turn.touched
	e.turn.fastest, e.turn.touched = 0, false
	return fastest, touched
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
