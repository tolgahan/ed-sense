package backend

import (
	"bytes"
	"encoding/hex"
	"log"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/dsu"
	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/gyro"
)

func unhex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.Join(strings.Fields(s), ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// snap is DS4Windows' dead band on each gyro value of its virtual
// DualSense: 32 counts (2 deg/s) or less is 0.
func snap(v int) int16 {
	if v >= -32 && v <= 32 {
		return 0
	}
	return int16(v)
}

// viiper is the virtual DualSense's report of DS4Windows' calibrated counts
// pitch, yaw, roll and accel, as DS4Windows fills it.
func viiper(p, y, r, ax, ay, az int) dualsense.State {
	return dualsense.State{Gyro: [3]int16{snap(p), snap(y), snap(r)}, Accel: [3]int16{int16(ax), int16(ay), int16(az)}, Clock: 3000}
}

// wire is the UDP server's packet of the same counts: pitch, yaw and roll
// at 16 per deg/s with yaw and roll turned, the accel in g turned on X and
// Y, and Z sent turned once more.
func wire(p, y, r, ax, ay, az int) dsu.Pad {
	return dsu.Pad{Slot: 0, State: dsu.StateConnected, Micros: 1000,
		Gyro:  [3]float32{float32(p) / 16, float32(-y) / 16, float32(-r) / 16},
		Accel: [3]float32{float32(-ax) / 8192, float32(-ay) / 8192, float32(-az) / 8192}}
}

// noNegZero: no zero of s is -0.
func noNegZero(t *testing.T, name string, s gyro.Sample) {
	t.Helper()
	for i, v := range append(s.Gyro[:], s.Accel[:]...) {
		if v == 0 && math.Signbit(v) {
			t.Errorf("%s: value %d is -0", name, i)
		}
	}
}

// TestUDPSampleVector: the UDP server's motion maps onto the virtual
// DualSense's: the same deg/s and g, bit for bit, above the dead band, and
// the slow turns the dead band drops.
func TestUDPSampleVector(t *testing.T) {
	at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	report := make([]byte, 64)
	report[0], report[1], report[2], report[3], report[4] = 0x01, 0x80, 0x80, 0x80, 0x80
	copy(report[16:], unhex(t, "A0 00 70 FE 30 00 9A 01 D6 1F 32 FB 3F 67 13 16"))
	report[33] = 0x80
	st, ok := dualsense.ParseInputReport(report)
	if !ok || st.Clock != 370370367 {
		t.Fatalf("report %+v", st)
	}
	pad := dualSenseSample(viiperRest(st), at, viiperGyroLSB)
	m, err := dsu.Parse(unhex(t, `
44 53 55 53 E9 03 54 00 E3 85 24 69 DD CC BB AA
02 00 10 00 00 02 02 01 0A 1B 2C 3D 4E 5F 05 01
E8 03 00 00 00 00 00 00 80 7F 80 7F 00 00 00 00
00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00
00 00 00 00 15 CD 5B 07 00 00 00 00 00 00 4D BD
00 B0 7E BF 00 C0 19 3E 00 00 20 41 00 00 C8 41
00 00 40 C0`))
	if err != nil {
		t.Fatal(err)
	}
	udp := udpSample(m.Pad, at)
	if pad.Gyro != [3]float64{10, -25, 3} || pad.Accel != [3]float64{0.050048828125, 0.994873046875, -0.150146484375} {
		t.Errorf("pad %+v", pad)
	}
	if udp.Gyro != pad.Gyro || udp.Accel != pad.Accel || udp.Touch || pad.Touch {
		t.Errorf("UDP %+v, pad %+v", udp, pad)
	}
	if udp.Stamp != 123456789 || udp.StampHz != 1e6 || !udp.At.Equal(at) {
		t.Errorf("UDP clock %d at %v Hz", udp.Stamp, udp.StampHz)
	}

	// built from the counts: the slow turn, the dead band's edges, both signs
	for _, c := range []struct {
		name              string
		p, y, r           int
		padGyro, udpGyro  [3]float64
		ax, ay, az        int
		padEqualsUDPAccel bool
	}{
		{"slow", 24, -16, 0, [3]float64{0, 0, 0}, [3]float64{1.5, -1, 0}, 0, 8192, 0, true},
		{"32", 32, 32, 32, [3]float64{0, 0, 0}, [3]float64{2, 2, 2}, 410, 8150, -1230, true},
		{"-32", -32, -32, -32, [3]float64{0, 0, 0}, [3]float64{-2, -2, -2}, -410, -8150, 1230, true},
		{"33", 33, 33, 33, [3]float64{2.0625, 2.0625, 2.0625}, [3]float64{2.0625, 2.0625, 2.0625}, 0, 0, -8192, true},
		{"-33", -33, -33, -33, [3]float64{-2.0625, -2.0625, -2.0625}, [3]float64{-2.0625, -2.0625, -2.0625}, 1, -1, 8191, true},
	} {
		ps := dualSenseSample(viiperRest(viiper(c.p, c.y, c.r, c.ax, c.ay, c.az)), at, viiperGyroLSB)
		us := udpSample(wire(c.p, c.y, c.r, c.ax, c.ay, c.az), at)
		if ps.Gyro != c.padGyro || us.Gyro != c.udpGyro {
			t.Errorf("%s: pad %v, UDP %v; want %v, %v", c.name, ps.Gyro, us.Gyro, c.padGyro, c.udpGyro)
		}
		if ps.Accel != us.Accel {
			t.Errorf("%s: accel pad %v, UDP %v", c.name, ps.Accel, us.Accel)
		}
		noNegZero(t, c.name, us)
	}

	// zeros of either sign on the wire come out as +0
	neg := float32(math.Copysign(0, -1))
	for _, p := range []dsu.Pad{
		{Micros: 1, Gyro: [3]float32{neg, neg, neg}, Accel: [3]float32{neg, neg, neg}},
		{Micros: 1, Gyro: [3]float32{0, 0, 0}, Accel: [3]float32{0, 0, 0}},
	} {
		noNegZero(t, "zeros", udpSample(p, at))
	}
	if s := udpSample(dsu.Pad{Micros: 1 << 33, Touch: true}, at); s.Stamp != 0 || !s.Touch {
		t.Errorf("the clock wraps as uint32, the touch is kept: %+v", s)
	}
}

// fakeUDP is a UDP server's client a test drives by hand.
type fakeUDP struct {
	mu sync.Mutex
	f  func(dsu.Pad, time.Time)
	st dsu.State
}

func (u *fakeUDP) OnPad(f func(dsu.Pad, time.Time)) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.f = f
}

