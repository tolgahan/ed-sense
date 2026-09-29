package gyro

import (
	"math"
	"math/rand"
	"testing"
	"time"
)

var restBias = [3]float64{0.3, 0.8, -0.2}

func near(a, b [3]float64, tol float64) bool {
	for i := range a {
		if math.Abs(a[i]-b[i]) > tol {
			return false
		}
	}
	return true
}

func TestCalibration(t *testing.T) {
	exact := DefaultSettings()
	exact.Exact = true
	for _, set := range []Settings{DefaultSettings(), exact} {
		rng := rand.New(rand.NewSource(1))
		r := newRig(set)
		r.a.SetHold(HoldMenu)
		r.feedNoisy(rng, 626, restBias, 0.3, 0.004) // 2.5 s
		st := r.a.Status()
		if !st.Calibrated || st.Learned != 1 || !near(st.Bias, restBias, 0.05) {
			t.Fatalf("exact %v, held at rest for 2.5 s: %+v", set.Exact, st)
		}

		r.a.SetHold(0)
		r.feedNoisy(rng, 2500, restBias, 0.3, 0.004) // 10 s
		dx, dy, _ := r.m.take()
		switch {
		case !set.Exact && (dx != 0 || dy != 0):
			t.Errorf("dsx, aiming at rest for 10 s: moved %d %d", dx, dy)
		case set.Exact && (math.Abs(float64(dx)) >= 5 || math.Abs(float64(dy)) >= 5):
			t.Errorf("exact, aiming at rest for 10 s: moved %d %d", dx, dy)
		}
		if st := r.a.Status(); st.Learned != 1 {
			t.Errorf("learned %d times", st.Learned)
		}
	}
}

// A slow steady yaw looks like drift. While the gyro aims, one still stretch
// moves the bias by 0.25 deg/s at most, also with a finger on the touchpad
// (the player re-centring the controller); while it holds, the bias follows
// the yaw all the way (the known weakness, kept visible here).
func TestCalibrationGuard(t *testing.T) {
	for _, c := range []struct {
		name        string
		held, touch bool
	}{
		{"aiming", false, false},
		{"touching", false, true},
		{"held", true, false},
		{"DSX aims", false, false},
	} {
		rng := rand.New(rand.NewSource(2))
		r := newRig(DefaultSettings())
		r.a.SetBias([3]float64{})
		if c.held {
			r.a.SetHold(HoldMenu)
		}
		if c.name == "DSX aims" {
			r.a.SetHold(HoldOff)
			r.a.SetDSXAims(true)
		}
		r.s.Touch = c.touch
		r.feedNoisy(rng, 2501, [3]float64{0, 0.5, 0}, 0.1, 0) // 10 s
		b := r.a.Status().Bias[1]
		switch {
		case !c.held && (b <= 0 || b > stepAiming+1e-9):
			t.Errorf("%s: the bias moved to %.3f, want at most %.2f", c.name, b, stepAiming)
		case c.held && math.Abs(b-0.5) > 0.05:
			t.Errorf("%s: the bias is %.3f, want 0.5", c.name, b)
		}
	}
}

// A bias learned while the gyro holds stays when it starts to aim with the
// controller still at rest: the guard counts from there.
func TestCalibrationHeldThenAiming(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	set := DefaultSettings()
	set.Exact = true
	r := newRig(set)
	r.a.SetBias([3]float64{}) // a saved bias that no longer fits
	r.a.SetHold(HoldMenu)
	r.feedNoisy(rng, 2500, restBias, 0.3, 0.004) // 10 s
	if st := r.a.Status(); !near(st.Bias, restBias, 0.05) {
		t.Fatalf("held at rest for 10 s: bias %v, want %v", st.Bias, restBias)
	}
	r.a.SetHold(0)
	r.feedNoisy(rng, 2500, restBias, 0.3, 0.004)
	if st := r.a.Status(); !near(st.Bias, restBias, 0.05) {
		t.Errorf("then aiming at rest for 10 s: bias %v, want %v", st.Bias, restBias)
	}
	if dx, dy, _ := r.m.take(); math.Abs(float64(dx)) >= 5 || math.Abs(float64(dy)) >= 5 {
		t.Errorf("exact, aiming at rest for 10 s: moved %d %d", dx, dy)
	}
}

