package backend

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dsu"
	"github.com/tolgahan/ed-sense/internal/dsu/dsutest"
	"github.com/tolgahan/ed-sense/internal/dualsense"
)

// udpFolder is a DS4Windows data folder whose Profiles.xml has the UDP
// server on these lines, with the test folder's other files; it never
// names DS4Windows' own port, so no test asks 127.0.0.1:26760.
func udpFolder(t *testing.T, lines string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "DS4Windows")
	if err := os.MkdirAll(filepath.Join(dir, "Profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join("..", "ds4w", "testdata", "DS4Windows")
	for _, name := range []string{"Auto Profiles.xml", "Profiles/Default.xml", "Profiles/Elite Passthru.xml"} {
		b, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	setUDP(t, dir, lines, time.Now().Add(-time.Hour))
	return dir
}

// setUDP writes Profiles.xml with these UDP server lines, saved at mod.
func setUDP(t *testing.T, dir, lines string, mod time.Time) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "ds4w", "testdata", "DS4Windows", "Profiles.xml"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(b), "</Profile>", lines+"</Profile>", 1)
	path := filepath.Join(dir, "Profiles.xml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func udpLines(port int, address string) string {
	s := fmt.Sprintf("  <UseUDPServer>True</UseUDPServer>\n  <UDPServerPort>%d</UDPServerPort>\n", port)
	if address != "" {
		s += "  <UDPServerListenAddress>" + address + "</UDPServerListenAddress>\n"
	}
	return s
}

// stubSystem keeps the backends off the real system for the test: the HID
// list has a pad that cannot be opened, DS4Windows is never asked, and its
// data folder is dir.
func stubSystem(t *testing.T, dir string) {
	oldList, oldDir, oldQuery, oldEvery := hidList, ds4wDataDir, ds4wQuery, udpEvery
	hidList = func() []dualsense.HIDDevice {
		return []dualsense.HIDDevice{{Path: `\\?\HID#EDSENSE_TEST_NO_SUCH_PAD#0#{4d1e55b2-f16f-11cf-88cb-001111000030}`,
			ProductID: 0x0CE6, Kind: dualsense.Virtual, InLen: 64, OutLen: 48}}
	}
	ds4wDataDir = func() string { return dir }
	ds4wQuery = func(int, string) (string, error) { return "", errors.New("no DS4Windows in a test") }
	t.Cleanup(func() { hidList, ds4wDataDir, ds4wQuery, udpEvery = oldList, oldDir, oldQuery, oldEvery })
}

func waitState(t *testing.T, what string, b *Backend, ok func(MotionState) bool) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); !ok(b.MotionState()); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("%s: %+v", what, b.MotionState())
		}
	}
}

