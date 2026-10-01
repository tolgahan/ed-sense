package gyro

import (
	"math/rand"
	"testing"
	"time"
)

// TestClockSource: a report on another clock (another motion source)
// starts the clock again and moves nothing; the next uses the new clock.
// A steady clock is not touched.
func TestClockSource(t *testing.T) {
	var c clock
	at := testStart
	step := func(stamp uint32, hz float64) (float64, bool) {
		at = at.Add(4 * time.Millisecond)
		return c.step(Sample{Stamp: stamp, StampHz: hz, At: at}, 4*time.Millisecond)
	}
	step(1000, 3e6)
	if dt, ok := step(13000, 3e6); !ok || dt != 0.004 {
		t.Fatalf("3 MHz: %v %v", dt, ok)
	}
	// the UDP server's microseconds, a number far from the pad's ticks
	if _, ok := step(123456789, 1e6); ok {
		t.Error("the first report on another clock gave a dt")
	}
	if dt, ok := step(123456789+4000, 1e6); !ok || dt != 0.004 {
		t.Errorf("1 MHz: %v %v", dt, ok)
	}
	if _, ok := step(25000, 3e6); ok {
		t.Error("back on the pad's clock: a dt")
	}
	if dt, ok := step(37000, 3e6); !ok || dt != 0.004 {
		t.Errorf("3 MHz again: %v %v", dt, ok)
	}

	// through Aim: the switch costs one report of movement, no more
	r := newRig(exactMode())
	r.feed(251, [3]float64{0, 10, 0})
	before, _, _ := r.m.take()
	r.s.StampHz, r.s.Stamp = 1e6, 5_000_000
	r.micros(251, [3]float64{0, 10, 0}) // the first on the new clock moves nothing
	after, _, _ := r.m.take()
	if before != -225 || after != -225 {
		t.Errorf("10 deg/s for 1 s: %d before the switch, %d after it", before, after)
	}
}

// micros feeds n reports on a 1 MHz clock, 4 ms apart.
func (r *rig) micros(n int, gyro [3]float64) {
	for range n {
		r.s.Gyro = gyro
		r.a.Feed(r.s)
		r.s.Stamp += 4000
		r.s.At = r.s.At.Add(4 * time.Millisecond)
	}
}

func TestClockSourceMicros(t *testing.T) {
	r := newRig(exactMode())
	r.s.StampHz = 1e6
	r.micros(251, [3]float64{0, 10, 0})
	if dx, _, _ := r.m.take(); dx != -225 {
		t.Errorf("10 deg/s for 1 s on the 1 MHz clock: %d", dx)
	}
	if !r.a.Status().StampLive {
		t.Error("the 1 MHz clock is not live")
	}
}

// TestSourceSwitchCalibration: a manual calibration under way when the
// motion source changes starts again on the new one, so its mean is the
// new source's alone; the stillness seen before the switch is dropped.
func TestSourceSwitchCalibration(t *testing.T) {
	rng := rand.New(rand.NewSource(8))
	udpDrift := [3]float64{0, 0.4, 0}
	r := newRig(exactMode())
	r.a.SetHold(HoldMenu)
	r.a.Calibrate(2 * time.Second)
	r.feed(250, [3]float64{}) // 1 s of the pad's dead band: zeros
	r.s.StampHz, r.s.Stamp = 1e6, 5_000_000
	for range 600 { // 2.4 s of the UDP server's drift
		var g [3]float64
		for i := range g {
			g[i] = udpDrift[i] + 0.3*(rng.Float64()-0.5)
		}
		r.micros(1, g)
	}
	st := r.a.Status()
	if st.Manual != 1 || !st.ManualOK || !near(st.Bias, udpDrift, 0.02) {
		t.Fatalf("calibrated across the switch: %+v", st)
	}

	// a source that keeps switching: the calibration still ends in time
	r = newRig(exactMode())
	r.a.SetHold(HoldMenu)
	r.a.Calibrate(2 * time.Second)
	for range 20 {
		r.feed(125, [3]float64{})
		r.s.StampHz = 1e6
		r.micros(125, udpDrift)
		r.s.StampHz = 3e6
	}
	if st := r.a.Status(); st.Calibrating || st.Manual != 1 {
		t.Errorf("switching every 0.5 s for 20 s: %+v", st)
	}

	// the auto calibration: 1.5 s still on the pad, then the UDP server's
	// reports; 1 s of them is no 2 s stretch yet
	r = newRig(DefaultSettings())
	r.a.SetHold(HoldMenu)
	r.feed(375, [3]float64{})
	r.s.StampHz, r.s.Stamp = 1e6, 5_000_000
	r.micros(250, udpDrift)
	if st := r.a.Status(); st.Calibrated || st.Learned != 0 {
		t.Fatalf("learned from both sources: %+v", st)
	}
	r.micros(300, udpDrift)
	if st := r.a.Status(); !st.Calibrated || !near(st.Bias, udpDrift, 1e-9) {
		t.Errorf("the UDP server's 2.2 s: %+v", st)
	}
}

// TestForget: the bias is dropped and learned again at the next
// stillness; a manual calibration under way finishes.
func TestForget(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	r := newRig(DefaultSettings())
	r.a.SetHold(HoldMenu)
	r.a.SetBias(restBias)
	r.a.Forget()
	if st := r.a.Status(); st.Calibrated || st.Bias != [3]float64{} || st.Learned != 0 {
		t.Fatalf("forgotten: %+v", st)
	}
	r.feedNoisy(rng, 626, restBias, 0.3, 0.004) // 2.5 s still
	if st := r.a.Status(); !st.Calibrated || st.Learned != 1 || !near(st.Bias, restBias, 0.05) {
		t.Fatalf("learned again: %+v", st)
	}

	// a manual calibration under way goes on
	r.a.Calibrate(2 * time.Second)
	r.feedNoisy(rng, 100, restBias, 0.3, 0.004)
	r.a.Forget()
	if st := r.a.Status(); !st.Calibrating || st.Calibrated {
		t.Fatalf("forgotten while calibrating: %+v", st)
	}
	r.feedNoisy(rng, 450, restBias, 0.3, 0.004)
	if st := r.a.Status(); st.Manual != 1 || !st.ManualOK || !st.Calibrated || !near(st.Bias, restBias, 0.02) {
		t.Errorf("the manual calibration: %+v", st)
	}

	// learned again after a second Forget: Learned counts it
	r.a.Forget()
	r.feedNoisy(rng, 626, restBias, 0.3, 0.004)
	if st := r.a.Status(); !st.Calibrated || st.Learned != 2 {
		t.Errorf("learned once more: %+v", st)
	}
}
