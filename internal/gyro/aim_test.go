package gyro

import (
	"math"
	"math/rand"
	"sync"
	"testing"
	"time"
)

var testStart = time.Date(2026, 9, 29, 18, 0, 0, 0, time.UTC)

// recorder is a Mouse that adds up what it is asked to move.
type recorder struct {
	mu     sync.Mutex
	dx, dy int64
	moves  int
	refuse bool
}

func (m *recorder) Move(dx, dy int32) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dx += int64(dx)
	m.dy += int64(dy)
	m.moves++
	return !m.refuse
}

// take returns what moved since the last call.
func (m *recorder) take() (dx, dy int64, moves int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dx, dy, moves = m.dx, m.dy, m.moves
	m.dx, m.dy, m.moves = 0, 0, 0
	return dx, dy, moves
}

// rig feeds reports 4 ms apart (Stamp += 12000 at 3 MHz, At += 4 ms), the
// controller flat (accel 0, 1, 0), into an Aim with hold 0 and a recording
// mouse. N+1 reports give N intervals.
type rig struct {
	a *Aim
	m *recorder
	s Sample
}

func newRig(set Settings) *rig {
	m := &recorder{}
	r := &rig{a: New(m), m: m, s: Sample{Accel: [3]float64{0, 1, 0}, StampHz: 3e6, At: testStart}}
	r.a.SetSettings(set)
	r.a.SetHold(0)
	return r
}

// feed sends n reports turning at gyro deg/s.
func (r *rig) feed(n int, gyro [3]float64) {
	for range n {
		r.s.Gyro = gyro
		r.next()
	}
}

// feedNoisy sends n reports at gyro deg/s plus uniform noise of gyroPP
// deg/s and accelPP g peak to peak on every axis.
func (r *rig) feedNoisy(rng *rand.Rand, n int, gyro [3]float64, gyroPP, accelPP float64) {
	for range n {
		for i := range 3 {
			r.s.Gyro[i] = gyro[i] + (rng.Float64()-0.5)*gyroPP
			r.s.Accel[i] = [3]float64{0, 1, 0}[i] + (rng.Float64()-0.5)*accelPP
		}
		r.next()
	}
	r.s.Accel = [3]float64{0, 1, 0}
}

func (r *rig) next() {
	r.a.Feed(r.s)
	r.s.Stamp += 12000
	r.s.At = r.s.At.Add(4 * time.Millisecond)
}

// dsxMode is DSX's formula with the calibration off, so a steady slow turn
// is not taken for drift.
func dsxMode() Settings {
	s := DefaultSettings()
	s.AutoCalibrate = false
	return s
}

func exactMode() Settings {
	s := dsxMode()
	s.Exact = true
	return s
}

func TestExactLinear(t *testing.T) {
	r := newRig(exactMode())
	r.feed(251, [3]float64{0, 10, 0})
	dx, dy, _ := r.m.take()
	if want := int64(math.Trunc(-10 * 1.0 * DSXCountsPerDegree)); dx != want || dy != 0 || want != -225 {
		t.Fatalf("10 deg/s yaw for 1 s: %d %d, want %d 0", dx, dy, want)
	}
	if st := r.a.Status(); st.Sent != [2]int64{dx, dy} || st.Reports != 251 || st.MoveFailed != 0 || !st.StampLive {
		t.Fatalf("status: %+v", st)
	}

	r.m.refuse = true
	r.feed(10, [3]float64{0, 10, 0})
	if st := r.a.Status(); st.MoveFailed != 9 && st.MoveFailed != 10 {
		t.Fatalf("refused moves: %d", st.MoveFailed)
	}
}

func TestSigns(t *testing.T) {
	turn := func(set Settings, gyro [3]float64) (int64, int64) {
		r := newRig(set)
		r.feed(251, gyro)
		dx, dy, _ := r.m.take()
		return dx, dy
	}
	if dx, dy := turn(exactMode(), [3]float64{0, 10, 0}); dx >= 0 || dy != 0 {
		t.Errorf("+yaw (turning left): %d %d, want the mouse left", dx, dy)
	}
	if dx, dy := turn(exactMode(), [3]float64{10, 0, 0}); dy >= 0 || dx != 0 {
		t.Errorf("+pitch (tilting up): %d %d, want the mouse up", dx, dy)
	}
	yaw, _ := turn(exactMode(), [3]float64{0, 10, 0})
	roll, dy := turn(exactMode(), [3]float64{0, 0, 10})
	if want := int64(math.Trunc(0.6 * 10 * -DSXCountsPerDegree)); roll != want || roll >= 0 || dy != 0 {
		t.Errorf("+roll: %d %d, want %d (0.6 of the yaw's %d)", roll, dy, want, yaw)
	}
	noRoll := exactMode()
	noRoll.RollMix = 0
	if dx, dy := turn(noRoll, [3]float64{0, 0, 10}); dx != 0 || dy != 0 {
		t.Errorf("roll with roll mix 0: %d %d", dx, dy)
	}
}

