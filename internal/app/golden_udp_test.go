package app

import (
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/backend"
	"github.com/tolgahan/ed-sense/internal/backend/backendtest"
	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dsu"
	"github.com/tolgahan/ed-sense/internal/gyro"
)

// DS4Windows' UDP server as the gyro's motion source: its client as the
// test scripts it.
var (
	udpReceiving = dsu.State{Addr: "127.0.0.1:26760", Answers: true, Receiving: true, Slot: 0}
	udpReady     = dsu.State{Addr: "127.0.0.1:26760", Answers: true, Slot: 0}
	udpSilent    = dsu.State{Addr: "127.0.0.1:26760", Slot: -1}
)

// udpTurn: a packet of the controller turning at yaw, pitch and roll
// deg/s, lying flat, as DS4Windows' UDP server sends it: yaw, roll and the
// accel turned.
func udpTurn(yaw, pitch, roll float64) *dsu.Pad {
	return &dsu.Pad{Slot: 0, State: dsu.StateConnected,
		Gyro: [3]float32{float32(pitch), float32(-yaw), float32(-roll)}, Accel: [3]float32{0, -1, 0}}
}

// newDS4WUDPPlayer: the DS4Windows player, with a UDP server whose client
// hears nothing yet. The virtual DualSense's reports carry the dead band:
// at rest, and for every turn under 2 deg/s, gyro 0.
func newDS4WUDPPlayer(t *testing.T, edit func(c *config.Config)) *player {
	return newPlayerWith(t, edit, backend.NewDS4Windows, true, func(r *rig) {
		r.udp = &backendtest.UDP{Rec: r.pad.Rec, Seen: udpSilent}
	})
}

// padBias is a drift saved for DS4Windows' virtual DualSense.
var padBias = [3]float64{0.1, -0.05, 0.02}

// Sessions on the DS4Windows backend with its UDP server: slow turns reach
// the aim through it, the virtual DualSense takes over when it stops, and
// each source keeps its own drift.
var udpScripts = []script{
	{"ds4w-udp-slow", ownGyro, func(p *player) {
		freeGyro(p)
		flying(p)
		p.udp.Seen, p.udp.Stream = udpReceiving, udpTurn(1.5, 0, 0)
		p.note("turning left at 1.5 deg/s: the virtual DualSense's reports say 0")
		p.run(time.Second)
		p.udp.Stream = udpTurn(-1.5, 0, 0)
		p.note("turning right at 1.5 deg/s")
		p.run(time.Second)
		p.config(func(c *config.Config) { c.GyroLowSpeed = config.GyroLowExact })
		p.udp.Stream = udpTurn(1.5, 0, 0)
		p.run(time.Second)
		p.udp.Stream = udpTurn(-1.5, 0, 0)
		p.note("turning right at 1.5 deg/s")
		p.run(time.Second)
		p.udp.Stream = udpTurn(1.5, 0, 0)
		p.note("turning left at 1.5 deg/s")
		p.run(time.Second)
		p.stop()
	}},
	{"ds4w-udp-falls-back", ownGyro, func(p *player) {
		freeGyro(p)
		flying(p)
		p.udp.Seen, p.udp.Stream = udpReceiving, udpTurn(1.5, 0, 0)
		p.note("turning left at 1.5 deg/s")
		p.run(time.Second)
		p.udp.Seen, p.udp.Stream = udpReady, nil
		p.note("the UDP server stops sending; still turning at 1.5 deg/s, which the virtual DualSense drops")
		p.run(1500 * time.Millisecond)
		p.pad.Stream = turn(20, 0, 0, false)
		p.note("turning left at 20 deg/s")
		p.run(time.Second)
		p.pad.Stream = nil
		p.udp.Seen, p.udp.Stream = udpReceiving, udpTurn(1.5, 0, 0)
		p.note("the UDP server sends again, turning at 1.5 deg/s")
		p.run(time.Second)
		p.stop()
	}},
	{"ds4w-udp-silent", ownGyro, func(p *player) {
		freeGyro(p)
		flying(p)
		p.note("the UDP server is off")
		p.run(12 * time.Second)
		p.stop()
	}},
	{"ds4w-udp-grace", ownGyro, func(p *player) {
		freeGyro(p)
		flying(p)
		p.udp.Seen, p.udp.Stream = udpReceiving, udpTurn(1.5, 0, 0)
		p.run(time.Second)
		p.s.running, p.pad.Open = false, false
		p.udp.Seen, p.udp.Stream = udpReady, nil
		p.note("Elite closed: the virtual DualSense is gone, and the UDP server's data stop")
		p.run(1500 * time.Millisecond)
		p.s.running, p.pad.Open = true, true
		p.event(`{"event":"LoadGame","Ship":"python"}`)
		p.note("Elite back: the virtual DualSense's reports first")
		p.run(200 * time.Millisecond)
		p.udp.Seen, p.udp.Stream = udpReceiving, udpTurn(1.5, 0, 0)
		p.note("the UDP server's data again")
		p.run(time.Second)
		p.stop()
	}},
	{"ds4w-udp-bias", func(c *config.Config) {
		ownGyro(c)
		// a drift saved for the virtual DualSense before EDSense starts; the
		// data folder is the journal's here
		if err := gyro.SaveBias(filepath.Join(c.JournalDir, backend.DS4WindowsBiasFile), padBias); err != nil {
			panic(err)
		}
	}, func(p *player) {
		freeGyro(p)
		flying(p)
		p.udp.Seen, p.udp.Stream = udpReceiving, udpTurn(0.2, 0, 0)
		p.note("the controller at rest: the UDP server shows its drift")
		p.run(3 * time.Second)
		p.note("aiming, the drift moves only after 4 s of stillness, as with a drift saved")
		p.run(7 * time.Second)
		p.stop()
		if b, ok := gyro.LoadBias(filepath.Join(p.dir, backend.DS4WindowsBiasFile)); !ok || b != padBias {
			p.t.Errorf("the virtual DualSense's drift: %v %v", b, ok)
		}
		if b, ok := gyro.LoadBias(filepath.Join(p.dir, backend.DS4WindowsUDPBiasFile)); !ok || b != [3]float64{0, 0.2, 0} {
			p.t.Errorf("the UDP server's drift: %v %v", b, ok)
		}
	}},
}

