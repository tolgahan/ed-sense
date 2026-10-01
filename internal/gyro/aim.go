// Package gyro is EDSense's own gyro aim: it turns the controller's
// rotation into relative mouse movement for Elite, the way DSX's motion to
// mouse does, and learns the gyro's drift while the controller lies still.
// A backend feeds it one Sample per input report; the app sets the
// settings and the hold, and reads Status each tick. It logs nothing.
package gyro

import (
	"math"
	"sync"
	"time"
)

// DSXCountsPerDegree: mouse counts per degree with DSX's bundled profile
// (motion to mouse, angles to pixels 16201, sensitivity 0.5).
const DSXCountsPerDegree = 16201.0 / 360 * 0.5 // 22.5014

// DSX 3.2.0's motion to mouse, as read from DSX.dll.
const (
	dsxFloor     = 0.375 // deg/s: each axis under this is dropped (ApplyGyroNoiseFloor)
	dsxMinCounts = 0.1   // a report's movement under this, per mouse axis, is dropped
	dsxDecay     = 0.94  // share of the leftover fraction of a count kept for the next report
	// exactFloor drops only the noise that calibration leaves, so slow
	// movement gets through
	exactFloor = 0.1
)

// Settings is how rotation becomes mouse movement.
type Settings struct {
	CountsPerDeg  [2]float64 // mouse counts per degree, sideways and up-down
	RollMix       float64    // share of roll added to sideways
	Exact         bool       // false: DSX's low-speed handling
	AutoCalibrate bool       // learn the drift whenever the controller lies still
}

// DefaultSettings match DSX's bundled profile.
func DefaultSettings() Settings {
	return Settings{
		CountsPerDeg:  [2]float64{DSXCountsPerDegree, DSXCountsPerDegree},
		RollMix:       0.6,
		AutoCalibrate: true,
	}
}

// Hold says why the gyro does not aim; 0 means it aims.
type Hold uint32

const (
	HoldStart  Hold = 1 << iota // not started yet, or stopped (New starts here)
	HoldOff                     // gyro_by dsx, EDSense cannot aim, or gyro_aim off
	HoldElite                   // Elite not running or not in front
	HoldPaused                  // Pause effects
	HoldMenu                    // a menu with the gyro off
	HoldDSX                     // DSX not answering, or not yet told to stop its mouse
	HoldDemo                    // the tray demo plays
)

// Status is what the app reads each tick to log and to decide.
type Status struct {
	Reports      int           // reports fed since New
	ZeroFor      time.Duration // how long motion has been all zero, touch reports left out
	StampLive    bool          // dt comes from the sensor clock
	Period       time.Duration // measured report period
	Bias         [3]float64    // deg/s
	Calibrated   bool          // a bias was learned or loaded
	Learned      int           // first bias learned from stillness (no saved one), for the log
	Manual       int           // manual calibrations finished
	ManualOK     bool          // the last one worked
	ManualNoData bool          // the last one failed because no motion came (else the controller moved)
	Calibrating  bool          // a manual calibration waits for motion or runs
	TouchLifts   int           // a finger left the touchpad
	Sent         [2]int64      // counts sent, x and y
	MoveFailed   int           // Mouse.Move returned false
}

// Aim turns motion reports into mouse movement. Feed runs on the backend's
// reading goroutine and everything else on the app's, so one mutex guards
// all of it, and Mouse.Move is called after unlocking.
type Aim struct {
	mu    sync.Mutex
	mouse Mouse
	set   Settings
	hold  Hold
	dsx   bool // DSX's gyro aims meanwhile
	clock clock
	cal   calibration

	rem       [2]float64 // fraction of a count not sent yet, x and y
	wasAiming bool

	reports    int
	lastAt     time.Time
	period     time.Duration
	zeroFor    time.Duration
	touching   bool
	touchLifts int
	sent       [2]int64
	moveFailed int
}

// New returns an Aim that holds (HoldStart), with DefaultSettings and no
// bias yet.
func New(m Mouse) *Aim {
	return &Aim{
		mouse:  m,
		set:    DefaultSettings(),
		hold:   HoldStart,
		cal:    newCalibration(),
		period: 4 * time.Millisecond, // the DualSense's 250 Hz until measured
	}
}

// SetSettings applies from the next report on.
func (a *Aim) SetSettings(s Settings) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.set = s
}

// SetHold stops the aim for the reasons in h, or lets it aim with 0.
func (a *Aim) SetHold(h Hold) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.hold = h
}

// SetDSXAims tells whether DSX's own gyro aims while this one holds. A slow
// steady turn may then be the player's, so the drift is learned as
// carefully as while this one aims.
func (a *Aim) SetDSXAims(on bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dsx = on
}

// SetBias sets a saved bias, in deg/s, which counts as calibrated.
func (a *Aim) SetBias(b [3]float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cal.set(b)
}