func TestDSXFloors(t *testing.T) {
	r := newRig(dsxMode())
	r.feed(2501, [3]float64{0, 1, 0})
	if dx, dy, n := r.m.take(); dx != 0 || dy != 0 || n != 0 {
		t.Errorf("1 deg/s for 10 s moved %d %d", dx, dy)
	}
	r.feed(2500, [3]float64{0.3, 0.3, 0.3})
	if dx, dy, n := r.m.take(); dx != 0 || dy != 0 || n != 0 {
		t.Errorf("0.3 deg/s on every axis moved %d %d", dx, dy)
	}

	r = newRig(exactMode())
	r.feed(2501, [3]float64{0, 0.5, 0})
	if dx, dy, _ := r.m.take(); dx != -112 || dy != 0 {
		t.Errorf("exact, 0.5 deg/s for 10 s: %d %d, want -112 0", dx, dy)
	}
}

// dsxReference is DSX 3.2.0's remainder step for a steady sideways turn, as
// read from DSX.dll, written apart from Aim: drop under 0.1 count, add,
// truncate, keep 94% of the leftover.
func dsxReference(degPerSec float64, reports int) int64 {
	x := -degPerSec * 0.004 * DSXCountsPerDegree
	if math.Abs(x) < 0.1 {
		x = 0
	}
	var rem float64
	var sent int64
	for range reports {
		rem += x
		pos := math.Trunc(rem)
		rem = (rem - pos) * 0.94
		sent += int64(pos)
	}
	return sent
}

func TestDSXRemainder(t *testing.T) {
	r := newRig(dsxMode())
	r.feed(251, [3]float64{0, 10, 0})
	dx, _, _ := r.m.take()
	if want := dsxReference(10, 250); dx != want {
		t.Errorf("10 deg/s for 1 s: %d, DSX sends %d", dx, want)
	}
	if share := float64(dx) / -225; share < 0.95 || share > 0.98 {
		t.Errorf("10 deg/s for 1 s delivered %.1f%% of 225", 100*share)
	}

	r = newRig(exactMode())
	r.feed(251, [3]float64{0, 10, 0})
	if dx, _, _ := r.m.take(); dx < -226 || dx > -224 {
		t.Errorf("exact, 10 deg/s for 1 s: %d, want 225 within 1", dx)
	}
}

func TestSymmetry(t *testing.T) {
	turns := [][3]float64{{0, 1.5, 0}, {-3, 7, 2}, {12, -40, 5}, {0.5, 0.2, -20}, {-60, 0, 0}}
	for _, set := range []Settings{dsxMode(), exactMode()} {
		sum := func(sign float64) (int64, int64) {
			r := newRig(set)
			for _, g := range turns {
				r.feed(200, [3]float64{sign * g[0], sign * g[1], sign * g[2]})
			}
			dx, dy, _ := r.m.take()
			return dx, dy
		}
		px, py := sum(1)
		nx, ny := sum(-1)
		if px != -nx || py != -ny || px == 0 || py == 0 {
			t.Errorf("exact %v: %d %d, negated %d %d", set.Exact, px, py, nx, ny)
		}
	}
}

// roundTrip turns 10 degrees left at 5 deg/s and back at 40 deg/s.
func roundTrip(set Settings) int64 {
	r := newRig(set)
	r.feed(501, [3]float64{0, 5, 0})  // 500 intervals: +10 deg
	r.feed(62, [3]float64{0, -40, 0}) // -9.92 deg
	r.feed(1, [3]float64{0, -20, 0})  // -0.08 deg
	dx, _, _ := r.m.take()
	return dx
}

func TestRoundTrip(t *testing.T) {
	if net := roundTrip(exactMode()); net < -1 || net > 1 {
		t.Errorf("exact: 10 degrees there and back left %d counts", net)
	}
	// DSX loses more of the slow leg (left, negative counts) than of the
	// fast one, so the mouse ends up right of where it started (12 counts
	// here). That is DSX's feel, kept on purpose; this notices a change.
	if net := roundTrip(dsxMode()); net <= 0 || net > 30 {
		t.Errorf("dsx: 10 degrees there and back left %d counts, want the documented loss (1 to 30)", net)
	}
}

