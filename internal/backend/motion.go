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

// DS4Windows' virtual DualSense (VIIPER) carries 16 counts per deg/s, and
// a report without motion data has gyro 0 and accel (0, 0, -8192).
const (
	viiperGyroLSB = 16.0
	viiperRestZ   = -8192
)

// Reports is the virtual DualSense's report hook (dualsense.Link.OnReport).
type Reports interface {
	OnReport(f func(dualsense.State, time.Time))
}

var _ Reports = (*dualsense.Link)(nil)

// dualSenseMotion streams a DualSense's motion, one Sample per report.
type dualSenseMotion struct {
	r      Reports
	lsb    float64 // gyro counts per deg/s
	viiper bool    // DS4Windows' pad: its no-motion report becomes an empty sample
}

func (m dualSenseMotion) OnSample(f func(gyro.Sample)) {
	if f == nil {
		m.r.OnReport(nil)
		return
	}
	m.r.OnReport(func(st dualsense.State, at time.Time) {
		if m.viiper && st.Gyro == [3]int16{} && st.Accel == [3]int16{0, 0, viiperRestZ} {
			st.Accel = [3]int16{}
		}
		f(dualSenseSample(st, at, m.lsb))
	})
}

// dualSenseSample: the report's raw motion in deg/s and g, the gyro at lsb
// counts per deg/s. The factory calibration is not applied (a few percent
// of gain); the sensitivity settings absorb it.
func dualSenseSample(st dualsense.State, at time.Time, lsb float64) gyro.Sample {
	s := gyro.Sample{Stamp: st.Clock, StampHz: dsClockHz, At: at, Touch: st.Touch}
	for i := range 3 {
		s.Gyro[i] = float64(st.Gyro[i]) / lsb
		s.Accel[i] = float64(st.Accel[i]) / dsAccelLSB
	}
	return s
}
