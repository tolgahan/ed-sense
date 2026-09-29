package gyro

import (
	"math"
	"time"
)

// A gyro at rest still reads a little rotation, its drift or bias, and with
// Elite's mouse decay off any drift left in the gyro keeps turning the
// ship. So the bias is learned whenever the controller lies still, and on
// request (Calibrate).
//
// The stillness test follows an idea from GamepadMotionHelpers by Julian
// "Jibb" Smart (MIT): the controller is still while every axis's peak to
// peak range over a growing window stays under a threshold, and the
// threshold's multiplier climbs slowly from 1 to 2 while nothing still is
// found, so a noisy controller is still calibrated in the end. The guard
// while the gyro aims and the cap on the bias are ours.
//
// Known weakness: a slow steady yaw with the controller flat looks like
// drift, and the accelerometer cannot see it. The cap, the guard and the
// fact that the controller mostly lies still in menus and loading screens,
// where the gyro holds anyway, keep that small.

const (
	stillGyro       = 1.2   // deg/s peak to peak per axis (JoyShockMapper's auto calibration value)
	stillAccel      = 0.015 // g peak to peak per axis
	stillMinN       = 10    // reports before a window is judged
	stillMinT       = 0.5   // s before a window is judged
	stillNeed       = 2.0   // s of stillness before the bias moves
	stillNeedAiming = 4.0   // s, while the gyro may aim and a bias is known
	stepAiming      = 0.25  // deg/s: the most one still stretch may move the bias while the gyro may aim
	biasMax         = 3.0   // deg/s: a larger mean is a slow turn, not drift
	blendTau        = 0.5   // s
	manualMoved     = 5.0   // deg/s peak to peak on any axis fails a manual calibration
	scaleRate       = 0.1   // the threshold multiplier's climb per second, and its drop
)

// window gathers reports while the controller may be still.
type window struct {
	n                  int
	t                  float64 // s
	gyroMin, gyroMax   [3]float64
	gyroSum            [3]float64
	accelMin, accelMax [3]float64
}

func (w *window) add(s Sample, dt float64) {
	if w.n == 0 {
		w.gyroMin, w.gyroMax = s.Gyro, s.Gyro
		w.accelMin, w.accelMax = s.Accel, s.Accel
	}
	for i := range 3 {
		w.gyroMin[i] = math.Min(w.gyroMin[i], s.Gyro[i])
		w.gyroMax[i] = math.Max(w.gyroMax[i], s.Gyro[i])
		w.gyroSum[i] += s.Gyro[i]
		w.accelMin[i] = math.Min(w.accelMin[i], s.Accel[i])
		w.accelMax[i] = math.Max(w.accelMax[i], s.Accel[i])
	}
	w.n++
	w.t += dt
}

// still: every axis stays within the thresholds times scale. A backend with
// no accelerometer sends zeros, which never move.
func (w *window) still(scale float64) bool {
	for i := range 3 {
		if w.gyroMax[i]-w.gyroMin[i] > stillGyro*scale || w.accelMax[i]-w.accelMin[i] > stillAccel*scale {
			return false
		}
	}
	return true
}

func (w *window) mean() (m [3]float64) {
	for i := range m {
		m[i] = w.gyroSum[i] / float64(w.n)
	}
	return m
}

type calibration struct {
	bias       [3]float64 // deg/s
	calibrated bool
	learned    int // first bias learned from stillness

	win    window
	scale  float64    // the stillness threshold's multiplier, 1-2
	steady float64    // s the current still stretch has moved the bias
	start  [3]float64 // the bias when that stretch began, or when the guard came on

	manualFor     float64 // s asked for
	manualPending bool
	manualRunning bool
	manualAt      time.Time // the first report after it was asked for
	manual        window
	manuals       int // manual calibrations finished
	manualOK      bool
	manualNoData  bool // the last one failed because no motion came
}

func newCalibration() calibration { return calibration{scale: 1} }

// busy: a manual calibration waits for reports or runs.
func (c *calibration) busy() bool { return c.manualPending || c.manualRunning }