func TestHold(t *testing.T) {
	for bit := HoldStart; bit <= HoldDemo; bit <<= 1 {
		r := newRig(dsxMode())
		r.a.SetHold(bit)
		r.feed(100, [3]float64{20, 30, 10})
		if _, _, n := r.m.take(); n != 0 {
			t.Errorf("hold %b moved the mouse", bit)
		}
	}
	if New(&recorder{}).hold != HoldStart {
		t.Error("New must start held")
	}

	// no burst on release: the first report moves one report's worth
	r := newRig(dsxMode())
	r.feed(50, [3]float64{0, 30, 0})
	r.a.SetHold(HoldMenu)
	r.feed(100, [3]float64{0, 30, 0})
	r.m.take()
	r.a.SetHold(0)
	r.feed(1, [3]float64{0, 30, 0})
	dx, _, _ := r.m.take()
	if worth := 30 * 0.004 * DSXCountsPerDegree; math.Abs(float64(dx)) > worth {
		t.Errorf("first report after the hold: %d counts, one report is worth %.1f", dx, worth)
	}

	// a finger on the touchpad: no movement, and each lift counted
	r.s.Touch = true
	r.feed(50, [3]float64{0, 30, 0})
	if _, _, n := r.m.take(); n != 0 {
		t.Error("moved while touching")
	}
	r.s.Touch = false
	r.feed(5, [3]float64{0, 30, 0})
	r.s.Touch = true
	r.feed(5, [3]float64{})
	r.s.Touch = false
	r.feed(5, [3]float64{0, 30, 0})
	if st := r.a.Status(); st.TouchLifts != 2 {
		t.Errorf("touch lifts: %d, want 2", st.TouchLifts)
	}
}

func TestEmptyReports(t *testing.T) {
	// 100 counts per degree: 1.5 deg/s leaves 0.6 of a count per report
	set := exactMode()
	set.CountsPerDeg = [2]float64{100, 100}
	r := newRig(set)
	r.feed(1, [3]float64{})
	r.feed(1, [3]float64{0, -1.5, 0})
	r.s.Accel = [3]float64{}
	for i := 1; i <= 10; i++ {
		r.feed(1, [3]float64{})
		if st := r.a.Status(); st.ZeroFor != time.Duration(i)*4*time.Millisecond {
			t.Fatalf("after %d empty reports ZeroFor is %v", i, st.ZeroFor)
		}
	}
	r.s.Touch = true
	r.feed(10, [3]float64{})
	if st := r.a.Status(); st.ZeroFor != 40*time.Millisecond {
		t.Errorf("touch reports counted: ZeroFor %v", st.ZeroFor)
	}
	r.s.Touch = false
	r.s.Accel = [3]float64{0, 1, 0}
	r.feed(1, [3]float64{0, -1.5, 0})
	if dx, _, n := r.m.take(); n != 0 {
		t.Errorf("the remainder from before the empty reports was kept: moved %d", dx)
	}
	if st := r.a.Status(); st.ZeroFor != 0 {
		t.Errorf("ZeroFor after motion: %v", st.ZeroFor)
	}

	// After a short empty run (20 ms, under the 50 ms gap) the next report
	// covers one report period, 0.6 of a count, not the whole run.
	r.feed(1, [3]float64{0, -1.5, 0})
	r.m.take()
	r.s.Accel = [3]float64{}
	r.feed(5, [3]float64{})
	r.s.Accel = [3]float64{0, 1, 0}
	r.feed(1, [3]float64{0, -1.5, 0})
	if dx, _, n := r.m.take(); n != 0 {
		t.Errorf("the first report after 20 ms of empty ones moved %d", dx)
	}
}

func TestSettingsSwap(t *testing.T) {
	r := newRig(exactMode())
	r.feed(101, [3]float64{0, -10, 0})
	if dx, _, _ := r.m.take(); dx < 89 || dx > 91 {
		t.Fatalf("before: %d", dx)
	}
	fast := exactMode()
	fast.CountsPerDeg = [2]float64{10 * DSXCountsPerDegree, DSXCountsPerDegree}
	r.a.SetSettings(fast)
	r.feed(1, [3]float64{0, -10, 0})
	if dx, _, _ := r.m.take(); dx < 9 {
		t.Errorf("the next report moved %d, want 9 or more", dx)
	}

	if r.a.Status().Calibrated {
		t.Fatal("calibrated without a bias")
	}
	r.a.SetBias([3]float64{0.1, -0.2, 0.3})
	if st := r.a.Status(); !st.Calibrated || st.Bias != [3]float64{0.1, -0.2, 0.3} {
		t.Errorf("SetBias: %+v", st)
	}
}

func TestRace(t *testing.T) {
	var a *Aim
	// Move asks for the status: it would deadlock if Feed held the lock
	a = New(MouseFunc(func(dx, dy int32) bool { return a.Status().Reports > 0 }))
	done := make(chan struct{})
	go func() {
		defer close(done)
		s := Sample{Accel: [3]float64{0, 1, 0}, StampHz: 3e6, At: testStart}
		for i := range 10000 {
			s.Gyro = [3]float64{float64(i%7) - 3, 40, -5}
			s.Touch = i%100 < 5
			s.Stamp += 12000
			s.At = s.At.Add(4 * time.Millisecond)
			a.Feed(s)
		}
	}()
	for i := range 10000 {
		set := DefaultSettings()
		set.Exact = i%2 == 0
		a.SetSettings(set)
		a.SetHold(Hold(i % 3))
		_ = a.Status()
		if i%1000 == 0 {
			a.Calibrate(20 * time.Millisecond)
		}
	}
	<-done
	if st := a.Status(); st.Reports != 10000 {
		t.Errorf("reports: %d", st.Reports)
	}
}