// TestFollowMotion: the drift goes with the motion source. A switch saves
// the drift of the source left, only when it changed, and loads the other
// one's, or forgets it until it is learned; the stop and a calibration by
// hand save to the source in use.
func TestFollowMotion(t *testing.T) {
	p := newDS4WPlayer(t, ownGyro)
	freeGyro(p)
	flying(p)
	ms := backend.MotionState{Source: backend.SourcePad}
	p.s.b.MotionState = func() backend.MotionState { return ms }
	padFile, udpFile := filepath.Join(p.dir, backend.DS4WindowsBiasFile), filepath.Join(p.dir, backend.DS4WindowsUDPBiasFile)
	logs := func() []string {
		var out []string
		for _, l := range p.rec.Take() {
			if s, ok := strings.CutPrefix(l, "log "); ok {
				out = append(out, s)
			}
		}
		return out
	}
	logs()
	p.s.gyro.SetBias(padBias)
	p.s.biasSaved = padBias // as if loaded

	p.s.followMotion()
	if p.s.biasSrc != backend.SourcePad || len(logs()) != 0 {
		t.Fatal("the pad's motion, as before: a switch")
	}

	ms.Source = backend.SourceUDP
	if got := p.s.followMotion(); got != ms {
		t.Errorf("followMotion %+v", got)
	}
	if _, err := os.Stat(padFile); err == nil {
		t.Error("the pad's drift saved, unchanged")
	}
	// 0, held as known so that the guard stays on while the gyro aims
	if st := p.s.gyro.Status(); !st.Calibrated || st.Bias != [3]float64{} || p.s.biasSrc != backend.SourceUDP || !p.s.biasGuess {
		t.Errorf("the UDP server's drift, unknown: %+v", st)
	}
	if got := logs(); !slices.Equal(got, []string{"Gyro: no drift known yet for DS4Windows' UDP server; it is learned when the controller lies still"}) {
		t.Errorf("log %q", got)
	}

	// learned for the UDP server, then back on the pad, whose file is there
	udp := [3]float64{0.3, 0.2, -0.1}
	p.s.gyro.SetBias(udp)
	if err := gyro.SaveBias(padFile, padBias); err != nil {
		t.Fatal(err)
	}
	ms.Source = backend.SourcePad
	p.s.followMotion()
	if b, ok := gyro.LoadBias(udpFile); !ok || b != udp {
		t.Errorf("the UDP server's drift saved: %v %v", b, ok)
	}
	if st := p.s.gyro.Status(); !st.Calibrated || st.Bias != padBias {
		t.Errorf("the pad's drift loaded: %+v", st)
	}
	if got := logs(); !slices.Equal(got, []string{"Gyro: drift 0.30 0.20 -0.10 deg/s, saved", "Gyro: drift for DS4Windows' virtual DualSense loaded (0.10 -0.05 0.02 deg/s)"}) {
		t.Errorf("log %q", got)
	}

	// the pad's drift moved; to the UDP server, whose file is there now
	moved := [3]float64{0.15, -0.05, 0.02}
	p.s.gyro.SetBias(moved)
	ms.Source = backend.SourceUDP
	p.s.followMotion()
	if b, _ := gyro.LoadBias(padFile); b != moved {
		t.Errorf("the pad's drift saved: %v", b)
	}
	if st := p.s.gyro.Status(); st.Bias != udp {
		t.Errorf("the UDP server's drift loaded: %+v", st)
	}
	if got := logs(); len(got) != 2 || got[1] != "Gyro: drift for DS4Windows' UDP server loaded (0.30 0.20 -0.10 deg/s)" {
		t.Errorf("log %q", got)
	}

	// no source yet: nothing changes
	ms.Source = backend.SourceNone
	p.s.followMotion()
	if p.s.biasSrc != backend.SourceUDP || len(logs()) != 0 {
		t.Error("SourceNone switched")
	}

	// a calibration by hand and the stop save to the source in use
	if p.s.biasGuess {
		t.Error("a loaded drift is a guess")
	}
	byHand := [3]float64{0.05, 0.05, 0.05}
	p.s.tellManual(gyro.Status{Manual: p.s.seen.Manual + 1, ManualOK: true, Bias: byHand})
	if b, _ := gyro.LoadBias(udpFile); b != byHand {
		t.Errorf("by hand: %v", b)
	}
	stopped := [3]float64{0.25, 0.15, 0}
	p.s.gyro.SetBias(stopped)
	p.s.stopGyro()
	if b, _ := gyro.LoadBias(udpFile); b != stopped {
		t.Errorf("the stop: %v", b)
	}
	if b, _ := gyro.LoadBias(padFile); b != moved {
		t.Errorf("the pad's file was written: %v", b)
	}

	// a backend without a motion state has none
	p.s.b.MotionState = nil
	if got := p.s.followMotion(); got != (backend.MotionState{}) {
		t.Errorf("no motion state: %+v", got)
	}
}