func (u *fakeUDP) State() dsu.State {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.st
}

func (u *fakeUDP) send(p dsu.Pad, at time.Time) {
	u.mu.Lock()
	f := u.f
	u.mu.Unlock()
	if f != nil {
		f(p, at)
	}
}

// lockedBuf is a log writer for several goroutines.
type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) take() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := strings.TrimSpace(l.b.String())
	l.b.Reset()
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// captureLog sends the log to the returned buffer until the test ends.
func captureLog(t *testing.T) *lockedBuf {
	buf := &lockedBuf{}
	old, flags := log.Writer(), log.Flags()
	log.SetOutput(buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(old); log.SetFlags(flags) })
	return buf
}

// switchRig feeds a ds4wMotion from scripted pad and UDP streams, on a
// scripted clock, and records what reaches the gyro.
type switchRig struct {
	t   *testing.T
	m   *ds4wMotion
	pad *fakeReports
	udp *fakeUDP
	now time.Time
	fed []fed
	log *lockedBuf
}

// fed is a sample that reached the gyro: from the UDP server or the pad.
type fed struct {
	udp bool
	at  time.Time
}

func newSwitchRig(t *testing.T) *switchRig {
	r := &switchRig{t: t, pad: &fakeReports{}, udp: &fakeUDP{st: dsu.State{Addr: "127.0.0.1:26760", Answers: true, Receiving: true, Slot: 0}},
		now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), log: captureLog(t)}
	r.m = newDS4WMotion(r.pad, r.udp, nil)
	r.m.OnSample(func(s gyro.Sample) { r.fed = append(r.fed, fed{s.StampHz == 1e6, s.At}) })
	return r
}

// run plays d of reports every 4 ms: the pad's when pad, the UDP
// server's when udp, both interleaved.
func (r *switchRig) run(d time.Duration, pad, udp bool) {
	for end := r.now.Add(d); r.now.Before(end); r.now = r.now.Add(4 * time.Millisecond) {
		if udp {
			r.udp.send(dsu.Pad{Micros: uint64(r.now.UnixMicro()), Gyro: [3]float32{0.5, 0, 0}}, r.now)
		}
		if pad {
			r.pad.f(dualsense.State{Gyro: [3]int16{160, 0, 0}, Clock: uint32(r.now.UnixMicro() * 3)}, r.now)
		}
	}
}

// count is how many samples of each source reached the gyro since the
// last call.
func (r *switchRig) count() (pad, udp int) {
	for _, f := range r.fed {
		if f.udp {
			udp++
		} else {
			pad++
		}
	}
	r.fed = nil
	return pad, udp
}

