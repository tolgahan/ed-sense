package backend

import (
	"errors"
	"io"
	"log"
	"net"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/ds4w"
	"github.com/tolgahan/ed-sense/internal/dsx"
	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/gyro"
)

// TestDetectSure: Detect's pick, and whether it is sure: one app runs, or
// one answers. The fallbacks are not.
func TestDetectSure(t *testing.T) {
	a := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 6969}
	b := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 6970}
	for _, c := range []struct {
		name         string
		dsxOn, ds4On bool
		version      string
		dsxAddr      *net.UDPAddr
		answers      map[string]dsx.Dialect
		want         Kind
		sure         bool
	}{
		{"only DSX", true, false, "", a, nil, KindDSX, true},
		{"only DS4Windows", false, true, "5.0.12.0", a, nil, KindDS4Windows, true},
		{"only DS4Windows 3.x", false, true, "3.3.3.0", a, nil, KindDSX, false},
		{"DS4Windows 3.x and DSX", true, true, "3.3.3.0", a, nil, KindDSX, true},
		{"neither", false, false, "", a, nil, KindDSX, false},
		{"neither seen, DS4Windows answers", false, false, "", a, map[string]dsx.Dialect{a.String(): dsx.DS4Windows}, KindDS4Windows, true},
		{"neither seen, DSX answers there", false, false, "", a, map[string]dsx.Dialect{a.String(): dsx.DSX}, KindDSX, false},
		{"both, DS4Windows answers", true, true, "", a, map[string]dsx.Dialect{a.String(): dsx.DS4Windows}, KindDS4Windows, true},
		{"both, DSX answers", true, true, "", a, map[string]dsx.Dialect{a.String(): dsx.DSX}, KindDSX, true},
		{"both, DSX on its own port", true, true, "", b, map[string]dsx.Dialect{b.String(): dsx.DSX}, KindDSX, true},
		{"both, silence", true, true, "", a, nil, KindDSX, false},
	} {
		env := DetectEnv{
			Running:    func(exe string) bool { return exe == DSXExe && c.dsxOn || exe == ds4w.Exe && c.ds4On },
			DS4Version: func() string { return c.version },
			Probe: func(addr *net.UDPAddr) (dsx.Dialect, bool) {
				d, ok := c.answers[addr.String()]
				return d, ok
			},
			DSXAddr: c.dsxAddr, DS4Addr: a,
		}
		kind, why, sure := DetectSure(env)
		if kind != c.want || sure != c.sure || why == "" {
			t.Errorf("%s: %s (%s), sure %v; want %s, sure %v", c.name, kind, why, sure, c.want, c.sure)
		}
		if k, w := Detect(env); k != kind || w != why {
			t.Errorf("%s: Detect says %s (%s), DetectSure %s (%s)", c.name, k, w, kind, why)
		}
	}
}

// TestAppWatchDetail: CheckDetail tells whether the pick is sure; Before
// runs only on a change, and the addresses it sets are the ones probed;
// Forget makes the next check a change.
func TestAppWatchDetail(t *testing.T) {
	old := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 6969}
	moved := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 6970}
	dsxOn, ds4On, befores := false, false, 0
	var probed []string
	w := &AppWatch{
		Env: DetectEnv{
			Running: func(exe string) bool { return exe == DSXExe && dsxOn || exe == ds4w.Exe && ds4On },
			Probe: func(addr *net.UDPAddr) (dsx.Dialect, bool) {
				probed = append(probed, addr.String())
				return dsx.DS4Windows, addr.String() == moved.String()
			},
			DSXAddr: old, DS4Addr: old,
		},
		Before: func(env *DetectEnv) {
			befores++
			env.DS4Addr = moved
		},
	}
	if k, why, sure, changed := w.CheckDetail(); !changed || k != KindDS4Windows || !sure || befores != 1 {
		t.Fatalf("first check: %s (%s), sure %v, changed %v, %d befores", k, why, sure, changed, befores)
	}
	if !reflect.DeepEqual(probed, []string{moved.String()}) {
		t.Fatalf("probed %v, want the address Before set", probed)
	}
	if _, _, _, changed := w.CheckDetail(); changed || befores != 1 || len(probed) != 1 {
		t.Fatalf("nothing changed: changed %v, %d befores, %d probes", changed, befores, len(probed))
	}
	w.Forget()
	if k, _, sure, changed := w.CheckDetail(); !changed || k != KindDS4Windows || !sure || befores != 2 || len(probed) != 2 {
		t.Fatalf("after Forget: %s, sure %v, changed %v, %d befores, %d probes", k, sure, changed, befores, len(probed))
	}
	dsxOn, ds4On = true, true
	w.Before = nil
	w.Env.Probe = func(*net.UDPAddr) (dsx.Dialect, bool) { return 0, false }
	if k, _, sure, changed := w.CheckDetail(); !changed || k != KindDSX || sure {
		t.Fatalf("both run, neither answers: %s, sure %v, changed %v", k, sure, changed)
	}
	if k, _, changed := w.Check(); changed || k != "" {
		t.Fatalf("Check with nothing changed: %s %v", k, changed)
	}
}

