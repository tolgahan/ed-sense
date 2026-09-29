package backend

import (
	"net"
	"testing"

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
	if b.Close == nil || b.NewAudio == nil || b.NewSetup == nil {
		t.Fatal("DSX lacks Close, NewAudio or NewSetup")
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