func (r *switchRig) lines(want ...string) {
	r.t.Helper()
	got := r.log.take()
	if len(got) != len(want) {
		r.t.Fatalf("log %q, want %q", got, want)
	}
	for i := range got {
		if !strings.HasPrefix(got[i], want[i]) {
			r.t.Fatalf("log %q, want %q", got, want)
		}
	}
}

const (
	startLine    = "Gyro: reading the motion from DS4Windows' UDP server (127.0.0.1:26760, controller 1): no dead band"
	fallbackLine = "Gyro: DS4Windows' UDP server sends no motion, so EDSense reads its virtual DualSense"
)

// TestDS4WMotionSwitch: the UDP server's motion wins while it comes; the
// pad's takes over half a second after it stops; a pad that comes back
// waits half a second for the UDP server; the two never feed the gyro
// within half a second of each other.
func TestDS4WMotionSwitch(t *testing.T) {
	r := newSwitchRig(t)
	var all []fed
	keep := func() (int, int) {
		all = append(all, r.fed...)
		return r.count()
	}

	// the first pad stream, with no UDP server seen: fed at once
	r.run(100*time.Millisecond, true, false)
	if pad, udp := keep(); pad != 25 || udp != 0 {
		t.Fatalf("first stream: %d pad, %d UDP", pad, udp)
	}
	r.lines()

	r.run(200*time.Millisecond, true, true)
	if pad, udp := keep(); pad != 0 || udp != 50 {
		t.Fatalf("UDP starts: %d pad, %d UDP", pad, udp)
	}
	r.lines(startLine)
	if r.m.State().Source != SourceUDP {
		t.Error("the source is not the UDP server")
	}

	// UDP stops: the pad's reports wait half a second
	r.run(496*time.Millisecond, true, false)
	if pad, udp := keep(); pad != 0 || udp != 0 {
		t.Fatalf("UDP just stopped: %d pad, %d UDP", pad, udp)
	}
	r.run(100*time.Millisecond, true, false)
	if pad, _ := keep(); pad != 25 {
		t.Fatalf("after half a second: %d pad", pad)
	}
	r.lines(fallbackLine)
	if r.m.State().Source != SourcePad {
		t.Error("the source is not the pad")
	}

	r.run(100*time.Millisecond, true, true)
	if pad, udp := keep(); pad != 0 || udp != 25 {
		t.Fatalf("UDP back: %d pad, %d UDP", pad, udp)
	}
	r.lines(startLine)

	// Elite closes, the pad and the UDP data stop; Elite starts again,
	// and the UDP data come back 200 ms after the pad: no line
	r.now = r.now.Add(2 * time.Second)
	r.run(200*time.Millisecond, true, false)
	r.run(300*time.Millisecond, true, true)
	if pad, udp := keep(); pad != 0 || udp != 75 {
		t.Fatalf("Elite again: %d pad, %d UDP", pad, udp)
	}
	r.lines()

	// a new pad stream with no UDP data falls back after its grace
	r.now = r.now.Add(2 * time.Second)
	r.run(496*time.Millisecond, true, false)
	r.run(100*time.Millisecond, true, false) // its first report is still within the grace
	if pad, udp := keep(); pad != 24 || udp != 0 {
		t.Fatalf("pad again without UDP: %d pad", pad)
	}
	r.lines(fallbackLine)

	// packets without motion are no motion
	r.run(100*time.Millisecond, false, false)
	r.udp.send(dsu.Pad{}, r.now)
	r.run(100*time.Millisecond, true, false)
	if pad, udp := keep(); pad != 25 || udp != 0 {
		t.Fatalf("empty UDP packets: %d pad, %d UDP", pad, udp)
	}

	// never both within half a second
	var lastUDP time.Time
	for _, f := range all {
		if f.udp {
			lastUDP = f.at
		} else if !lastUDP.IsZero() && f.at.Sub(lastUDP) < udpFresh {
			t.Fatalf("a pad report %v after a UDP packet", f.at.Sub(lastUDP))
		}
	}

	r.m.OnSample(nil)
	if r.pad.f != nil || r.udp.f != nil {
		t.Error("OnSample(nil) left a hook")
	}
}

// TestDS4WMotionLines: a source that keeps switching logs 20 lines, then
// one that says so.
func TestDS4WMotionLines(t *testing.T) {
	r := newSwitchRig(t)
	r.run(8*time.Millisecond, true, false)
	for range 15 { // 30 switches
		r.run(8*time.Millisecond, true, true)
		r.run(600*time.Millisecond, true, false)
	}
	got := r.log.take()
	if len(got) != 21 || got[20] != "Gyro: the motion source keeps switching; no more lines about it" ||
		!strings.HasPrefix(got[19], fallbackLine) || got[0] != startLine {
		t.Errorf("%d lines:\n%s", len(got), strings.Join(got, "\n"))
	}
}

