package backend

import (
	"math"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/ds4w"
	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/gyro"
)

// TestDS4Windows: the DS4Windows backend has every part, its caps (no gyro
// switch, overrides kept, rumble mutes native haptics), its own calibration
// file and words, and a link whose stops switch rumble emulation off. Its
// setup is not made here: that would ask a DS4Windows running on this PC.
func TestDS4Windows(t *testing.T) {
	sink, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	b, err := DS4Windows(DS4WindowsOptions{Addr: sink.LocalAddr().(*net.UDPAddr), Haptics: func() string { return config.DS4WHapticsAuto }})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	defer b.Pad.Close()
	if b.Close == nil || b.NewAudio == nil || b.NewSetup == nil || b.Motion == nil {
		t.Fatal("DS4Windows lacks Close, NewAudio, NewSetup or Motion")
	}
	want := DSXCaps()
	want.MotionOff, want.KeepsOverrides, want.RumbleMutesHaptics = false, true, true
	if b.Name != "DS4Windows" || b.Kind != KindDS4Windows || b.Caps != want || b.BiasFile != DS4WindowsBiasFile || b.Words.Name != "DS4Windows" {
		t.Errorf("DS4Windows is %q (%s) with %+v, %s", b.Name, b.Kind, b.Caps, b.BiasFile)
	}
	if _, ok := b.Output.(*dsx.Client); !ok {
		t.Errorf("Output is %T", b.Output)
	}
	if _, ok := b.Pad.(*dualsense.Link); !ok {
		t.Errorf("Pad is %T", b.Pad)
	}
	if m, ok := b.Motion.(dualSenseMotion); !ok || m.lsb != 16 || !m.viiper {
		t.Errorf("motion %+v", b.Motion)
	}
	if o := ds4wLinkOptions(b.Words); !o.StopClears || o.Prefer != dualsense.HostUSBIPWin2 || o.Missing != b.Words.PadMissing {
		t.Errorf("link options %+v", o)
	}
}

