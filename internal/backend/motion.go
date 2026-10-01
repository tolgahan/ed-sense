package backend

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/tolgahan/ed-sense/internal/dsu"
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
		if m.viiper {
			st = viiperRest(st)
		}
		f(dualSenseSample(st, at, m.lsb))
	})
}

// viiperRest: DS4Windows' virtual DualSense's report without motion data
// becomes one without any, so EDSense sees that no motion comes.
func viiperRest(st dualsense.State) dualsense.State {
	if st.Gyro == [3]int16{} && st.Accel == [3]int16{0, 0, viiperRestZ} {
		st.Accel = [3]int16{}
	}
	return st
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

// udpSample is a DSU packet's motion as the virtual DualSense's report
// gives it (dualSenseSample): DS4Windows sends yaw, roll and the three
// accelerations with the opposite sign, and its motion clock in
// microseconds. No value comes out as -0.
func udpSample(p dsu.Pad, at time.Time) gyro.Sample {
	keep := func(v float32) float64 { return float64(v) + 0 }
	flip := func(v float32) float64 { return 0 - float64(v) }
	return gyro.Sample{
		Gyro:    [3]float64{keep(p.Gyro[0]), flip(p.Gyro[1]), flip(p.Gyro[2])},
		Accel:   [3]float64{flip(p.Accel[0]), flip(p.Accel[1]), flip(p.Accel[2])},
		Stamp:   uint32(p.Micros),
		StampHz: 1_000_000,
		At:      at,
		Touch:   p.Touch,
	}
}

// UDPMotion is DS4Windows' UDP server as a motion source (dsu.Client).
type UDPMotion interface {
	OnPad(f func(dsu.Pad, time.Time))
	State() dsu.State
}

var _ UDPMotion = (*dsu.Client)(nil)

// MotionSource is where the motion stream's samples come from.
type MotionSource int

const (
	SourceNone MotionSource = iota // nothing yet
	SourcePad                      // the virtual DualSense's reports
	SourceUDP                      // DS4Windows' UDP server
)

// How DS4Windows' UDP server serves the motion, in MotionState.UDP.
const (
	UDPReceiving = "receiving" // EDSense reads the motion from it now
	UDPReady     = "ready"     // it answers, with the controller EDSense drives
	UDPNoData    = "nodata"    // it answers with that controller, and sends none of its motion though asked
	UDPOther     = "other"     // it answers, without that controller (it serves controllers 1 to 4 only)
	UDPSilent    = "silent"    // it does not answer: off, or DS4Windows stopped
	UDPElsewhere = "elsewhere" // it listens on an address EDSense does not use, by DS4Windows' settings
)

// MotionState is where the motion comes from now.
type MotionState struct {
	Source   MotionSource
	UDP      string // one of the UDP states; "": no UDP server is read
	UDPAddr  string // where EDSense asks it, or (UDPElsewhere) the address it listens on
	Smoothed bool   // DS4Windows smooths what it sends (Use Smoothing)
	Slot     int    // the DS4Windows slot read, from 0; -1: none
}

// udpInfo is what DS4Windows' settings say of its UDP server.
type udpInfo struct {
	elsewhere bool   // it listens on an address EDSense does not use
	addr      string // where it listens, by the settings
	smoothed  bool   // Use Smoothing
}

// udpState is the UDP state of a client's state, by the settings. A server
// the settings have elsewhere that answers at 127.0.0.1 has been moved
// there in DS4Windows' window, which saves its settings only when it
// exits.
func udpState(st dsu.State, in udpInfo) string {
	switch {
	case in.elsewhere && !st.Answers:
		return UDPElsewhere
	case st.Receiving:
		return UDPReceiving
	case st.Answers && st.Slot >= 0 && st.NoData:
		return UDPNoData
	case st.Answers && st.Slot >= 0:
		return UDPReady
	case st.Answers:
		return UDPOther
	}
	return UDPSilent
}

const (
	udpFresh = 500 * time.Millisecond // UDP motion this recent: the pad's reports are dropped
	padGap   = time.Second            // a longer gap between pad reports starts a new stream
	padGrace = 500 * time.Millisecond // a new pad stream waits this long for the UDP server it read before
	maxLines = 20                     // switch lines per backend, then one last line
)

// ds4wMotion streams DS4Windows' motion: its UDP server's while that sends
// motion for the controller, else the virtual DualSense's reports. Only one
// source feeds the gyro at a time. Every decision goes by the samples' own
// times.
type ds4wMotion struct {
	pad  Reports
	udp  UDPMotion
	info func() udpInfo // nil: none

	mu       sync.Mutex
	f        func(gyro.Sample)
	src      MotionSource
	said     MotionSource // the source the last switch line named
	lines    int          // switch lines logged
	lastUDP  time.Time    // At of the last UDP packet with motion
	lastPad  time.Time    // At of the last pad report
	padSince time.Time    // At of the first report of the pad's current stream
}

func newDS4WMotion(pad Reports, udp UDPMotion, info func() udpInfo) *ds4wMotion {
	return &ds4wMotion{pad: pad, udp: udp, info: info}
}

func (m *ds4wMotion) OnSample(f func(gyro.Sample)) {
	if f == nil {
		m.udp.OnPad(nil)
		m.pad.OnReport(nil)
		m.mu.Lock()
		m.f = nil
		m.mu.Unlock()
		return
	}
	m.mu.Lock()
	m.f = f
	m.mu.Unlock()
	m.udp.OnPad(m.onUDP)
	m.pad.OnReport(m.onPad)
}

// onUDP takes a packet of the UDP server, on its client's goroutine.
func (m *ds4wMotion) onUDP(p dsu.Pad, at time.Time) {
	if !p.HasMotion() {
		return
	}
	st := m.udp.State() // its own lock, never under m.mu
	m.mu.Lock()
	m.lastUDP = at
	if m.src != SourceUDP {
		m.src = SourceUDP
		if m.said != SourceUDP {
			m.line(fmt.Sprintf("Gyro: reading the motion from DS4Windows' UDP server (%s, controller %d): no dead band", st.Addr, st.Slot+1))
			m.said = SourceUDP
		}
	}
	f := m.f
	m.mu.Unlock()
	if f != nil {
		f(udpSample(p, at))
	}
}

// onPad takes a report of the virtual DualSense, on its goroutine.
func (m *ds4wMotion) onPad(st dualsense.State, at time.Time) {
	m.mu.Lock()
	if m.lastPad.IsZero() || at.Sub(m.lastPad) > padGap {
		m.padSince = at
	}
	m.lastPad = at
	// the UDP server sends, or may send again soon after the pad came back
	if !m.lastUDP.IsZero() && at.Sub(m.lastUDP) < udpFresh || m.src == SourceUDP && at.Sub(m.padSince) < padGrace {
		m.mu.Unlock()
		return
	}
	if m.src != SourcePad {
		m.src = SourcePad
		if m.said == SourceUDP {
			m.line("Gyro: DS4Windows' UDP server sends no motion, so EDSense reads its virtual DualSense, which drops turns under 2 deg/s")
			m.said = SourcePad
		}
	}
	f := m.f
	m.mu.Unlock()
	if f != nil {
		f(dualSenseSample(viiperRest(st), at, viiperGyroLSB))
	}
}

// line logs a switch, up to maxLines, then once that there are more.
// Under mu.
func (m *ds4wMotion) line(s string) {
	m.lines++
	switch {
	case m.lines <= maxLines:
		log.Print(s)
	case m.lines == maxLines+1:
		log.Print("Gyro: the motion source keeps switching; no more lines about it")
	}
}

// State is where the motion comes from now.
func (m *ds4wMotion) State() MotionState {
	st := m.udp.State()
	var in udpInfo
	if m.info != nil {
		in = m.info()
	}
	m.mu.Lock()
	src := m.src
	m.mu.Unlock()
	ms := MotionState{Source: src, UDP: udpState(st, in), UDPAddr: st.Addr, Smoothed: in.smoothed, Slot: st.Slot}
	if ms.UDP == UDPElsewhere {
		ms.UDPAddr = in.addr
	}
	return ms
}