// TestSwitchKeepsGuard: a switch to a source with no drift saved, in
// flight. A slow pan held while aiming, 1 deg/s with a hand's tremor, moves
// the drift by 0.25 deg/s at most, as with a drift saved; a 0 that nobody
// learned is not saved at the stop.
func TestSwitchKeepsGuard(t *testing.T) {
	p := newDS4WPlayer(t, ownGyro)
	freeGyro(p)
	flying(p)
	ms := backend.MotionState{Source: backend.SourcePad}
	p.s.b.MotionState = func() backend.MotionState { return ms }
	ms.Source = backend.SourceUDP
	p.s.followMotion()
	p.s.gyro.SetHold(0)
	at := base
	s := gyro.Sample{Accel: [3]float64{0, 1, 0}, StampHz: 1e6, Stamp: 1000, At: at}
	rng := rand.New(rand.NewSource(9))
	for range 1500 { // 6 s at 250 Hz
		s.Gyro = [3]float64{0, 1 + 0.8*(rng.Float64()-0.5), 0}
		p.s.gyro.Feed(s)
		s.Stamp += 4000
		s.At = s.At.Add(4 * time.Millisecond)
	}
	if b := p.s.gyro.Status().Bias; b[1] > 0.25+1e-9 {
		t.Errorf("a slow pan taken for drift: %v", b)
	}

	q := newDS4WPlayer(t, ownGyro)
	freeGyro(q)
	flying(q)
	q.s.b.MotionState = func() backend.MotionState { return ms }
	q.s.followMotion()
	q.s.stopGyro()
	if _, err := os.Stat(filepath.Join(q.dir, backend.DS4WindowsUDPBiasFile)); err == nil {
		t.Error("the 0 held for the UDP server was saved")
	}
}