// Forget drops the bias, so the drift is learned again from the next
// stillness (Status.Learned counts it). A manual calibration under way
// goes on; Feed has started it again on the new source's first report.
func (a *Aim) Forget() {
	a.mu.Lock()
	defer a.mu.Unlock()
	c := &a.cal
	c.bias, c.calibrated, c.win, c.steady, c.start = [3]float64{}, false, window{}, 0, [3]float64{}
}

// Calibrate starts a manual calibration: the bias becomes the mean of the
// next d of reports if the controller stays still meanwhile, and nothing
// moves while it runs. Status.Manual counts it when it ends.
func (a *Aim) Calibrate(d time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cal.startManual(d.Seconds())
}

// Status returns the aim's state as it is now.
func (a *Aim) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return Status{
		Reports:      a.reports,
		ZeroFor:      a.zeroFor,
		StampLive:    a.clock.running(),
		Period:       a.period,
		Bias:         a.cal.bias,
		Calibrated:   a.cal.calibrated,
		Learned:      a.cal.learned,
		Manual:       a.cal.manuals,
		ManualOK:     a.cal.manualOK,
		ManualNoData: a.cal.manualNoData,
		Calibrating:  a.cal.busy(),
		TouchLifts:   a.touchLifts,
		Sent:         a.sent,
		MoveFailed:   a.moveFailed,
	}
}

// Feed takes one report, on the backend's goroutine, and moves the mouse
// when the whole-count movement is not zero. It must stay quick: it runs
// once per report.
func (a *Aim) Feed(s Sample) {
	a.mu.Lock()
	a.count(s)
	// a report on another clock comes from another motion source: the
	// stillness and a manual calibration under way start again on it, so
	// one source's reports never set the drift of the other
	if a.clock.have && s.StampHz != a.clock.hz {
		a.cal.newSource()
	}
	a.cal.expire(s.At)
	// The clock steps on empty reports too, so the first report after a
	// short pause covers one report period.
	dt, ok := a.clock.step(s, a.period)
	if s.empty() {
		if !s.Touch {
			a.zeroFor += a.period
		}
		a.rem = [2]float64{}
		a.mu.Unlock()
		return
	}
	a.zeroFor = 0
	if !ok {
		a.rem = [2]float64{} // as DSX does after a gap
		a.mu.Unlock()
		return
	}
	// The calibration sees every report with motion. It is guarded while
	// this gyro or DSX's aims, touch or not: a finger on the touchpad is how
	// a player re-centres the controller in flight, and that slow turn is
	// not drift.
	manual := a.cal.busy()
	a.cal.add(s, dt, a.hold == 0 || a.dsx, a.set.AutoCalibrate)
	aiming := a.hold == 0 && !s.Touch && !manual
	if aiming != a.wasAiming {
		a.rem = [2]float64{} // no burst when aiming starts or stops
		a.wasAiming = aiming
	}
	if !aiming {
		a.mu.Unlock()
		return
	}
	dx, dy := a.move(s.Gyro, dt)
	a.sent[0] += int64(dx)
	a.sent[1] += int64(dy)
	a.mu.Unlock()
	if (dx != 0 || dy != 0) && !a.mouse.Move(dx, dy) {
		a.mu.Lock()
		a.moveFailed++
		a.mu.Unlock()
	}
}

// count keeps the report statistics: the report period (an average of the
// arrival gaps under 50 ms) and the touchpad lifts.
func (a *Aim) count(s Sample) {
	a.reports++
	if !a.lastAt.IsZero() {
		if d := s.At.Sub(a.lastAt); d >= 0 && d < maxGap {
			a.period += time.Duration(0.02 * float64(d-a.period))
		}
	}
	a.lastAt = s.At
	if a.touching && !s.Touch {
		a.touchLifts++
	}
	a.touching = s.Touch
}

// move turns one report's rotation into whole mouse counts and keeps the
// fraction for the next report. With Exact off it matches DSX 3.2.0's
// motion to mouse: the per-axis floor, the per-report floor, and a leftover
// that fades by 6% per report, which together lose slow movement (at
// 250 Hz nothing under about 1.1 deg/s, 82% at 2 deg/s, 97% at 10).
func (a *Aim) move(gyro [3]float64, dt float64) (dx, dy int32) {
	floor := dsxFloor
	if a.set.Exact {
		floor = exactFloor
	}
	var w [3]float64
	for i := range w {
		w[i] = gyro[i] - a.cal.bias[i]
		if math.Abs(w[i]) < floor {
			w[i] = 0
		}
	}
	// Turning left (+yaw) moves the mouse left, and tilting the controller
	// up (+pitch) moves it up.
	v := [2]float64{
		-(w[1] + a.set.RollMix*w[2]) * dt * a.set.CountsPerDeg[0],
		-w[0] * dt * a.set.CountsPerDeg[1],
	}
	var out [2]int32
	for i := range v {
		if !a.set.Exact && math.Abs(v[i]) < dsxMinCounts {
			v[i] = 0
		}
		a.rem[i] += v[i]
		whole := math.Trunc(a.rem[i])
		a.rem[i] -= whole
		if !a.set.Exact {
			a.rem[i] *= dsxDecay
		}
		out[i] = int32(whole)
	}
	return out[0], out[1]
}
