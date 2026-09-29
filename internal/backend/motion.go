package backend

import (
	"time"

	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/gyro"
)

// The DualSense's IMU: +-2000 deg/s and +-4 g over 16 bits, and the
// sensor clock DSX copies into the virtual DualSense's reports.
const (
	dsGyroLSB  = 32768.0 / 2000 // per deg/s
	dsAccelLSB = 8192.0         // per g
	dsClockHz  = 3_000_000
)

// Reports is the virtual DualSense's report hook (dualsense.Link.OnReport).
type Reports interface {
	OnReport(f func(dualsense.State, time.Time))
}

var _ Reports = (*dualsense.Link)(nil)

// dualSenseMotion streams a DualSense's motion, one Sample per report.
type dualSenseMotion struct{ r Reports }

func (m dualSenseMotion) OnSample(f func(gyro.Sample)) {
	if f == nil {
		m.r.OnReport(nil)
		return
	}
	m.r.OnReport(func(st dualsense.State, at time.Time) { f(dualSenseSample(st, at)) })
}

// dualSenseSample: the report's raw motion in deg/s and g. The factory
// calibration is not applied (a few percent of gain); the sensitivity
// settings absorb it.
func dualSenseSample(st dualsense.State, at time.Time) gyro.Sample {
	s := gyro.Sample{Stamp: st.Clock, StampHz: dsClockHz, At: at, Touch: st.Touch}
	for i := range 3 {
		s.Gyro[i] = float64(st.Gyro[i]) / dsGyroLSB
		s.Accel[i] = float64(st.Accel[i]) / dsAccelLSB
	}
	return s
}