// TestUDPNoDataOnce: a UDP server that answers for this controller and
// sends no motion for 10 s of gyro aim in a row is told once; a moment of
// it at a switch, again and again, is not.
func TestUDPNoDataOnce(t *testing.T) {
	p := newDS4WPlayer(t, ownGyro)
	p.rec.Take()
	p.s.hold = 0
	p.s.b.MotionState = func() backend.MotionState { return backend.MotionState{} }
	noData := backend.MotionState{Source: backend.SourcePad, UDP: backend.UDPNoData, Slot: 0}
	for range 30 {
		p.s.ms = noData
		for range 20 {
			p.s.checkMotion(true)
		}
		p.s.ms = backend.MotionState{Source: backend.SourceUDP, UDP: backend.UDPReceiving, Slot: 0}
		p.s.checkMotion(true)
	}
	if got := p.rec.Take(); len(got) != 0 {
		t.Errorf("moments of it: %q", got)
	}
	p.s.ms = noData
	for range 800 {
		p.s.checkMotion(true)
	}
	got := p.rec.Take()
	if len(got) != 2 || got[0] != "log "+p.s.words.UDPNoDataLog || !strings.HasPrefix(got[1], "notify ") {
		t.Errorf("10 s of it: %q", got)
	}
}

// TestUDPOtherOnce: a UDP server without this controller is logged once,
// after 10 s of gyro aim; never without a motion state.
func TestUDPOtherOnce(t *testing.T) {
	p := newDS4WPlayer(t, ownGyro)
	p.rec.Take()
	p.s.hold = 0
	for range 500 {
		p.s.checkMotion(true)
	}
	if got := p.rec.Take(); len(got) != 0 {
		t.Errorf("without a motion state: %q", got)
	}
	p.s.b.MotionState = func() backend.MotionState { return backend.MotionState{} }
	p.s.ms = backend.MotionState{Source: backend.SourcePad, UDP: backend.UDPOther, Slot: -1}
	for range 399 {
		p.s.checkMotion(true)
	}
	if got := p.rec.Take(); len(got) != 0 {
		t.Errorf("before 10 s: %q", got)
	}
	for range 500 {
		p.s.checkMotion(true)
	}
	if got := p.rec.Take(); len(got) != 1 || got[0] != "log "+p.s.words.UDPOtherLog {
		t.Errorf("after 10 s: %q", got)
	}
}

// ds4w-gyro-by-dsx: EDSense's gyro off under DS4Windows. Its profile is
// still followed (aim=true for the setup), and once Elite has been in
// front for 6 s with a profile that leaves the gyro alone, nothing aims,
// which the player hears once. With Elite behind, the profile read is the
// window in front's.
var gyroByDSXScript = script{"ds4w-gyro-by-dsx", dsxGyro, func(p *player) {
	freeGyro(p)
	flying(p)
	p.run(3 * time.Second)
	p.front = false
	p.setup.Free, p.setup.Mouse = false, true
	p.note("alt-tab: DS4Windows gives the window in front a profile with a gyro mouse")
	p.run(2 * time.Second)
	p.front = true
	p.setup.Free, p.setup.Mouse = true, false
	p.note("back in Elite, whose profile leaves the gyro alone")
	p.run(7 * time.Second)
	p.config(ownGyro)
	p.run(3 * time.Second)
	p.config(dsxGyro)
	p.run(3 * time.Second)
	p.stop()
}}