// startManual: the bias becomes the mean of the next sec seconds of
// reports, if the controller stays still meanwhile.
func (c *calibration) startManual(sec float64) {
	c.manualFor, c.manualPending, c.manualRunning = sec, true, false
	c.manualAt = time.Time{}
}

// manualSlack: a manual calibration that has not finished this long after
// its time is up fails, so it never waits for motion and then runs on
// whatever comes later (the controller back in the player's hands).
const manualSlack = 1.5 // s

// expire ends a manual calibration that ran out of time, counting every
// report, those without motion too.
func (c *calibration) expire(at time.Time) {
	if !c.busy() {
		return
	}
	if c.manualAt.IsZero() {
		c.manualAt = at
		return
	}
	if at.Sub(c.manualAt).Seconds() <= c.manualFor+manualSlack {
		return
	}
	c.manuals++
	c.manualOK, c.manualNoData = false, c.manualPending || c.manual.n == 0
	c.manualPending, c.manualRunning = false, false
	c.win, c.steady = window{}, 0
}

// set takes a saved bias.
func (c *calibration) set(b [3]float64) {
	c.bias, c.calibrated, c.steady = b, true, 0
}

// add takes one report that has motion. guarded: the gyro may aim, so a
// slow steady turn may be the player's, and the bias moves later, slower
// and only so far.
func (c *calibration) add(s Sample, dt float64, guarded, auto bool) {
	if c.manualPending {
		c.manualPending, c.manualRunning, c.manual = false, true, window{}
	}
	if c.manualRunning {
		c.addManual(s, dt)
		return
	}
	if !auto {
		c.win, c.steady = window{}, 0 // so turning it on starts afresh
		return
	}
	c.win.add(s, dt)
	if c.win.n < stillMinN || c.win.t < stillMinT {
		c.climb(dt)
		return
	}
	if !c.win.still(c.scale) {
		if c.steady > 0 {
			// the stretch just used ended in movement: be stricter
			c.scale = math.Max(c.scale-scaleRate, 1)
		} else {
			c.climb(dt)
		}
		c.win, c.steady = window{}, 0
		return
	}
	need := stillNeed
	if guarded && c.calibrated {
		need = stillNeedAiming
	}
	if c.win.t < need {
		c.climb(dt)
		return
	}
	// The window is not reset while still, so this is the mean of the
	// whole still stretch.
	mean := c.win.mean()
	for _, m := range mean {
		if math.Abs(m) > biasMax {
			return
		}
	}
	if !c.calibrated {
		c.bias, c.start, c.calibrated = mean, mean, true
		c.learned++
		c.steady = dt
		return
	}
	// The guard counts from where the bias was when the gyro began to aim,
	// so a bias learned while it held is kept when aiming starts.
	if c.steady == 0 || !guarded {
		c.start = c.bias
	}
	c.steady += dt
	target := mean
	if guarded {
		for i := range target {
			target[i] = clamp(target[i], c.start[i]-stepAiming, c.start[i]+stepAiming)
		}
	}
	f := 1 - math.Exp(-dt/blendTau)
	for i := range c.bias {
		c.bias[i] += (target[i] - c.bias[i]) * f
	}
}

func (c *calibration) climb(dt float64) { c.scale = math.Min(c.scale+scaleRate*dt, 2) }

func (c *calibration) addManual(s Sample, dt float64) {
	c.manual.add(s, dt)
	if c.manual.t+1e-6 < c.manualFor { // sums of dt fall a hair short
		return
	}
	// a steady turn has no spread, so a mean too large for drift fails too
	ok := true
	mean := c.manual.mean()
	for i := range 3 {
		if c.manual.gyroMax[i]-c.manual.gyroMin[i] > manualMoved || math.Abs(mean[i]) > biasMax {
			ok = false
		}
	}
	if ok {
		c.bias, c.calibrated = mean, true
	}
	c.manuals++
	c.manualOK, c.manualNoData, c.manualRunning = ok, false, false
	c.win, c.steady = window{}, 0
}