// TestFollowListener: while DS4Windows does not answer, the client moves
// to where the DS4Windows that starts later listens, and the listener is
// looked up only when the DS4Windows that runs changes.
func TestFollowListener(t *testing.T) {
	listen := func() (*net.UDPConn, <-chan struct{}) {
		c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		got := make(chan struct{}, 16)
		go func() {
			buf := make([]byte, 65536)
			for {
				if _, err := c.Read(buf); err != nil {
					return
				}
				select {
				case got <- struct{}{}:
				default:
				}
			}
		}()
		return c, got
	}
	first, _ := listen()
	portable, heard := listen()
	client, err := dsx.NewClientTo(first.LocalAddr().(*net.UDPAddr), false, dsx.DS4Windows)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var mu sync.Mutex
	exe, lookups := "", 0
	running := func() string {
		mu.Lock()
		defer mu.Unlock()
		return exe
	}
	listener := func() (*net.UDPAddr, string) {
		mu.Lock()
		lookups++
		mu.Unlock()
		return portable.LocalAddr().(*net.UDPAddr), `C:\DS4Windows`
	}
	stop := make(chan struct{})
	defer close(stop)
	go followListener(client, running, listener, 5*time.Millisecond, stop)
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if lookups != 0 {
		t.Errorf("looked up %d times while no DS4Windows started", lookups)
	}
	exe = `C:\DS4Windows\DS4Windows.exe`
	mu.Unlock()
	for end := time.Now().Add(2 * time.Second); ; {
		client.RequestStatus()
		select {
		case <-heard:
		case <-time.After(20 * time.Millisecond):
			if time.Now().Before(end) {
				continue
			}
			t.Fatal("the client never reached the portable DS4Windows' listener")
		}
		break
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if lookups != 1 {
		t.Errorf("looked up %d times for one DS4Windows that started", lookups)
	}
}

// TestDS4WindowsSetup: the profile watch, on DS4Windows files made for
// the tests and without asking any DS4Windows, tells the gyro's use.
func TestDS4WindowsSetup(t *testing.T) {
	var s Setup = ds4wSetup{ds4w.NewSetup(ds4w.Env{DataDir: func() string { return "../ds4w/testdata/DS4Windows" }}, nil)}
	if _, ok := s.(GameWatcher); !ok {
		t.Fatalf("Setup %T does not follow the game", s)
	}
	for end := time.Now().Add(2 * time.Second); s.Gyro() == GyroUnknown && time.Now().Before(end); {
		time.Sleep(10 * time.Millisecond)
	}
	if s.Gyro() != GyroUnused {
		t.Fatalf("Controller1's profile has its gyro on Passthru: %v", s.Gyro())
	}
}

// fakeReports hands the reports to the motion stream by hand.
type fakeReports struct {
	f func(dualsense.State, time.Time)
}

func (r *fakeReports) OnReport(f func(dualsense.State, time.Time)) { r.f = f }

// TestViiperMotion: DS4Windows' virtual DualSense has 16 counts per deg/s,
// and its report without motion data becomes an empty sample, so EDSense
// sees that no motion comes.
func TestViiperMotion(t *testing.T) {
	r := &fakeReports{}
	var got []gyro.Sample
	dualSenseMotion{r: r, lsb: viiperGyroLSB, viiper: true}.OnSample(func(s gyro.Sample) { got = append(got, s) })
	at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	r.f(dualsense.State{Gyro: [3]int16{160, -32, 0}, Accel: [3]int16{0, 8192, 0}}, at)
	r.f(dualsense.State{Accel: [3]int16{0, 0, -8192}}, at)
	r.f(dualsense.State{Gyro: [3]int16{0, 0, 40}, Accel: [3]int16{0, 0, -8192}}, at)
	if len(got) != 3 {
		t.Fatalf("%d samples", len(got))
	}
	if math.Abs(got[0].Gyro[0]-10) > 1e-9 || math.Abs(got[0].Gyro[1]+2) > 1e-9 || got[0].Accel != [3]float64{0, 1, 0} {
		t.Errorf("motion %+v", got[0])
	}
	if got[1].Gyro != [3]float64{} || got[1].Accel != [3]float64{} {
		t.Errorf("no motion data is not empty: %+v", got[1])
	}
	if got[2].Accel != [3]float64{0, 0, -1} || got[2].Gyro[2] != 2.5 {
		t.Errorf("motion with the rest accel: %+v", got[2])
	}
	dsxM := dualSenseMotion{r: r, lsb: dsGyroLSB}
	dsxM.OnSample(func(s gyro.Sample) { got = append(got, s) })
	r.f(dualsense.State{Accel: [3]int16{0, 0, -8192}}, at)
	if s := got[len(got)-1]; s.Accel != [3]float64{0, 0, -1} {
		t.Errorf("DSX's reports are left as they are: %+v", s)
	}
}

func TestHapticsRoute(t *testing.T) {
	for setting, want := range map[string]dualsense.AudioRoute{
		config.DS4WHapticsAuto: dualsense.RouteAuto, config.DS4WHapticsController: dualsense.RouteController,
		config.DS4WHapticsVirtual: dualsense.RouteVirtual, "": dualsense.RouteAuto,
	} {
		if got := HapticsRoute(setting); got != want {
			t.Errorf("%q: %v", setting, got)
		}
	}
}

// TestDetect: "auto" takes the app that runs; with both, the one that
// answers; with neither, DS4Windows only when it answers, else DSX. A
// DS4Windows before 5 has no DSX listener and counts as not running; a
// renamed one is found by its window.
func TestDetect(t *testing.T) {
	a := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 6969}
	b := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 6970}
	ds4 := map[string]dsx.Dialect{a.String(): dsx.DS4Windows}
	for _, c := range []struct {
		name          string
		dsxOn, ds4On  bool
		window        bool   // DS4Windows' window is there
		version       string // the running DS4Windows'
		dsxAddr       *net.UDPAddr
		answers       map[string]dsx.Dialect // by address; missing: silence
		want          Kind
		probesAllowed bool
	}{
		{"only DSX", true, false, false, "", a, nil, KindDSX, false},
		{"only DS4Windows", false, true, false, "5.0.12.0", a, nil, KindDS4Windows, false},
		{"only DS4Windows, version unknown", false, true, false, "", a, nil, KindDS4Windows, false},
		{"only DS4Windows, renamed", false, false, true, "5.0.12.0", a, nil, KindDS4Windows, false},
		{"only DS4Windows 3.x", false, true, true, "3.3.3.0", a, nil, KindDSX, true},
		{"DS4Windows 3.x and DSX", true, true, true, "3.3.3.0", a, nil, KindDSX, false},
		{"neither", false, false, false, "", a, nil, KindDSX, true},
		{"neither seen, DS4Windows answers", false, false, false, "", a, ds4, KindDS4Windows, true},
		{"neither seen, DSX answers there", false, false, false, "", a, map[string]dsx.Dialect{a.String(): dsx.DSX}, KindDSX, true},
		{"both, DS4Windows answers", true, true, false, "", a, ds4, KindDS4Windows, true},
		{"both, DSX answers", true, true, false, "", a, map[string]dsx.Dialect{a.String(): dsx.DSX}, KindDSX, true},
		{"both, DSX on its own port", true, true, false, "", b, map[string]dsx.Dialect{b.String(): dsx.DSX}, KindDSX, true},
		{"both, silence", true, true, false, "", a, nil, KindDSX, true},
	} {
		probed, versions := 0, 0
		env := DetectEnv{
			Running: func(exe string) bool {
				switch exe {
				case DSXExe:
					return c.dsxOn
				case ds4w.Exe:
					return c.ds4On
				}
				t.Fatalf("asked about %s", exe)
				return false
			},
			DS4Window: func() bool { return c.window },
			DS4Version: func() string {
				versions++
				return c.version
			},
			Probe: func(addr *net.UDPAddr) (dsx.Dialect, bool) {
				probed++
				d, ok := c.answers[addr.String()]
				return d, ok
			},
			DSXAddr: c.dsxAddr, DS4Addr: a,
		}
		got, why := Detect(env)
		if got != c.want || why == "" {
			t.Errorf("%s: %s (%s)", c.name, got, why)
		}
		if !c.probesAllowed && probed > 0 {
			t.Errorf("%s: probed with one app running", c.name)
		}
		if !c.ds4On && !c.window && versions > 0 {
			t.Errorf("%s: asked the version of a DS4Windows that does not run", c.name)
		}
	}
	if k, _ := Detect(DetectEnv{Running: func(exe string) bool { return exe == ds4w.Exe }}); k != KindDS4Windows {
		t.Errorf("without a window or version check: %s", k)
	}
}