func TestAutoCalibrateOff(t *testing.T) {
	rng := rand.New(rand.NewSource(6))
	r := newRig(dsxMode())
	r.a.SetHold(HoldMenu)
	r.feedNoisy(rng, 1000, restBias, 0.3, 0.004) // 4 s
	if st := r.a.Status(); st.Calibrated || st.Learned != 0 || st.Bias != [3]float64{} {
		t.Errorf("auto calibration off, 4 s at rest: %+v", st)
	}
}

func TestCalibrationCap(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	r := newRig(DefaultSettings())
	r.a.SetHold(HoldMenu)
	r.feedNoisy(rng, 1500, [3]float64{0, 4, 0}, 0.3, 0.004) // 6 s at 4 deg/s
	if st := r.a.Status(); st.Calibrated || st.Learned != 0 || st.Bias != [3]float64{} {
		t.Errorf("4 deg/s taken for drift: %+v", st)
	}
	r.a.SetBias(restBias)
	r.feedNoisy(rng, 1500, [3]float64{0, 4, 0}, 0.3, 0.004)
	if st := r.a.Status(); st.Bias != restBias {
		t.Errorf("4 deg/s moved a known bias to %v", st.Bias)
	}
}

func TestManualCalibration(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	r := newRig(exactMode())
	r.feedNoisy(rng, 10, restBias, 0.3, 0.004)
	r.m.take()

	r.a.Calibrate(2 * time.Second)
	r.feedNoisy(rng, 500, restBias, 0.3, 0.004)
	st := r.a.Status()
	if st.Manual != 1 || !st.ManualOK || !st.Calibrated || !near(st.Bias, restBias, 0.02) || st.Learned != 0 {
		t.Fatalf("2 s still: %+v", st)
	}
	if _, _, n := r.m.take(); n != 0 {
		t.Error("the mouse moved during the calibration")
	}

	r.a.Calibrate(2 * time.Second)
	r.feedNoisy(rng, 200, restBias, 0.3, 0.004)
	r.feed(25, [3]float64{restBias[0], restBias[1] + 10, restBias[2]}) // a 10 deg/s swing
	r.feedNoisy(rng, 275, restBias, 0.3, 0.004)
	if got := r.a.Status(); got.Manual != 2 || got.ManualOK || got.Bias != st.Bias {
		t.Errorf("moved during the calibration: %+v", got)
	}
	if _, _, n := r.m.take(); n != 0 {
		t.Error("the mouse moved during the calibration")
	}

	// a steady turn has no spread, but it is too fast to be drift
	r.a.Calibrate(2 * time.Second)
	r.feed(500, [3]float64{0, 20, 0})
	if got := r.a.Status(); got.Manual != 3 || got.ManualOK || got.Bias != st.Bias {
		t.Errorf("a steady 20 deg/s turn taken for drift: %+v", got)
	}
}

// A manual calibration that gets no motion (DSX zeroes it in a menu) ends
// with NoData after its time and a little more, and one that stalls midway
// fails too: neither waits to run on what comes later.
func TestManualDeadline(t *testing.T) {
	r := newRig(exactMode())
	r.a.Calibrate(2 * time.Second)
	r.s.Accel = [3]float64{}
	r.feed(1000, [3]float64{}) // 4 s of empty reports
	st := r.a.Status()
	if st.Manual != 1 || st.ManualOK || !st.ManualNoData || st.Calibrating || st.Calibrated {
		t.Fatalf("no motion for 4 s: %+v", st)
	}
	r.s.Accel = [3]float64{0, 1, 0}
	r.feed(500, restBias) // motion comes later: nothing is pending
	if st := r.a.Status(); st.Manual != 1 || st.Bias == restBias && st.Calibrated && st.Learned == 0 {
		t.Fatalf("a stale request ran on later motion: %+v", st)
	}

	r = newRig(exactMode())
	r.a.Calibrate(2 * time.Second)
	r.feed(100, restBias) // 0.4 s, then the motion stops
	r.s.Accel = [3]float64{}
	r.feed(1000, [3]float64{})
	if st := r.a.Status(); st.Manual != 1 || st.ManualOK || st.ManualNoData || st.Calibrating {
		t.Fatalf("stalled midway: %+v", st)
	}
}