// TestWordsFor: DS4Windows' words for DS4Windows, DSX's for the rest.
func TestWordsFor(t *testing.T) {
	if WordsFor(KindDS4Windows).Name != "DS4Windows" || WordsFor(KindDSX).Name != "DSX" || WordsFor("").Name != "DSX" {
		t.Fatal("words")
	}
	if !reflect.DeepEqual(WordsFor(KindDS4Windows), DS4WindowsWords()) || !reflect.DeepEqual(WordsFor(KindDSX), DSXWords()) {
		t.Fatal("WordsFor differs from the words themselves")
	}
}

// countPad is a pad that records its closes.
type countPad struct {
	mu  *sync.Mutex
	log *[]string
}

func (p countPad) Maintain()                   {}
func (p countPad) Available() bool             { return false }
func (p countPad) State() Input                { return Input{} }
func (p countPad) SetRumble(left, right uint8) {}
func (p countPad) Close()                      { p.add("pad close") }

func (p countPad) add(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	*p.log = append(*p.log, s)
}

// TestCloseOnce: a backend's Close runs once however often it is called;
// Discard closes the pad first, then the backend; Addr is Parts.Addr, ""
// without one.
func TestCloseOnce(t *testing.T) {
	for _, assemble := range []func(Parts) *Backend{NewDSX, NewDS4Windows} {
		var mu sync.Mutex
		var got []string
		pad := countPad{&mu, &got}
		b := assemble(Parts{Pad: pad, Close: func() { pad.add("backend close") }, Addr: func() string { return "127.0.0.1:1" }})
		b.Discard()
		b.Close()
		b.Discard()
		if want := []string{"pad close", "backend close", "pad close"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v, want %v", b.Name, got, want)
		}
		if b.Addr() != "127.0.0.1:1" {
			t.Errorf("%s: Addr %q", b.Name, b.Addr())
		}
		none := assemble(Parts{})
		if none.Close != nil || none.Addr == nil || none.Addr() != "" {
			t.Errorf("%s without Close and Addr: Close set %v, Addr set %v", none.Name, none.Close != nil, none.Addr != nil)
		}
		none.Discard()
	}
}

