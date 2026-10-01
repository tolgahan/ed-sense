package gyro

import (
	"math"
	"time"
)

// Each report's time step, dt, follows DSX: the controller's sensor clock
// when it runs, clamped to 0.5-20 ms, and a report after a gap of more than
// 50 ms (or a repeated one) moves nothing.
const (
	minStep = 0.0005 // s
	maxStep = 0.020  // s
	maxGap  = 50 * time.Millisecond
	// liveReports: a sensor clock that stops is trusted for this many more
	// repeated reports, then dt comes from the report period
	liveReports = 16
)

type clock struct {
	have   bool
	prev   uint32
	live   int     // reports left before a stuck stamp counts as "no clock"
	hz     float64 // the ticks per second of the reports it follows
	lastAt time.Time
}

// step returns the time the report covers, in seconds. False: it has none
// (the first report, a repeat, or one after a gap), and its movement is
// dropped.
func (c *clock) step(s Sample, period time.Duration) (dt float64, ok bool) {
	gap := s.At.Sub(c.lastAt)
	// two sources' clocks cannot be subtracted, so a report on another
	// clock starts it again and moves nothing
	if s.StampHz != c.hz {
		c.have, c.live, c.hz = false, 0, s.StampHz
	}
	if !c.have {
		c.have, c.prev, c.lastAt = true, s.Stamp, s.At
		return 0, false
	}
	d := s.Stamp - c.prev // uint32, so a wrap needs nothing
	c.prev, c.lastAt = s.Stamp, s.At
	if s.StampHz > 0 && (d != 0 || c.live > 0) {
		if d == 0 {
			c.live--
			return 0, false
		}
		c.live = liveReports
		sec := float64(d) / s.StampHz
		if sec > maxGap.Seconds() {
			return 0, false
		}
		return clamp(sec, minStep, maxStep), true
	}
	if gap > maxGap {
		return 0, false
	}
	// No sensor clock: the average period, since time.Now on Windows may be
	// too coarse to time single reports.
	return clamp(period.Seconds(), minStep, maxStep), true
}

// running: dt comes from the sensor clock.
func (c *clock) running() bool { return c.live > 0 }

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