// TestDS4WindowsUDP: the real backend asks the UDP server where
// DS4Windows' Profiles.xml has it, follows the file when it changes, and
// leaves an address on another PC alone; Close ends it all.
func TestDS4WindowsUDP(t *testing.T) {
	first, second := dsutest.NewServer(t), dsutest.NewServer(t)
	connected := [dsu.Slots]dsu.Port{{Slot: 0, State: dsu.StateConnected, Model: 2, Connection: 1, MAC: [6]byte{1, 2, 3, 4, 5, 6}}, {Slot: 1}, {Slot: 2}, {Slot: 3}}
	first.SetPorts(connected)
	second.SetPorts(connected)
	dir := udpFolder(t, udpLines(first.Addr().Port, ""))
	stubSystem(t, dir)
	udpEvery = 20 * time.Millisecond
	logged := captureLog(t)
	sink := udpSink(t)
	base := runtime.NumGoroutine()

	b, err := DS4Windows(DS4WindowsOptions{Addr: sink, UDP: true, Haptics: func() string { return config.DS4WHapticsAuto }})
	if err != nil {
		t.Fatal(err)
	}
	if b.MotionState == nil {
		t.Fatal("no motion state")
	}
	waitState(t, "the first server answers", b, func(s MotionState) bool {
		return s.UDP == UDPReady && s.UDPAddr == first.Addr().String() && s.Slot == 0
	})
	if ports, data := first.Requests(); ports == 0 || data != 0 {
		t.Errorf("%d port requests, %d data requests with the virtual pad closed", ports, data)
	}
	want := fmt.Sprintf("DS4Windows: gyro motion from its UDP server on %s when it sends (from %s, where it is on)", first.Addr(), filepath.Join(dir, "Profiles.xml"))
	if got := logged.take(); len(got) == 0 || got[0] != want {
		t.Errorf("log %q, want %q", got, want)
	}

	setUDP(t, dir, udpLines(second.Addr().Port, "127.0.0.1"), time.Now())
	waitState(t, "the second server answers", b, func(s MotionState) bool {
		return s.UDP == UDPReady && s.UDPAddr == second.Addr().String()
	})
	if got := strings.Join(logged.take(), "\n"); !strings.Contains(got, "DS4Windows: its UDP server is on "+second.Addr().String()+" now") {
		t.Errorf("log %q", got)
	}

	// another PC's address at a port where nothing listens on 127.0.0.1
	gone := closedPort(t)
	setUDP(t, dir, udpLines(gone, "192.168.1.5"), time.Now().Add(time.Second))
	waitState(t, "an address on another PC", b, func(s MotionState) bool {
		return s.UDP == UDPElsewhere && s.UDPAddr == fmt.Sprintf("192.168.1.5:%d", gone)
	})
	if got := strings.Join(logged.take(), "\n"); !strings.Contains(got, fmt.Sprintf(`its UDP server listens on "192.168.1.5:%d"`, gone)) ||
		!strings.Contains(got, fmt.Sprintf("EDSense asks 127.0.0.1:%d in case", gone)) {
		t.Errorf("log %q", got)
	}

	b.Pad.Close()
	b.Close()
	b.Close()
	waitGoroutines(t, base)
}

// closedPort is a port on 127.0.0.1 where no server listens now.
func closedPort(t *testing.T) int {
	srv := dsutest.NewServer(t)
	port := srv.Addr().Port
	srv.Close()
	return port
}

// TestDS4WindowsUDPElsewhere: settings that name another PC's address from
// the start: EDSense says so, and asks 127.0.0.1 at their port all the
// same, where DS4Windows serves once the address is set to 127.0.0.1 in
// its window (it writes its settings only when it exits).
func TestDS4WindowsUDPElsewhere(t *testing.T) {
	gone := closedPort(t)
	stubSystem(t, udpFolder(t, udpLines(gone, "10.1.2.3")))
	logged := captureLog(t)
	b, err := DS4Windows(DS4WindowsOptions{Addr: udpSink(t), UDP: true})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	defer b.Pad.Close()
	time.Sleep(50 * time.Millisecond)
	if s := b.MotionState(); s.UDP != UDPElsewhere || s.UDPAddr != fmt.Sprintf("10.1.2.3:%d", gone) {
		t.Errorf("state %+v", s)
	}
	if got := logged.take(); len(got) == 0 || !strings.Contains(got[0], "which EDSense does not use (it reads it on this PC only)") {
		t.Errorf("log %q", got)
	}

	// the server moved to 127.0.0.1 in DS4Windows' window: found
	srv := dsutest.NewServer(t)
	srv.SetPorts([dsu.Slots]dsu.Port{{Slot: 0, State: dsu.StateConnected, MAC: [6]byte{1, 2, 3, 4, 5, 6}}, {Slot: 1}, {Slot: 2}, {Slot: 3}})
	stubSystem(t, udpFolder(t, udpLines(srv.Addr().Port, "10.1.2.3")))
	b2, err := DS4Windows(DS4WindowsOptions{Addr: udpSink(t), UDP: true})
	if err != nil {
		t.Fatal(err)
	}
	defer b2.Close()
	defer b2.Pad.Close()
	waitState(t, "the server on 127.0.0.1", b2, func(s MotionState) bool {
		return s.UDP == UDPReady && s.UDPAddr == srv.Addr().String()
	})
}