// TestBackendAddr: the real backends tell where they send, and
// DS4Windows' backend closes twice without a panic.
func TestBackendAddr(t *testing.T) {
	sink := udpSink(t)
	b, err := DSX(sink.Port, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Addr(); got != sink.String() {
		t.Errorf("DSX: %s, want %s", got, sink)
	}
	b.Discard()
	d, err := DS4Windows(DS4WindowsOptions{Addr: sink, Follow: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Addr(); got != sink.String() {
		t.Errorf("DS4Windows: %s, want %s", got, sink)
	}
	d.Close()
	d.Close()
	d.Discard()
}

// TestDS4WindowsWarned: every Setup of one DS4Windows backend gets the
// same set of warnings told, the one given when there is one.
func TestDS4WindowsWarned(t *testing.T) {
	client, err := dsx.NewClientTo(udpSink(t), false, dsx.DS4Windows)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	once := &ds4w.Once{}
	if env := ds4wEnv(client, DS4WindowsOptions{Warned: once}); env.Warned != once {
		t.Error("the given set is not used")
	}
	if env := ds4wEnv(client, DS4WindowsOptions{}); env.Warned == nil {
		t.Error("no set of its own")
	}
}

// TestDS4WindowsSetupSeams: DS4Windows' setup publishes its Report and
// closes, ending its worker.
func TestDS4WindowsSetupSeams(t *testing.T) {
	base := runtime.NumGoroutine()
	var s Setup = ds4wSetup{ds4w.NewSetup(ds4w.Env{DataDir: func() string { return "../ds4w/testdata/DS4Windows" }}, nil)}
	r, ok := s.(Reporter)
	if !ok {
		t.Fatalf("Setup %T does not report", s)
	}
	c, ok := s.(interface{ Close() })
	if !ok {
		t.Fatalf("Setup %T does not close", s)
	}
	select {
	case <-s.(Checker).Checked():
	case <-time.After(2 * time.Second):
		t.Fatal("the first check never ended")
	}
	if rep := r.Report(); rep == nil || rep.Profile != "Elite Passthru" || rep.Gyro != ds4w.GyroFree {
		t.Fatalf("report %+v", rep)
	}
	c.Close()
	waitGoroutines(t, base)
}

// TestNoGoroutineLeft: the real DSX and DS4Windows backends, built and
// closed again and again, leave no goroutine behind, whether discarded
// before the app attached them or closed as the app closes an attached
// one and its session's setup. The HID list is stubbed, so no controller
// is touched: the pad it lists cannot be opened; DS4Windows' setup reads
// the test folder and asks no DS4Windows.
func TestNoGoroutineLeft(t *testing.T) {
	oldList, oldLog, oldDir, oldQuery := hidList, log.Writer(), ds4wDataDir, ds4wQuery
	ds4wDataDir = func() string { return "../ds4w/testdata/DS4Windows" }
	ds4wQuery = func(int, string) (string, error) { return "", errors.New("no DS4Windows in a test") }
	var mu sync.Mutex
	listed := 0
	hidList = func() []dualsense.HIDDevice {
		mu.Lock()
		listed++
		mu.Unlock()
		return []dualsense.HIDDevice{{Path: `\\?\HID#EDSENSE_TEST_NO_SUCH_PAD#0#{4d1e55b2-f16f-11cf-88cb-001111000030}`,
			ProductID: 0x0CE6, Kind: dualsense.Virtual, InLen: 64, OutLen: 48}}
	}
	log.SetOutput(io.Discard)
	defer func() {
		hidList, ds4wDataDir, ds4wQuery = oldList, oldDir, oldQuery
		log.SetOutput(oldLog)
	}()
	sink := udpSink(t)
	warned := &ds4w.Once{}
	base := runtime.NumGoroutine()
	for i := range 20 {
		var b *Backend
		var err error
		if i%2 == 0 {
			b, err = DSX(sink.Port, false)
		} else {
			b, err = DS4Windows(DS4WindowsOptions{Addr: sink, Follow: true, Warned: warned,
				Haptics: func() string { return config.DS4WHapticsAuto }})
		}
		if err != nil {
			t.Fatal(err)
		}
		b.Pad.Maintain()
		if i%4 < 2 {
			b.Discard() // built, never attached
		} else {
			// attached and detached, as the app does it
			audio := b.NewAudio(func([]int16) {})
			setup := b.NewSetup(t.TempDir(), func(string) {})
			b.Motion.OnSample(func(gyro.Sample) {})
			b.Motion.OnSample(nil)
			if c, ok := setup.(Checker); ok { // a worker that waits for more
				select {
				case <-c.Checked():
				case <-time.After(5 * time.Second):
					t.Fatal("the first check never ended")
				}
			}
			// the session's close, then the backend's detach
			if c, ok := setup.(interface{ Close() }); ok {
				c.Close()
			} else if i%2 == 1 {
				t.Errorf("DS4Windows' setup %T does not close", setup)
			}
			audio.Close()
			b.Pad.Close()
			b.Close()
		}
		b.Close()
		b.Discard()
	}
	waitGoroutines(t, base)
	mu.Lock()
	defer mu.Unlock()
	if runtime.GOOS == "windows" && listed == 0 {
		t.Error("the links did not look at the stubbed HID list")
	}
}

// TestPadsFromHIDList: the setup checks' pad facts come from the HID list:
// the virtual pad each backend would open, and a real one games can see.
func TestPadsFromHIDList(t *testing.T) {
	old := hidList
	defer func() { hidList = old }()
	dsxPad := dualsense.HIDDevice{Path: "dsx", ProductID: 0x0CE6, Kind: dualsense.Virtual, InLen: 64, OutLen: 48}
	viiper := dualsense.HIDDevice{Path: "viiper", ProductID: 0x0CE6, Kind: dualsense.Virtual, Host: dualsense.HostUSBIPWin2, InLen: 64, OutLen: 48}
	phys := dualsense.HIDDevice{Path: "real", ProductID: 0x0CE6, Kind: dualsense.Physical, InLen: 64, OutLen: 48}
	hidList = func() []dualsense.HIDDevice { return []dualsense.HIDDevice{phys, dsxPad, viiper} }
	if d, ok := VirtualPad(KindDSX); !ok || d.Path != "dsx" {
		t.Errorf("DSX's pad: %v %v", d.Path, ok)
	}
	if d, ok := VirtualPad(KindDS4Windows); !ok || d.Path != "viiper" {
		t.Errorf("DS4Windows' pad: %v %v", d.Path, ok)
	}
	if !PhysicalPadVisible() {
		t.Error("the real pad is not seen")
	}
	hidList = func() []dualsense.HIDDevice { return []dualsense.HIDDevice{phys} }
	if _, ok := VirtualPad(KindDS4Windows); ok {
		t.Error("a real pad taken for the virtual one")
	}
	hidList = func() []dualsense.HIDDevice { return nil }
	if PhysicalPadVisible() {
		t.Error("a real pad seen in an empty list")
	}
}

// udpSink is a local port that swallows what it gets.
func udpSink(t *testing.T) *net.UDPAddr {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buf := make([]byte, 65536)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr)
}

// waitGoroutines waits up to 2 s for the goroutines to be back to want.
func waitGoroutines(t *testing.T, want int) {
	t.Helper()
	n := 0
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if n = runtime.NumGoroutine(); n <= want {
			return
		}
	}
	buf := make([]byte, 1<<16)
	t.Fatalf("%d goroutines, want %d:\n%s", n, want, buf[:runtime.Stack(buf, true)])
}