// TestAppWatch: the port is probed, and the version read, only when the
// apps that run change; DS4Windows' window appearing is a change.
func TestAppWatch(t *testing.T) {
	dsxOn, ds4On, window, probes, versions := false, false, false, 0, 0
	w := &AppWatch{Env: DetectEnv{
		Running:   func(exe string) bool { return exe == DSXExe && dsxOn || exe == ds4w.Exe && ds4On },
		DS4Window: func() bool { return window },
		DS4Version: func() string {
			versions++
			return "5.0.12.0"
		},
		Probe: func(*net.UDPAddr) (dsx.Dialect, bool) {
			probes++
			return dsx.DSX, false
		},
		DSXAddr: &net.UDPAddr{Port: 1}, DS4Addr: &net.UDPAddr{Port: 1},
	}}
	if k, _, changed := w.Check(); !changed || k != KindDSX || probes != 1 {
		t.Fatalf("first check: %s %v, %d probes", k, changed, probes)
	}
	if _, _, changed := w.Check(); changed || probes != 1 {
		t.Fatalf("nothing changed: %v, %d probes", changed, probes)
	}
	window = true
	if k, _, changed := w.Check(); !changed || k != KindDS4Windows || versions != 1 {
		t.Fatalf("a renamed DS4Windows started: %s %v, %d version reads", k, changed, versions)
	}
	ds4On, window = true, false
	if k, _, changed := w.Check(); !changed || k != KindDS4Windows {
		t.Fatalf("DS4Windows started: %s %v", k, changed)
	}
	dsxOn = true
	w.Env.Probe = func(*net.UDPAddr) (dsx.Dialect, bool) {
		probes++
		return dsx.DS4Windows, true
	}
	if k, _, changed := w.Check(); !changed || k != KindDS4Windows || probes != 2 {
		t.Fatalf("DSX started too: %s %v, %d probes", k, changed, probes)
	}
	w.Check()
	if probes != 2 || versions != 3 {
		t.Fatalf("probed or read the version again without a change: %d probes, %d version reads", probes, versions)
	}
}

// TestWords: every text is there, in ASCII, and DS4Windows' warnings take
// their detail.
func TestWords(t *testing.T) {
	for _, w := range []Words{DSXWords(), DS4WindowsWords()} {
		v := reflect.ValueOf(w)
		for i := range v.NumField() {
			f := v.Type().Field(i)
			s, ok := v.Field(i).Interface().(string)
			if !ok {
				continue
			}
			if s == "" && !(w.Name == "DSX" && (f.Name == "OutputsOff" || f.Name == "ProfileUnknown" || f.Name == "DemoNoFallback")) {
				t.Errorf("%s: %s is empty", w.Name, f.Name)
			}
			for _, r := range s {
				if r > 127 {
					t.Errorf("%s: %s is not ASCII: %q", w.Name, f.Name, s)
				}
			}
		}
	}
	w := DS4WindowsWords()
	for _, which := range []ds4w.Warning{ds4w.WarnNotDualSense, ds4w.WarnPhysicalVisible, ds4w.WarnDSXOnPort, ds4w.WarnOldVersion, ds4w.WarnTriggerLab, ds4w.WarnTouchpadMouse} {
		text := w.Warning(which, "DETAIL")
		if text == "" || strings.Contains(text, "%") {
			t.Errorf("warning %d: %q", which, text)
		}
	}
	if !strings.Contains(w.Warning(ds4w.WarnOldVersion, "4.9"), "4.9") {
		t.Error("the version is not told")
	}
}
