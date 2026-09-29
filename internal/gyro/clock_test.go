package gyro

import (
	"math"
	"testing"
	"time"
)

func TestClock(t *testing.T) {
	var c clock
	at := testStart
	step := func(stamp uint32) (float64, bool) {
		at = at.Add(4 * time.Millisecond)
		return c.step(Sample{Stamp: stamp, StampHz: 3e6, At: at}, 4*time.Millisecond)
	}
	if _, ok := step(0xFFFFF000); ok {
		t.Fatal("the first report has no dt")
	}
	for _, s := range []struct {
		name  string
		stamp uint32
		dt    float64
		ok    bool
	}{
		{"the clock wraps", 0x00001EE0, 0.004, true},
		{"12000 ticks", 0x00001EE0 + 12000, 0.004, true},
		{"a repeat", 0x00001EE0 + 12000, 0, false},
		{"after the repeat", 0x00001EE0 + 24000, 0.004, true},
		{"150001 ticks, over 50 ms", 0x00001EE0 + 174001, 0, false},
		{"300 ticks", 0x00001EE0 + 174301, 0.0005, true},
		{"45000 ticks", 0x00001EE0 + 219301, 0.015, true},
	} {
		dt, ok := step(s.stamp)
		if ok != s.ok || math.Abs(dt-s.dt) > 1e-12 {
			t.Errorf("%s: %v %v, want %v %v", s.name, dt, ok, s.dt, s.ok)
		}
	}
	if !c.running() {
		t.Error("the sensor clock runs")
	}

	// A clock that stops is trusted for a few more reports, then dt comes
	// from the report period.
	for range liveReports {
		if _, ok := step(0x00001EE0 + 219301); ok {
			t.Fatal("a repeat gave a dt")
		}
	}
	if dt, ok := step(0x00001EE0 + 219301); !ok || dt != 0.004 || c.running() {
		t.Errorf("stopped clock: %v %v, want the period", dt, ok)
	}
	at = at.Add(60 * time.Millisecond)
	if _, ok := step(0x00001EE0 + 219301); ok {
		t.Error("a 64 ms gap without a clock gave a dt")
	}
}

func TestClockRemainder(t *testing.T) {
	// 100 counts per degree: 1.5 deg/s leaves 0.6 of a count per report,
	// which the repeat and the gap must drop
	set := exactMode()
	set.CountsPerDeg = [2]float64{100, 100}
	r := newRig(set)
	r.feed(2, [3]float64{0, -1.5, 0})
	r.s.Stamp -= 12000 // a repeat
	r.feed(2, [3]float64{0, -1.5, 0})
	r.s.Stamp += 150001 // a gap
	r.feed(2, [3]float64{0, -1.5, 0})
	if dx, _, n := r.m.take(); n != 0 {
		t.Errorf("moved %d: a leftover was kept over a repeat or a gap", dx)
	}
	r.feed(1, [3]float64{0, -1.5, 0})
	if dx, _, _ := r.m.take(); dx != 1 {
		t.Errorf("0.6 + 0.6 counts: %d, want 1", dx)
	}
}

// Without a sensor clock, or with one stuck at 0, dt comes from the report
// period and the output is the same as with the clock.
func TestNoSensorClock(t *testing.T) {
	for _, hz := range []float64{3e6, 0} {
		r := newRig(exactMode())
		r.s.StampHz = hz
		for range 251 {
			r.s.Gyro = [3]float64{0, 10, 0}
			r.a.Feed(r.s)
			r.s.At = r.s.At.Add(4 * time.Millisecond)
		}
		if dx, dy, _ := r.m.take(); dx != -225 || dy != 0 {
			t.Errorf("StampHz %v, stamp stuck at 0: %d %d, want -225 0", hz, dx, dy)
		}
		if st := r.a.Status(); st.StampLive || st.Period != 4*time.Millisecond {
			t.Errorf("StampHz %v: %+v", hz, st)
		}
	}
}