func TestMotionState(t *testing.T) {
	captureLog(t)
	m := newDS4WMotion(&fakeReports{}, &fakeUDP{}, nil)
	u := m.udp.(*fakeUDP)
	var info udpInfo
	m.info = func() udpInfo { return info }
	for _, c := range []struct {
		st   dsu.State
		in   udpInfo
		want MotionState
	}{
		{dsu.State{Addr: "127.0.0.1:26760", Slot: -1}, udpInfo{}, MotionState{UDP: UDPSilent, UDPAddr: "127.0.0.1:26760", Slot: -1}},
		{dsu.State{Addr: "127.0.0.1:26760", Answers: true, Slot: -1}, udpInfo{}, MotionState{UDP: UDPOther, UDPAddr: "127.0.0.1:26760", Slot: -1}},
		{dsu.State{Addr: "127.0.0.1:26760", Answers: true, Slot: 2}, udpInfo{smoothed: true}, MotionState{UDP: UDPReady, UDPAddr: "127.0.0.1:26760", Slot: 2, Smoothed: true}},
		{dsu.State{Addr: "127.0.0.2:26800", Answers: true, Receiving: true, Slot: 0}, udpInfo{}, MotionState{UDP: UDPReceiving, UDPAddr: "127.0.0.2:26800", Slot: 0}},
		{dsu.State{Addr: "127.0.0.1:26760", Answers: true, NoData: true, Slot: 1}, udpInfo{}, MotionState{UDP: UDPNoData, UDPAddr: "127.0.0.1:26760", Slot: 1}},
		{dsu.State{Addr: "127.0.0.1:26760", Slot: -1}, udpInfo{elsewhere: true, addr: "192.168.1.5:26760"}, MotionState{UDP: UDPElsewhere, UDPAddr: "192.168.1.5:26760", Slot: -1}},
		// moved to 127.0.0.1 in DS4Windows' window, not saved yet
		{dsu.State{Addr: "127.0.0.1:26760", Answers: true, Slot: 0}, udpInfo{elsewhere: true, addr: "192.168.1.5:26760"}, MotionState{UDP: UDPReady, UDPAddr: "127.0.0.1:26760", Slot: 0}},
	} {
		u.st, info = c.st, c.in
		if got := m.State(); got != c.want {
			t.Errorf("%+v %+v: %+v, want %+v", c.st, c.in, got, c.want)
		}
	}
	m.OnSample(func(gyro.Sample) {})
	u.send(dsu.Pad{Micros: 1}, time.Now())
	if m.State().Source != SourceUDP {
		t.Error("the source after a UDP packet")
	}
	m.info = nil
	if got := m.State(); got.Smoothed || got.UDP == UDPElsewhere {
		t.Errorf("no settings: %+v", got)
	}
}

// TestNewDS4WindowsNoUDP: without a UDP server part the backend is the
// one the golden tests know: the pad's motion, no motion state.
func TestNewDS4WindowsNoUDP(t *testing.T) {
	r := &fakeReports{}
	b := NewDS4Windows(Parts{Reports: r})
	if m, ok := b.Motion.(dualSenseMotion); !ok || !m.viiper || m.lsb != viiperGyroLSB || b.MotionState != nil {
		t.Errorf("motion %T %+v, state %v", b.Motion, b.Motion, b.MotionState != nil)
	}
	if b.UDPBiasFile != DS4WindowsUDPBiasFile || b.BiasFileFor(SourceUDP) != DS4WindowsUDPBiasFile ||
		b.BiasFileFor(SourcePad) != DS4WindowsBiasFile || b.BiasFileFor(SourceNone) != DS4WindowsBiasFile {
		t.Errorf("bias files %q %q", b.BiasFile, b.UDPBiasFile)
	}
	b = NewDS4Windows(Parts{Reports: r, UDP: &fakeUDP{}})
	if _, ok := b.Motion.(*ds4wMotion); !ok || b.MotionState == nil {
		t.Errorf("with UDP: motion %T", b.Motion)
	}
	d := NewDSX(Parts{Reports: r, Profile: func(string, func(string)) Setup { return nil }})
	if d.MotionState != nil || d.BiasFileFor(SourceUDP) != d.BiasFile {
		t.Errorf("DSX: %q", d.BiasFileFor(SourceUDP))
	}
}
