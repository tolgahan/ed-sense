package backend

import (
	"math"
	"net"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/dualsense"
)

// TestDSX: the DSX backend has every part, DSX's caps, and DSX's devices
// behind them. The golden tests use the same assembly with recording parts.
func TestDSX(t *testing.T) {
	sink, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	b, err := DSX(sink.LocalAddr().(*net.UDPAddr).Port, false)
	if err != nil {
		t.Fatal(err)
	}
	if b.Close == nil || b.NewAudio == nil || b.NewSetup == nil || b.Motion == nil {
		t.Fatal("DSX lacks Close, NewAudio, NewSetup or Motion")
	}
	defer b.Close()
	defer b.Pad.Close()
	if b.Name != "DSX" || b.Caps != DSXCaps() {
		t.Errorf("DSX is %q with %+v", b.Name, b.Caps)
	}
	if _, ok := b.Output.(*dsx.Client); !ok {
		t.Errorf("Output is %T", b.Output)
	}
	if _, ok := b.Pad.(*dualsense.Link); !ok {
		t.Errorf("Pad is %T", b.Pad)
	}
	if a, ok := b.NewAudio(func([]int16) {}).(*dualsense.HapticsOut); !ok {
		t.Errorf("Audio is %T", a)
	}
	if s, ok := b.NewSetup(t.TempDir(), func(string) {}).(*dsx.ProfileInstaller); !ok {
		t.Errorf("Setup is %T", s)
	}
}

// TestDualSenseSample: raw IMU counts in deg/s and g, the sensor clock and
// the touch copied.
func TestDualSenseSample(t *testing.T) {
	at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := dualSenseSample(dualsense.State{Gyro: [3]int16{16384, -1638, 0}, Accel: [3]int16{0, 8192, 0}, Clock: 12345, Touch: true}, at)
	if math.Abs(s.Gyro[0]-1000) > 1e-9 || math.Abs(s.Gyro[1]+99.976) > 0.001 || s.Gyro[2] != 0 {
		t.Errorf("gyro %v", s.Gyro)
	}
	if s.Accel != [3]float64{0, 1, 0} || s.Stamp != 12345 || s.StampHz != 3e6 || !s.At.Equal(at) || !s.Touch {
		t.Errorf("sample %+v", s)
	}
}
